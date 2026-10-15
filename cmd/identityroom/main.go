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
	pool := mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime}
	identityDB, err := mysqlstore.Open(cfg.Databases.IdentityDSN, pool)
	if err != nil {
		log.Error("open identity mysql", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := identityDB.Close(); err != nil {
			log.Error("close identity mysql", "error", err)
		}
	}()
	roomDB, err := mysqlstore.Open(cfg.Databases.RoomDSN, pool)
	if err != nil {
		log.Error("open room mysql", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := roomDB.Close(); err != nil {
			log.Error("close room mysql", "error", err)
		}
	}()
	m.RegisterDBPool("identity-mysql", identityDB.Stats)
	m.RegisterDBPool("room-mysql", roomDB.Stats)
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
	tokens := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	authService := auth.NewService(auth.NewRepository(identityDB), tokens)
	h := httpapi.NewIdentityRoom(httpapi.Deps{Log: log, MySQL: databasePingers{identityDB, roomDB}, Redis: redis, Centrifugo: realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, m), Metrics: m, Auth: authService, AppTokens: tokens, CFTokens: cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL), CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL, Rooms: room.NewService(room.NewRepository(roomDB), authService), Stats: stats.NewService(stats.NewRedisStore(redis))})
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

type databasePingers []*mysqlstore.Store

func (stores databasePingers) Ping(ctx context.Context) error {
	for _, store := range stores {
		if err := store.Ping(ctx); err != nil {
			return err
		}
	}
	return nil
}
