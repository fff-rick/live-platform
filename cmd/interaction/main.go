// live-interaction is the Stage 3 high-frequency interaction boundary.
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
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/identityclient"
	"github.com/example/live-platform/internal/like"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/redisstore"
	cftoken "github.com/example/live-platform/internal/token"
	"github.com/example/live-platform/internal/traffic"
	"github.com/example/live-platform/internal/viewer"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	shutdownTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-interaction", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()
	metrics := observability.NewMetrics("live-interaction")
	redis, err := redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redis.Close(); err != nil {
			log.Error("close redis", "error", err)
		}
	}()
	producer, err := mq.NewProducerWithConfig(mq.ClientConfig{Brokers: cfg.Kafka.Brokers, TLSEnabled: cfg.Kafka.TLSEnabled, SASLMechanism: cfg.Kafka.SASLMechanism, SASLUsername: cfg.Kafka.SASLUsername, SASLPassword: cfg.Kafka.SASLPassword}, log, metrics)
	if err != nil {
		log.Error("create kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()
	identity := identityclient.New(cfg.IdentityRoom.BaseURL)
	limiter := redisstore.NewFixedWindowLimiter(redis)
	policy := traffic.NewPolicy(redis, traffic.Config{HotViewers: cfg.Traffic.HotViewers, ProtectViewers: cfg.Traffic.ProtectViewers, HotDanmakuRate: cfg.Traffic.HotDanmakuRate, ProtectDanmakuRate: cfg.Traffic.ProtectDanmakuRate, HotSampleRate: cfg.Traffic.HotSampleRate, ProtectSampleRate: cfg.Traffic.ProtectSampleRate, RateWindow: cfg.Traffic.RateWindow, AdaptiveEnabled: cfg.Traffic.AdaptiveEnabled, TargetFanoutRate: cfg.Traffic.TargetFanoutRate, HotFanoutRate: cfg.Traffic.HotFanoutRate, ProtectFanoutRate: cfg.Traffic.ProtectFanoutRate, MinSampleRate: cfg.Traffic.MinSampleRate}, metrics)
	publisher := realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, metrics)
	h := httpapi.NewInteraction(httpapi.Deps{Log: log, Redis: redis, Centrifugo: publisher, Metrics: metrics, AppTokens: auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL), CFTokens: cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL), CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL, Rooms: identity, Danmaku: danmaku.NewService(identity, identity, limiter, danmaku.NewSensitiveFilter(cfg.Danmaku.SensitiveWords), publisher, danmaku.NewKafkaProducer(producer, cfg.Kafka.DanmakuTopic, log), cfg.Danmaku.UserRateLimit, cfg.Danmaku.UserRateWindow, policy), Likes: like.NewService(identity, redis, like.Config{UserRateLimit: cfg.Like.UserRateLimit, UserRateWindow: cfg.Like.UserRateWindow, RoomCacheTTL: cfg.Like.RoomCacheTTL, BanCacheTTL: cfg.Like.BanCacheTTL}), Viewers: viewer.NewService(redis, cfg.Engagement.ViewerTTL), Stats: stats.NewService(stats.NewRedisStore(redis))})
	srv := &http.Server{Addr: cfg.Interaction.HTTPAddr, Handler: h, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("interaction started", "addr", cfg.Interaction.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case e := <-errCh:
		if !errors.Is(e, http.ErrServerClosed) {
			log.Error("http server", "error", e)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown", "error", err)
	}
}
