package main

import (
	"context"
	"errors"
	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
	cftoken "github.com/example/live-platform/internal/token"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	closeTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-identity-room", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = closeTrace(ctx)
	}()
	m := observability.NewMetrics("live-identity-room")
	db, err := mysqlstore.Open(cfg.MySQL.DSN, mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime})
	if err != nil {
		log.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	m.RegisterDBPool("mysql", db.Stats)
	redis, err := redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer redis.Close()
	tokens := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	h := httpapi.NewIdentityRoom(httpapi.Deps{Log: log, MySQL: db, Redis: redis, Centrifugo: realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, m), Metrics: m, Auth: auth.NewService(auth.NewRepository(db), tokens), AppTokens: tokens, CFTokens: cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL), CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL, Rooms: room.NewService(room.NewRepository(db)), Stats: stats.NewService(stats.NewRedisStore(redis))})
	srv := &http.Server{Addr: cfg.IdentityRoom.HTTPAddr, Handler: h, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	ch := make(chan error, 1)
	go func() {
		log.Info("identity-room started", "addr", cfg.IdentityRoom.HTTPAddr)
		ch <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case e := <-ch:
		if !errors.Is(e, http.ErrServerClosed) {
			log.Error("http server", "error", e)
		}
	}
	stopctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(stopctx); err != nil {
		log.Error("shutdown", "error", err)
	}
}
