package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	roles := roleSet(cfg.Worker.Roles)

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

	needsMySQL := roles["outbox"] || roles["gift-consumer"] || roles["danmaku-consumer"]
	needsRedis := roles["stats"]
	needsPublisher := roles["stats"] || roles["gift-consumer"]

	var mysql *mysqlstore.Store
	if needsMySQL {
		mysql, err = mysqlstore.Open(cfg.MySQL.DSN, mysqlstore.Config{
			MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime,
		})
		if err != nil {
			log.Error("open mysql", "error", err)
			os.Exit(1)
		}
		defer mysql.Close()
		metrics.RegisterDBPool("mysql", mysql.Stats)
	}

	var redis *redisstore.Store
	if needsRedis {
		redis, err = redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
		if err != nil {
			log.Error("open redis", "error", err)
			os.Exit(1)
		}
		defer redis.Close()
	}

	var publisher *realtime.Centrifugo
	if needsPublisher {
		publisher = realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, metrics)
	}

	kafkaCfg := mq.ClientConfig{
		Brokers: cfg.Kafka.Brokers, TLSEnabled: cfg.Kafka.TLSEnabled, SASLMechanism: cfg.Kafka.SASLMechanism,
		SASLUsername: cfg.Kafka.SASLUsername, SASLPassword: cfg.Kafka.SASLPassword,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	componentErr := make(chan error, len(cfg.Worker.Roles))
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

	if roles["stats"] {
		statsStore := stats.NewRedisStore(redis)
		aggregator := stats.NewAggregator(log, statsStore, publisher, cfg.Engagement.StatsInterval, cfg.Engagement.ActiveRoomWindow, cfg.Engagement.ActiveRoomBatch, metrics)
		run("stats-aggregator", aggregator.Run)
	}

	var kafkaProducer *mq.Producer
	if roles["outbox"] {
		kafkaProducer, err = mq.NewProducerWithConfig(kafkaCfg, log, metrics)
		if err != nil {
			log.Error("create kafka producer", "error", err)
			os.Exit(1)
		}
		defer kafkaProducer.Close()
		outboxRepo := outbox.NewRepository(mysql)
		outboxPublisher := outbox.NewPublisher(log, outboxRepo, kafkaProducer, "worker-"+idgen.New(), cfg.Outbox.PollInterval, cfg.Outbox.BatchSize, cfg.Outbox.Lease, cfg.Outbox.ProduceTimeout, metrics)
		run("outbox-publisher", outboxPublisher.Run)
	}

	var giftConsumer *mq.Consumer
	if roles["gift-consumer"] {
		giftConsumer, err = mq.NewConsumerWithConfig(kafkaCfg, cfg.Kafka.GiftConsumerGroup, []string{cfg.Kafka.GiftTopic}, log, metrics)
		if err != nil {
			log.Error("create gift consumer", "error", err)
			os.Exit(1)
		}
		defer giftConsumer.Close()
		dedup := eventdedup.NewStore(mysql)
		giftHandler := gift.NewConsumerHandler(cfg.Kafka.GiftConsumerGroup, dedup, publisher, cfg.Kafka.ConsumerLease)
		run("gift-consumer", func(ctx context.Context) error { return giftConsumer.Run(ctx, giftHandler) })
	}

	var danmakuConsumer *mq.Consumer
	if roles["danmaku-consumer"] {
		danmakuConsumer, err = mq.NewConsumerWithConfig(kafkaCfg, cfg.Kafka.DanmakuConsumerGroup, []string{cfg.Kafka.DanmakuTopic}, log, metrics)
		if err != nil {
			log.Error("create danmaku consumer", "error", err)
			os.Exit(1)
		}
		defer danmakuConsumer.Close()
		danmakuHandler := danmaku.NewPersistenceHandler(mysql, log)
		run("danmaku-consumer", func(ctx context.Context) error { return danmakuConsumer.Run(ctx, danmakuHandler) })
	}

	metricsServer := newMetricsServer(cfg.Observability.WorkerMetricsAddr, metrics, cfg.Worker.Roles)
	serverErr := make(chan error, 1)
	go func() {
		log.Info("worker metrics server started", "addr", cfg.Observability.WorkerMetricsAddr)
		serverErr <- metricsServer.ListenAndServe()
	}()

	log.Info("worker started", "milestone", "M8", "roles", strings.Join(cfg.Worker.Roles, ","), "kafka_brokers", cfg.Kafka.Brokers)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

func roleSet(roles []string) map[string]bool {
	out := make(map[string]bool, len(roles))
	for _, role := range roles {
		out[role] = true
	}
	return out
}

func newMetricsServer(addr string, metrics *observability.Metrics, roles []string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"live-worker","milestone":"M8","roles":"` + strings.Join(roles, ",") + `"}`))
	}
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /ready", health)
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
}

func shutdownTracing(log *slog.Logger, fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fn(ctx); err != nil {
		log.Warn("shutdown tracing", "error", err)
	}
}
