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

	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/querymodel"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
	"github.com/example/live-platform/internal/viewer"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	shutdownTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{
		Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-query", Environment: cfg.Observability.Environment,
		Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio,
	})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()

	metrics := observability.NewMetrics("live-query")
	db, err := mysqlstore.Open(cfg.Databases.QueryDSN, mysqlstore.Config{
		MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime,
	})
	if err != nil {
		log.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	metrics.RegisterDBPool("mysql", db.Stats)

	redis, err := redisstore.Open(redisstore.Config{
		URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards,
	})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer func() { _ = redis.Close() }()

	query := httpapi.NewQuery(httpapi.Deps{
		Log: log, MySQL: db, History: db, Redis: redis, Metrics: metrics,
		Profiles: querymodel.NewRepository(db),
		Viewers:  viewer.NewService(redis, cfg.Engagement.ViewerTTL), Stats: stats.NewService(stats.NewRedisStore(redis)),
	})
	srv := &http.Server{Addr: cfg.Query.HTTPAddr, Handler: query, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("query started", "addr", cfg.Query.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("query server", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("query shutdown", "error", err)
	}
}
