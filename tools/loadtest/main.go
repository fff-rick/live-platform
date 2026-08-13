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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	centrifuge "github.com/centrifugal/centrifuge-go"
)

type config struct {
	endpoint, apiURL, apiKey, secret, report, scenario string
	clients, rooms                                     int
	roomBase                                           int64
	connectRate, publishRate                           float64
	connectConcurrency, publishConcurrency             int
	connectReadyTimeout                                time.Duration
	duration, slowDelay                                time.Duration
	slowRatio                                          float64
	messageBytes                                       int
}

type counters struct {
	connectedCurrent      atomic.Int64
	initialConnected      atomic.Int64
	connectEvents         atomic.Int64
	reconnectEvents       atomic.Int64
	connectImmediateError atomic.Int64
	clientErrors          atomic.Int64
	disconnectEvents      atomic.Int64
	fastDisconnects       atomic.Int64
	slowDisconnects       atomic.Int64
	subscribeEvents       atomic.Int64
	initialSubscribed     atomic.Int64
	resubscribeEvents     atomic.Int64
	subscriptionErrors    atomic.Int64
	publications          atomic.Int64
	fastPublications      atomic.Int64
	slowPublications      atomic.Int64
	recovered             atomic.Int64
	recoveryAttempts      atomic.Int64
	publishOK             atomic.Int64
	publishErr            atomic.Int64
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
	return append([]time.Duration(nil), l.values...)
}

type latencySummary struct {
	P50 string `json:"p50"`
	P95 string `json:"p95"`
	P99 string `json:"p99"`
	Max string `json:"max"`
}

