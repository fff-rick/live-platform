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
	"github.com/example/live-platform/internal/commerceapi"
	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
	"github.com/example/live-platform/internal/viewer"
	"github.com/example/live-platform/internal/wallet"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	shutdownTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-commerce", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()
	metrics := observability.NewMetrics("live-commerce")
	mysql, err := mysqlstore.Open(cfg.MySQL.DSN, mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime})
	if err != nil {
		log.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer mysql.Close()
	metrics.RegisterDBPool("mysql", mysql.Stats)
	redis, err := redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer redis.Close()

	rooms := room.NewService(room.NewRepository(mysql)) // Transitional room read dependency; Stage 4 replaces it with a room read model/API.
	gifts := gift.NewService(gift.NewRepository(mysql), rooms, redisstore.NewFixedWindowLimiter(redis), gift.Config{MaxCountPerRequest: cfg.Gift.MaxCountPerRequest, UserRateLimit: cfg.Gift.UserRateLimit, UserRateWindow: cfg.Gift.UserRateWindow}, cfg.Kafka.GiftTopic)
	api := commerceapi.New(commerceapi.Deps{Log: log, Tokens: auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL), Gifts: gifts, Wallet: wallet.NewService(wallet.NewRepository(mysql), cfg.Wallet.DevCreditEnabled), Viewers: viewer.NewService(redis, cfg.Engagement.ViewerTTL), Metrics: metrics, MySQL: mysql, Redis: redis})
	srv := &http.Server{Addr: cfg.Commerce.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { log.Info("commerce started", "addr", cfg.Commerce.HTTPAddr); errCh <- srv.ListenAndServe() }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("commerce server", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("commerce shutdown", "error", err)
	}
}
