package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type sampleSet struct {
	mu sync.Mutex
	v  []time.Duration
}

func (s *sampleSet) add(v time.Duration) {
	s.mu.Lock()
	s.v = append(s.v, v)
	s.mu.Unlock()
}

func (s *sampleSet) snapshot() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.v...)
}

type reportData struct {
	StartedAt          string           `json:"started_at"`
	Scenario           string           `json:"scenario,omitempty"`
	GeneratorHostname  string           `json:"generator_hostname"`
	GeneratorGOOS      string           `json:"generator_goos"`
	GeneratorGOARCH    string           `json:"generator_goarch"`
	GeneratorCPUs      int              `json:"generator_cpus"`
	GeneratorGoVersion string           `json:"generator_go_version"`
	Duration           string           `json:"duration"`
	LoadDuration       string           `json:"load_duration"`
	TargetRate         int              `json:"target_rate_per_sec"`
	AchievedRate       float64          `json:"achieved_rate_per_sec"`
	Concurrency        int              `json:"concurrency"`
	Method             string           `json:"method"`
	URL                string           `json:"url"`
	Body               string           `json:"request_body"`
	BodyBytes          int              `json:"request_body_bytes"`
	Idempotency        bool             `json:"idempotency"`
	BearerTokenCount   int              `json:"bearer_token_count"`
	Sent               int64            `json:"sent"`
	Completed          int64            `json:"completed"`
	Failed             int64            `json:"failed"`
	TransportFailed    int64            `json:"transport_failed"`
	HTTPFailed         int64            `json:"http_failed"`
	Statuses           map[string]int64 `json:"statuses"`
	P50                string           `json:"p50"`
	P95                string           `json:"p95"`
	P99                string           `json:"p99"`
	Max                string           `json:"max"`
}

func main() {
	url := flag.String("url", "http://localhost:8080/health", "target URL")
	method := flag.String("method", "GET", "HTTP method")
	body := flag.String("body", "", "request body")
	bearer := flag.String("bearer", "", "single Bearer token")
	bearerFile := flag.String("bearer-file", "", "file containing one Bearer token per line; requests round-robin across tokens")
	rate := flag.Int("rate", 100, "target requests per second")
	concurrency := flag.Int("concurrency", 32, "max in-flight requests")
	duration := flag.Duration("duration", 30*time.Second, "test duration")
	requestTimeout := flag.Duration("request-timeout", 10*time.Second, "per-request timeout")
	idem := flag.Bool("idempotency", false, "generate a unique Idempotency-Key per request")
	idemPrefix := flag.String("idempotency-prefix", "m7", "Idempotency-Key prefix")
	report := flag.String("report", "", "JSON report path")
	scenario := flag.String("scenario", "", "human-readable benchmark scenario label")
	flag.Parse()
	if *rate <= 0 || *concurrency <= 0 || *duration <= 0 || *requestTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "rate, concurrency, duration and request-timeout must be positive")
		os.Exit(2)
	}

	tokens, err := loadTokens(*bearer, *bearerFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load bearer tokens:", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: *requestTimeout}
	sem := make(chan struct{}, *concurrency)
	interval := time.Second / time.Duration(*rate)
	if interval <= 0 {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var sent, completed, transportFailed, httpFailed atomic.Int64
	statuses := sync.Map{}
	var samples sampleSet
	var wg sync.WaitGroup
	started := time.Now()
	deadline := time.NewTimer(*duration)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-deadline.C:
			goto done
		case <-ticker.C:
			select {
			case <-ctx.Done():
				goto done
			case sem <- struct{}{}:
			}
			n := sent.Add(1)
			wg.Add(1)
			go func(seq int64) {
				defer wg.Done()
				defer func() { <-sem }()
				req, err := http.NewRequestWithContext(ctx, strings.ToUpper(*method), *url, bytes.NewBufferString(*body))
				if err != nil {
					transportFailed.Add(1)
					completed.Add(1)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				if len(tokens) > 0 {
					req.Header.Set("Authorization", "Bearer "+tokens[(seq-1)%int64(len(tokens))])
				}
				if *idem {
					req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d-%d", *idemPrefix, started.UnixNano(), seq))
				}
				begin := time.Now()
				resp, err := client.Do(req)
				samples.add(time.Since(begin))
				if err != nil {
					transportFailed.Add(1)
					completed.Add(1)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				key := strconv.Itoa(resp.StatusCode)
				v, _ := statuses.LoadOrStore(key, new(atomic.Int64))
				v.(*atomic.Int64).Add(1)
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					httpFailed.Add(1)
				}
				completed.Add(1)
			}(n)
		}
	}

done:
	loadElapsed := time.Since(started)
	wg.Wait()
	elapsed := time.Since(started)
	vals := samples.snapshot()
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	quant := func(p float64) string {
		if len(vals) == 0 {
			return "0s"
		}
		i := int(float64(len(vals)-1) * p)
		return vals[i].String()
	}
	maxLatency := "0s"
	if len(vals) > 0 {
		maxLatency = vals[len(vals)-1].String()
	}
	sm := map[string]int64{}
	statuses.Range(func(k, v any) bool {
		sm[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	achieved := 0.0
	if loadElapsed > 0 {
		achieved = float64(sent.Load()) / loadElapsed.Seconds()
	}
	totalFailed := transportFailed.Load() + httpFailed.Load()
	hostname, _ := os.Hostname()
	out := reportData{
		StartedAt: started.UTC().Format(time.RFC3339Nano), Scenario: *scenario, GeneratorHostname: hostname, GeneratorGOOS: runtime.GOOS, GeneratorGOARCH: runtime.GOARCH, GeneratorCPUs: runtime.NumCPU(), GeneratorGoVersion: runtime.Version(), Duration: elapsed.String(), LoadDuration: loadElapsed.String(), TargetRate: *rate, AchievedRate: achieved, Concurrency: *concurrency,
		Method: strings.ToUpper(*method), URL: *url, Body: *body, BodyBytes: len(*body), Idempotency: *idem, BearerTokenCount: len(tokens),
		Sent: sent.Load(), Completed: completed.Load(), Failed: totalFailed, TransportFailed: transportFailed.Load(), HTTPFailed: httpFailed.Load(), Statuses: sm,
		P50: quant(.50), P95: quant(.95), P99: quant(.99), Max: maxLatency,
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
	if *report != "" {
		if err := os.WriteFile(*report, raw, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
		}
	}
}

func loadTokens(single, path string) ([]string, error) {
	var out []string
	if strings.TrimSpace(single) != "" {
		out = append(out, strings.TrimSpace(single))
	}
	if strings.TrimSpace(path) == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		v := strings.TrimSpace(s.Text())
		if v != "" {
			out = append(out, v)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
