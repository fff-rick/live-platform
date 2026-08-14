package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/eventdedup"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/outbox"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
)

func main() {
	baseHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(observability.NewTraceHandler(baseHandler))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	traceShutdown, err := observability.InitTracer(context.Background(), observability.TraceConfig{
		Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-worker", Environment: cfg.Observability.Environment,
		Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio,
	})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer shutdownTracing(log, traceShutdown)
	metrics := observability.NewMetrics("live-worker")

	mysql, err := mysqlstore.Open(cfg.MySQL.DSN)
	if err != nil {
		log.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer func() { _ = mysql.Close() }()
	redis := redisstore.Open(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer func() { _ = redis.Close() }()

	kafkaProducer, err := mq.NewProducer(cfg.Kafka.Brokers, log, metrics)
	if err != nil {
		log.Error("create kafka producer", "error", err)
		os.Exit(1)
	}
	defer kafkaProducer.Close()
	giftConsumer, err := mq.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GiftConsumerGroup, []string{cfg.Kafka.GiftTopic}, log, metrics)
	if err != nil {
		log.Error("create gift consumer", "error", err)
		os.Exit(1)
	}
	defer giftConsumer.Close()
	danmakuConsumer, err := mq.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.DanmakuConsumerGroup, []string{cfg.Kafka.DanmakuTopic}, log, metrics)
	if err != nil {
		log.Error("create danmaku consumer", "error", err)
		os.Exit(1)
	}
	defer danmakuConsumer.Close()

	publisher := realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, metrics)
	statsStore := stats.NewRedisStore(redis)
	aggregator := stats.NewAggregator(log, statsStore, publisher, cfg.Engagement.StatsInterval, cfg.Engagement.ActiveRoomWindow, cfg.Engagement.ActiveRoomBatch, metrics)

	outboxRepo := outbox.NewRepository(mysql)
	outboxPublisher := outbox.NewPublisher(log, outboxRepo, kafkaProducer, "worker-"+idgen.New(), cfg.Outbox.PollInterval, cfg.Outbox.BatchSize, cfg.Outbox.Lease, cfg.Outbox.ProduceTimeout, metrics)

	dedup := eventdedup.NewStore(mysql)
	giftHandler := gift.NewConsumerHandler(cfg.Kafka.GiftConsumerGroup, dedup, publisher, cfg.Kafka.ConsumerLease)
	danmakuHandler := danmaku.NewPersistenceHandler(mysql, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	metricsServer := newMetricsServer(cfg.Observability.WorkerMetricsAddr, metrics)
	serverErr := make(chan error, 1)
	go func() {
		log.Info("worker metrics server started", "addr", cfg.Observability.WorkerMetricsAddr)
		serverErr <- metricsServer.ListenAndServe()
	}()

	componentErr := make(chan error, 4)
	var wg sync.WaitGroup
	run := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("worker component started", "component", name)
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				componentErr <- errors.New(name + ": " + err.Error())
			}
		}()
	}
	run("stats-aggregator", aggregator.Run)
	run("outbox-publisher", outboxPublisher.Run)
	run("gift-consumer", func(ctx context.Context) error { return giftConsumer.Run(ctx, giftHandler) })
	run("danmaku-consumer", func(ctx context.Context) error { return danmakuConsumer.Run(ctx, danmakuHandler) })

	log.Info("worker started", "milestone", "M7", "kafka_brokers", cfg.Kafka.Brokers, "outbox_interval", cfg.Outbox.PollInterval.String())
	select {
	case <-ctx.Done():
		log.Info("worker shutdown signal")
	case err := <-componentErr:
		log.Error("worker component stopped unexpectedly", "error", err)
		stop()
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("worker metrics server stopped unexpectedly", "error", err)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = metricsServer.Shutdown(shutdownCtx)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("worker graceful shutdown timed out")
	}
	log.Info("worker stopped")
}

func newMetricsServer(addr string, metrics *observability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"live-worker","milestone":"M7"}`))
	})
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
}

func shutdownTracing(log *slog.Logger, fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fn(ctx); err != nil {
		log.Warn("shutdown tracing", "error", err)
	}
}
