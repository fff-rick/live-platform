package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/like"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
	cftoken "github.com/example/live-platform/internal/token"
	"github.com/example/live-platform/internal/viewer"
	"github.com/example/live-platform/internal/wallet"
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
		Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-api", Environment: cfg.Observability.Environment,
		Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio,
	})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer shutdownTracing(log, traceShutdown)
	metrics := observability.NewMetrics("live-api")

	mysql, err := mysqlstore.Open(cfg.MySQL.DSN)
	if err != nil {
		log.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer mysql.Close()
	redis := redisstore.Open(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer redis.Close()

	appTokens := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	cfTokens := cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL)
	publisher := realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, metrics)
	// Kafka is deliberately not part of readiness. The API must keep serving gift
	// transactions through the outbox, and realtime danmaku can degrade without MQ.
	kafkaProducer, err := mq.NewProducer(cfg.Kafka.Brokers, log, metrics)
	if err != nil {
		log.Error("create kafka producer", "error", err)
		os.Exit(1)
	}
	defer kafkaProducer.Close()

	authRepo := auth.NewRepository(mysql)
	authService := auth.NewService(authRepo, appTokens)
	roomRepo := room.NewRepository(mysql)
	roomService := room.NewService(roomRepo)
	limiter := redisstore.NewFixedWindowLimiter(redis)
	filter := danmaku.NewSensitiveFilter(cfg.Danmaku.SensitiveWords)
	danmakuService := danmaku.NewService(roomService, authService, limiter, filter, publisher, danmaku.NewKafkaProducer(kafkaProducer, cfg.Kafka.DanmakuTopic, log))
	likeService := like.NewService(roomService, redis)
	viewerService := viewer.NewService(redis, cfg.Engagement.ViewerTTL)
	statsStore := stats.NewRedisStore(redis)
	statsService := stats.NewService(statsStore)
	walletRepo := wallet.NewRepository(mysql)
	walletService := wallet.NewService(walletRepo, cfg.Wallet.DevCreditEnabled)
	giftRepo := gift.NewRepository(mysql)
	giftService := gift.NewService(giftRepo, roomService, cfg.Kafka.GiftTopic)

	api := httpapi.New(httpapi.Deps{
		Log: log, MySQL: mysql, Redis: redis, Centrifugo: publisher, Metrics: metrics,
		Auth: authService, AppTokens: appTokens, CFTokens: cfTokens, CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL,
		Rooms: roomService, Danmaku: danmakuService, Likes: likeService, Viewers: viewerService, Stats: statsService,
		Gifts: giftService, Wallet: walletService,
	})

	srv := &http.Server{Addr: cfg.HTTP.Addr, Handler: api.Handler(), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("api started", "addr", cfg.HTTP.Addr, "milestone", "M6")
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Info("shutdown signal", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "error", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown", "error", err)
	}
}

func shutdownTracing(log *slog.Logger, fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fn(ctx); err != nil {
		log.Warn("shutdown tracing", "error", err)
	}
}