type result struct {
	StartedAt              string         `json:"started_at"`
	Scenario               string         `json:"scenario,omitempty"`
	GeneratorHostname      string         `json:"generator_hostname"`
	GeneratorGOOS          string         `json:"generator_goos"`
	GeneratorGOARCH        string         `json:"generator_goarch"`
	GeneratorCPUs          int            `json:"generator_cpus"`
	GeneratorGoVersion     string         `json:"generator_go_version"`
	Duration               string         `json:"duration"`
	SteadyDuration         string         `json:"steady_duration"`
	Clients                int            `json:"clients"`
	Rooms                  int            `json:"rooms"`
	MessageTargetBytes     int            `json:"message_target_bytes"`
	ConnectRate            float64        `json:"connect_rate_target_per_sec"`
	ConnectActualRate      float64        `json:"connect_rate_actual_per_sec"`
	ConnectConcurrency     int            `json:"connect_concurrency"`
	ConnectReadyTimeout    string         `json:"connect_ready_timeout"`
	ConnectPhaseDuration   string         `json:"connect_phase_duration"`
	ConnectionSuccessRate  float64        `json:"connection_success_rate"`
	PublishRate            float64        `json:"publish_rate_target_per_sec"`
	PublishActualRate      float64        `json:"publish_rate_actual_per_sec"`
	PublishConcurrency     int            `json:"publish_concurrency"`
	FanoutActualRate       float64        `json:"fanout_delivery_actual_per_sec"`
	SlowRatio              float64        `json:"slow_ratio"`
	SlowDelay              string         `json:"slow_delay"`
	Connected              int64          `json:"connected_current"`
	InitialConnected       int64          `json:"initial_connected"`
	ConnectEvents          int64          `json:"connect_events"`
	ReconnectEvents        int64          `json:"reconnect_events"`
	ConnectImmediateErrors int64          `json:"connect_immediate_errors"`
	ClientErrors           int64          `json:"client_errors"`
	Disconnected           int64          `json:"disconnect_events"`
	FastDisconnects        int64          `json:"fast_disconnect_events"`
	SlowDisconnects        int64          `json:"slow_disconnect_events"`
	Subscribed             int64          `json:"subscribe_events"`
	InitialSubscribed      int64          `json:"initial_subscribed"`
	ResubscribeEvents      int64          `json:"resubscribe_events"`
	SubscriptionErrors     int64          `json:"subscription_errors"`
	Publications           int64          `json:"publications_received"`
	FastPublications       int64          `json:"fast_publications_received"`
	SlowPublications       int64          `json:"slow_publications_received"`
	RecoveryAttempts       int64          `json:"recovery_attempts"`
	Recovered              int64          `json:"recovered_success"`
	PublishOK              int64          `json:"publish_success"`
	PublishErr             int64          `json:"publish_errors"`
	LatencyP50             string         `json:"latency_p50"`
	LatencyP95             string         `json:"latency_p95"`
	LatencyP99             string         `json:"latency_p99"`
	LatencyMax             string         `json:"latency_max"`
	FastLatency            latencySummary `json:"fast_latency"`
	SlowLatency            latencySummary `json:"slow_latency"`
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
	flag.Float64Var(&c.connectRate, "connect-rate", 1000, "target new clients per second; 0 = as fast as possible")
	flag.IntVar(&c.connectConcurrency, "connect-concurrency", 256, "maximum concurrent client setup operations")
	flag.DurationVar(&c.connectReadyTimeout, "connect-ready-timeout", 30*time.Second, "maximum wait for initial client connections after launch")
	flag.Float64Var(&c.publishRate, "publish-rate", 0, "target server-side publishes per second; 0 disables publishing")
	flag.IntVar(&c.publishConcurrency, "publish-concurrency", 64, "maximum concurrent publish HTTP requests")
	flag.IntVar(&c.messageBytes, "message-bytes", 256, "approximate publication payload size in bytes")
	flag.DurationVar(&c.duration, "duration", 30*time.Second, "steady-state test duration after connection phase")
	flag.Float64Var(&c.slowRatio, "slow-ratio", 0, "fraction of clients whose publication callback intentionally blocks")
	flag.DurationVar(&c.slowDelay, "slow-delay", 250*time.Millisecond, "delay for simulated slow consumers")
	flag.StringVar(&c.report, "report", "", "write JSON report to path")
	flag.StringVar(&c.scenario, "scenario", "", "human-readable benchmark scenario label")
	flag.Parse()
	if c.clients <= 0 || c.rooms <= 0 || c.slowRatio < 0 || c.slowRatio > 1 || c.connectConcurrency <= 0 || c.publishConcurrency <= 0 || c.messageBytes < 0 || c.connectReadyTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "invalid clients/rooms/concurrency/slow-ratio/message-bytes/connect-ready-timeout")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	started := time.Now()
	var ctr counters
	var allLat, fastLat, slowLat latencySet
	clients := make([]*centrifuge.Client, c.clients)

	connectStarted := time.Now()
	launchClients(ctx, c, clients, &ctr, &allLat, &fastLat, &slowLat)
	waitForInitialConnections(ctx, c, &ctr)
	connectDuration := time.Since(connectStarted)

	steadyStarted := time.Now()
	pubCtx, cancelPub := context.WithCancel(ctx)
	var pubWG sync.WaitGroup
	if c.publishRate > 0 {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			publishLoop(pubCtx, c, &ctr)
		}()
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
			fmt.Printf("connected=%d initial=%d reconnects=%d pubs=%d publish_ok=%d publish_err=%d recovered=%d\n",
				ctr.connectedCurrent.Load(), ctr.initialConnected.Load(), ctr.reconnectEvents.Load(), ctr.publications.Load(), ctr.publishOK.Load(), ctr.publishErr.Load(), ctr.recovered.Load())
		}
	}
	steadyDuration := time.Since(steadyStarted)
	cancelPub()
	pubWG.Wait()

	// Capture counters before intentionally closing all clients.
	r := buildResult(c, started, connectDuration, steadyDuration, &ctr, allLat.snapshot(), fastLat.snapshot(), slowLat.snapshot())
	for _, cl := range clients {
		if cl != nil {
			cl.Close()
		}
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(raw))
	if c.report != "" {
		if err := os.WriteFile(c.report, raw, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
		}
	}
}

