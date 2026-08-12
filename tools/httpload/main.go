package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type sampleSet struct {
	mu sync.Mutex
	v  []time.Duration
}

func (s *sampleSet) add(v time.Duration) { s.mu.Lock(); s.v = append(s.v, v); s.mu.Unlock() }
func main() {
	url := flag.String("url", "http://localhost:8080/health", "target URL")
	method := flag.String("method", "GET", "HTTP method")
	body := flag.String("body", "", "request body")
	bearer := flag.String("bearer", "", "Bearer token")
	rate := flag.Int("rate", 100, "requests per second")
	concurrency := flag.Int("concurrency", 32, "max in-flight requests")
	duration := flag.Duration("duration", 30*time.Second, "test duration")
	idem := flag.Bool("idempotency", false, "generate a unique Idempotency-Key per request")
	report := flag.String("report", "", "JSON report path")
	flag.Parse()
	if *rate <= 0 || *concurrency <= 0 {
		panic("rate and concurrency must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second}
	sem := make(chan struct{}, *concurrency)
	ticker := time.NewTicker(time.Second / time.Duration(*rate))
	defer ticker.Stop()
	var sent, failed atomic.Int64
	statuses := sync.Map{}
	var samples sampleSet
	var wg sync.WaitGroup
	started := time.Now()
	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			sem <- struct{}{}
			wg.Add(1)
			n := sent.Add(1)
			go func(seq int64) {
				defer wg.Done()
				defer func() { <-sem }()
				req, err := http.NewRequestWithContext(ctx, *method, *url, bytes.NewBufferString(*body))
				if err != nil {
					failed.Add(1)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				if *bearer != "" {
					req.Header.Set("Authorization", "Bearer "+*bearer)
				}
				if *idem {
					req.Header.Set("Idempotency-Key", fmt.Sprintf("m7-%d-%d", time.Now().UnixNano(), seq))
				}
				begin := time.Now()
				resp, err := client.Do(req)
				samples.add(time.Since(begin))
				if err != nil {
					failed.Add(1)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				key := strconv.Itoa(resp.StatusCode)
				v, _ := statuses.LoadOrStore(key, new(atomic.Int64))
				v.(*atomic.Int64).Add(1)
			}(n)
		}
	}
done:
	wg.Wait()
	vals := samples.v
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	quant := func(p float64) string {
		if len(vals) == 0 {
			return "0s"
		}
		i := int(float64(len(vals)-1) * p)
		return vals[i].String()
	}
	sm := map[string]int64{}
	statuses.Range(func(k, v any) bool { sm[k.(string)] = v.(*atomic.Int64).Load(); return true })
	out := map[string]any{"started_at": started.UTC(), "duration": time.Since(started).String(), "sent": sent.Load(), "failed": failed.Load(), "statuses": sm, "p50": quant(.50), "p95": quant(.95), "p99": quant(.99)}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
	if *report != "" {
		_ = os.WriteFile(*report, raw, 0644)
	}
}
