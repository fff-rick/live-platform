package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	centrifuge "github.com/centrifugal/centrifuge-go"
)

type config struct {
	endpoint, apiURL, apiKey, secret, report string
	clients, rooms                           int
	roomBase                                 int64
	connectRate, publishRate                 float64
	duration, slowDelay                      time.Duration
	slowRatio                                float64
}

type counters struct {
	connected          atomic.Int64
	connectEvents      atomic.Int64
	connectErrors      atomic.Int64
	disconnected       atomic.Int64
	subscribed         atomic.Int64
	subscriptionErrors atomic.Int64
	publications       atomic.Int64
	recovered          atomic.Int64
	recoveryAttempts   atomic.Int64
	publishOK          atomic.Int64
	publishErr         atomic.Int64
}

type latencySet struct {
	mu     sync.Mutex
	values []time.Duration
}

func (l *latencySet) add(v time.Duration) {
	if v < 0 || v > time.Hour {
		return
	}
	l.mu.Lock()
	l.values = append(l.values, v)
	l.mu.Unlock()
}
func (l *latencySet) snapshot() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]time.Duration(nil), l.values...)
	return out
}

type result struct {
	StartedAt          string  `json:"started_at"`
	Duration           string  `json:"duration"`
	Clients            int     `json:"clients"`
	Rooms              int     `json:"rooms"`
	ConnectRate        float64 `json:"connect_rate_per_sec"`
	PublishRate        float64 `json:"publish_rate_per_sec"`
	SlowRatio          float64 `json:"slow_ratio"`
	Connected          int64   `json:"connected_current"`
	ConnectEvents      int64   `json:"connect_events"`
	ConnectErrors      int64   `json:"connect_errors"`
	Disconnected       int64   `json:"disconnect_events"`
	Subscribed         int64   `json:"subscribe_events"`
	SubscriptionErrors int64   `json:"subscription_errors"`
	Publications       int64   `json:"publications_received"`
	RecoveryAttempts   int64   `json:"recovery_attempts"`
	Recovered          int64   `json:"recovered_success"`
	PublishOK          int64   `json:"publish_success"`
	PublishErr         int64   `json:"publish_errors"`
	LatencyP50         string  `json:"latency_p50"`
	LatencyP95         string  `json:"latency_p95"`
	LatencyP99         string  `json:"latency_p99"`
	LatencyMax         string  `json:"latency_max"`
}

type payload struct {
	Seq     int64  `json:"seq"`
	SentAt  int64  `json:"sent_at_unix_nano"`
	Padding string `json:"padding,omitempty"`
}

func main() {
	var c config
	flag.StringVar(&c.endpoint, "endpoint", "ws://localhost:8000/connection/websocket", "Centrifugo websocket endpoint")
	flag.StringVar(&c.apiURL, "api-url", "http://localhost:8000/api/publish", "Centrifugo publish API URL")
	flag.StringVar(&c.apiKey, "api-key", "dev-api-key-change-me", "Centrifugo API key")
	flag.StringVar(&c.secret, "token-secret", "dev-token-secret-change-me", "Centrifugo JWT secret")
	flag.IntVar(&c.clients, "clients", 1000, "virtual websocket clients")
	flag.IntVar(&c.rooms, "rooms", 1, "number of rooms to distribute clients across")
	flag.Int64Var(&c.roomBase, "room-base", 900000, "first synthetic room id")
	flag.Float64Var(&c.connectRate, "connect-rate", 1000, "new clients per second; 0 = as fast as possible")
	flag.Float64Var(&c.publishRate, "publish-rate", 0, "server-side publishes per second; 0 disables publishing")
	flag.DurationVar(&c.duration, "duration", 30*time.Second, "steady-state test duration after client creation")
	flag.Float64Var(&c.slowRatio, "slow-ratio", 0, "fraction of clients whose publication callback intentionally blocks")
	flag.DurationVar(&c.slowDelay, "slow-delay", 250*time.Millisecond, "delay for simulated slow consumers")
	flag.StringVar(&c.report, "report", "", "write JSON report to path")
	flag.Parse()
	if c.clients <= 0 || c.rooms <= 0 || c.slowRatio < 0 || c.slowRatio > 1 {
		fmt.Fprintln(os.Stderr, "invalid clients/rooms/slow-ratio")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	started := time.Now()
	var ctr counters
	var lat latencySet
	clients := make([]*centrifuge.Client, 0, c.clients)
	var clientsMu sync.Mutex

	pace := time.Duration(0)
	if c.connectRate > 0 {
		pace = time.Duration(float64(time.Second) / c.connectRate)
	}
clientLoop:
	for i := 0; i < c.clients; i++ {
		select {
		case <-ctx.Done():
			break clientLoop
		default:
		}
		userID := strconv.FormatInt(100000000+int64(i), 10)
		roomID := c.roomBase + int64(i%c.rooms)
		slow := float64(i)/float64(c.clients) < c.slowRatio
		client := makeClient(c, userID, roomID, slow, &ctr, &lat)
		clientsMu.Lock()
		clients = append(clients, client)
		clientsMu.Unlock()
		if pace > 0 {
			select {
			case <-ctx.Done():
				break clientLoop
			case <-time.After(pace):
			}
		}
	}

	pubCtx, cancelPub := context.WithCancel(ctx)
	var pubWG sync.WaitGroup
	if c.publishRate > 0 {
		pubWG.Add(1)
		go func() { defer pubWG.Done(); publishLoop(pubCtx, c, &ctr) }()
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(c.duration)
	defer deadline.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-deadline.C:
			break loop
		case <-ticker.C:
			fmt.Printf("connected=%d connect_events=%d pubs=%d publish_ok=%d publish_err=%d recovered=%d\n", ctr.connected.Load(), ctr.connectEvents.Load(), ctr.publications.Load(), ctr.publishOK.Load(), ctr.publishErr.Load(), ctr.recovered.Load())
		}
	}
	cancelPub()
	pubWG.Wait()

	// Capture counters before intentionally closing all clients, otherwise the
	// final connected_current value would always trend to zero.
	r := buildResult(c, started, &ctr, lat.snapshot())
	clientsMu.Lock()
	for _, cl := range clients {
		cl.Close()
	}
	clientsMu.Unlock()
	raw, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(raw))
	if c.report != "" {
		if err := os.WriteFile(c.report, raw, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
		}
	}
}