func launchClients(ctx context.Context, c config, clients []*centrifuge.Client, ctr *counters, allLat, fastLat, slowLat *latencySet) {
	sem := make(chan struct{}, c.connectConcurrency)
	var wg sync.WaitGroup
	var ticker *time.Ticker
	if c.connectRate > 0 {
		interval := time.Duration(float64(time.Second) / c.connectRate)
		if interval <= 0 {
			interval = time.Nanosecond
		}
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	for i := 0; i < c.clients; i++ {
		if ticker != nil && i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			userID := strconv.FormatInt(100000000+int64(i), 10)
			roomID := c.roomBase + int64(i%c.rooms)
			slow := i < int(math.Round(float64(c.clients)*c.slowRatio))
			clients[i] = makeClient(c, userID, roomID, slow, ctr, allLat, fastLat, slowLat)
		}(i)
	}
	wg.Wait()
}

func waitForInitialConnections(ctx context.Context, c config, ctr *counters) {
	deadline := time.NewTimer(c.connectReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctr.initialConnected.Load()+ctr.connectImmediateError.Load() >= int64(c.clients) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}

func makeClient(c config, userID string, roomID int64, slow bool, ctr *counters, allLat, fastLat, slowLat *latencySet) *centrifuge.Client {
	tokenFn := func() string {
		return signJWT(c.secret, map[string]any{"sub": userID, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
	}
	client := centrifuge.NewJsonClient(c.endpoint, centrifuge.Config{Token: tokenFn(), GetToken: func(centrifuge.ConnectionTokenEvent) (string, error) { return tokenFn(), nil }})
	var currentlyConnected atomic.Bool
	var everConnected atomic.Bool
	client.OnConnected(func(centrifuge.ConnectedEvent) {
		if currentlyConnected.CompareAndSwap(false, true) {
			ctr.connectedCurrent.Add(1)
		}
		ctr.connectEvents.Add(1)
		if everConnected.CompareAndSwap(false, true) {
			ctr.initialConnected.Add(1)
		} else {
			ctr.reconnectEvents.Add(1)
		}
	})
	client.OnDisconnected(func(centrifuge.DisconnectedEvent) {
		if currentlyConnected.CompareAndSwap(true, false) {
			ctr.connectedCurrent.Add(-1)
		}
		ctr.disconnectEvents.Add(1)
		if slow {
			ctr.slowDisconnects.Add(1)
		} else {
			ctr.fastDisconnects.Add(1)
		}
	})
	client.OnError(func(centrifuge.ErrorEvent) { ctr.clientErrors.Add(1) })
	if err := client.Connect(); err != nil {
		ctr.connectImmediateError.Add(1)
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
	var everSubscribed atomic.Bool
	sub.OnSubscribed(func(e centrifuge.SubscribedEvent) {
		ctr.subscribeEvents.Add(1)
		if everSubscribed.CompareAndSwap(false, true) {
			ctr.initialSubscribed.Add(1)
		} else {
			ctr.resubscribeEvents.Add(1)
		}
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
		if slow {
			ctr.slowPublications.Add(1)
		} else {
			ctr.fastPublications.Add(1)
		}
		var p payload
		if json.Unmarshal(e.Data, &p) == nil && p.SentAt > 0 {
			v := time.Since(time.Unix(0, p.SentAt))
			allLat.add(v)
			if slow {
				slowLat.add(v)
			} else {
				fastLat.add(v)
			}
		}
		if slow {
			time.Sleep(c.slowDelay)
		}
	})
	if err := sub.Subscribe(); err != nil {
		ctr.subscriptionErrors.Add(1)
	}
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
	sem := make(chan struct{}, c.publishConcurrency)
	var wg sync.WaitGroup
	var seq atomic.Int64
	padding := strings.Repeat("x", max(0, c.messageBytes-72))
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			n := seq.Add(1)
			wg.Add(1)
			go func(seq int64) {
				defer wg.Done()
				defer func() { <-sem }()
				roomID := c.roomBase + ((seq - 1) % int64(c.rooms))
				p := payload{Seq: seq, SentAt: time.Now().UnixNano(), Padding: padding}
				data, _ := json.Marshal(p)
				body, _ := json.Marshal(map[string]any{"channel": fmt.Sprintf("room:%d:stream", roomID), "data": json.RawMessage(data)})
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
				if err != nil {
					ctr.publishErr.Add(1)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-API-Key", c.apiKey)
				resp, err := httpClient.Do(req)
				if err != nil {
					ctr.publishErr.Add(1)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					ctr.publishOK.Add(1)
				} else {
					ctr.publishErr.Add(1)
				}
			}(n)
		}
	}
}

func signJWT(secret string, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func buildResult(c config, started time.Time, connectDuration, steadyDuration time.Duration, ctr *counters, all, fast, slow []time.Duration) result {
	hostname, _ := os.Hostname()
	initial := ctr.initialConnected.Load()
	connectionSuccessRate := 0.0
	if c.clients > 0 {
		connectionSuccessRate = float64(initial) / float64(c.clients)
	}
	connectActualRate := 0.0
	if connectDuration > 0 {
		connectActualRate = float64(initial) / connectDuration.Seconds()
	}
	publishActualRate := 0.0
	fanoutActualRate := 0.0
	if steadyDuration > 0 {
		publishActualRate = float64(ctr.publishOK.Load()) / steadyDuration.Seconds()
		fanoutActualRate = float64(ctr.publications.Load()) / steadyDuration.Seconds()
	}
	allSummary := summarizeLatency(all)
	return result{
		StartedAt: started.UTC().Format(time.RFC3339), Scenario: c.scenario, GeneratorHostname: hostname, GeneratorGOOS: runtime.GOOS, GeneratorGOARCH: runtime.GOARCH, GeneratorCPUs: runtime.NumCPU(), GeneratorGoVersion: runtime.Version(), Duration: time.Since(started).String(), SteadyDuration: steadyDuration.String(), Clients: c.clients, Rooms: c.rooms,
		MessageTargetBytes: c.messageBytes, ConnectRate: c.connectRate, ConnectActualRate: connectActualRate, ConnectConcurrency: c.connectConcurrency,
		ConnectReadyTimeout: c.connectReadyTimeout.String(), ConnectPhaseDuration: connectDuration.String(), ConnectionSuccessRate: connectionSuccessRate,
		PublishRate: c.publishRate, PublishActualRate: publishActualRate, PublishConcurrency: c.publishConcurrency, FanoutActualRate: fanoutActualRate,
		SlowRatio: c.slowRatio, SlowDelay: c.slowDelay.String(), Connected: ctr.connectedCurrent.Load(), InitialConnected: initial,
		ConnectEvents: ctr.connectEvents.Load(), ReconnectEvents: ctr.reconnectEvents.Load(), ConnectImmediateErrors: ctr.connectImmediateError.Load(), ClientErrors: ctr.clientErrors.Load(),
		Disconnected: ctr.disconnectEvents.Load(), FastDisconnects: ctr.fastDisconnects.Load(), SlowDisconnects: ctr.slowDisconnects.Load(),
		Subscribed: ctr.subscribeEvents.Load(), InitialSubscribed: ctr.initialSubscribed.Load(), ResubscribeEvents: ctr.resubscribeEvents.Load(), SubscriptionErrors: ctr.subscriptionErrors.Load(),
		Publications: ctr.publications.Load(), FastPublications: ctr.fastPublications.Load(), SlowPublications: ctr.slowPublications.Load(), RecoveryAttempts: ctr.recoveryAttempts.Load(), Recovered: ctr.recovered.Load(),
		PublishOK: ctr.publishOK.Load(), PublishErr: ctr.publishErr.Load(), LatencyP50: allSummary.P50, LatencyP95: allSummary.P95, LatencyP99: allSummary.P99, LatencyMax: allSummary.Max,
		FastLatency: summarizeLatency(fast), SlowLatency: summarizeLatency(slow),
	}
}

func summarizeLatency(vals []time.Duration) latencySummary {
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
	maxV := time.Duration(0)
	if len(vals) > 0 {
		maxV = vals[len(vals)-1]
	}
	return latencySummary{P50: q(.50).String(), P95: q(.95).String(), P99: q(.99).String(), Max: maxV.String()}
}