func makeClient(c config, userID string, roomID int64, slow bool, ctr *counters, lat *latencySet) *centrifuge.Client {
	tokenFn := func() string {
		return signJWT(c.secret, map[string]any{"sub": userID, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
	}
	client := centrifuge.NewJsonClient(c.endpoint, centrifuge.Config{Token: tokenFn(), GetToken: func(centrifuge.ConnectionTokenEvent) (string, error) { return tokenFn(), nil }})
	client.OnConnected(func(centrifuge.ConnectedEvent) { ctr.connected.Add(1); ctr.connectEvents.Add(1) })
	client.OnDisconnected(func(centrifuge.DisconnectedEvent) { ctr.connected.Add(-1); ctr.disconnected.Add(1) })
	client.OnError(func(centrifuge.ErrorEvent) { ctr.connectErrors.Add(1) })
	if err := client.Connect(); err != nil {
		ctr.connectErrors.Add(1)
		return client
	}
	channel := fmt.Sprintf("room:%d:stream", roomID)
	subToken := func() string {
		return signJWT(c.secret, map[string]any{"sub": userID, "channel": channel, "exp": time.Now().Add(10 * time.Minute).Unix(), "expire_at": 0, "iat": time.Now().Unix()})
	}
	sub, err := client.NewSubscription(channel, centrifuge.SubscriptionConfig{Token: subToken(), GetToken: func(centrifuge.SubscriptionTokenEvent) (string, error) { return subToken(), nil }, Recoverable: true, Positioned: true})
	if err != nil {
		ctr.subscriptionErrors.Add(1)
		return client
	}
	sub.OnSubscribed(func(e centrifuge.SubscribedEvent) {
		ctr.subscribed.Add(1)
		if e.WasRecovering {
			ctr.recoveryAttempts.Add(1)
		}
		if e.Recovered {
			ctr.recovered.Add(1)
		}
	})
	sub.OnError(func(centrifuge.SubscriptionErrorEvent) { ctr.subscriptionErrors.Add(1) })
	sub.OnPublication(func(e centrifuge.PublicationEvent) {
		ctr.publications.Add(1)
		var p payload
		if json.Unmarshal(e.Data, &p) == nil && p.SentAt > 0 {
			lat.add(time.Since(time.Unix(0, p.SentAt)))
		}
		if slow {
			time.Sleep(c.slowDelay)
		}
	})
	_ = sub.Subscribe()
	return client
}

func publishLoop(ctx context.Context, c config, ctr *counters) {
	interval := time.Duration(float64(time.Second) / c.publishRate)
	if interval <= 0 {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	httpClient := &http.Client{Timeout: 5 * time.Second}
	var seq int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seq++
			roomID := c.roomBase + (seq % int64(c.rooms))
			p := payload{Seq: seq, SentAt: time.Now().UnixNano()}
			data, _ := json.Marshal(p)
			body, _ := json.Marshal(map[string]any{"channel": fmt.Sprintf("room:%d:stream", roomID), "data": json.RawMessage(data)})
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", c.apiKey)
			resp, err := httpClient.Do(req)
			if err != nil {
				ctr.publishErr.Add(1)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				ctr.publishOK.Add(1)
			} else {
				ctr.publishErr.Add(1)
			}
		}
	}
}

func signJWT(secret string, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func buildResult(c config, started time.Time, ctr *counters, vals []time.Duration) result {
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	q := func(p float64) time.Duration {
		if len(vals) == 0 {
			return 0
		}
		idx := int(math.Ceil(p*float64(len(vals)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(vals) {
			idx = len(vals) - 1
		}
		return vals[idx]
	}
	max := time.Duration(0)
	if len(vals) > 0 {
		max = vals[len(vals)-1]
	}
	return result{StartedAt: started.UTC().Format(time.RFC3339), Duration: time.Since(started).String(), Clients: c.clients, Rooms: c.rooms, ConnectRate: c.connectRate, PublishRate: c.publishRate, SlowRatio: c.slowRatio, Connected: ctr.connected.Load(), ConnectEvents: ctr.connectEvents.Load(), ConnectErrors: ctr.connectErrors.Load(), Disconnected: ctr.disconnected.Load(), Subscribed: ctr.subscribed.Load(), SubscriptionErrors: ctr.subscriptionErrors.Load(), Publications: ctr.publications.Load(), RecoveryAttempts: ctr.recoveryAttempts.Load(), Recovered: ctr.recovered.Load(), PublishOK: ctr.publishOK.Load(), PublishErr: ctr.publishErr.Load(), LatencyP50: q(.50).String(), LatencyP95: q(.95).String(), LatencyP99: q(.99).String(), LatencyMax: max.String()}
}
