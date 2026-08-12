package stats

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/realtime"
)

type Store interface {
	ActiveRooms(context.Context, time.Time, int64) ([]int64, error)
	TakeLikeDelta(context.Context, int64) (int64, error)
	RestoreLikeDelta(context.Context, int64, int64) error
	Snapshot(context.Context, int64) (Snapshot, error)
	LastPublishedViewer(context.Context, int64) (int64, bool, error)
	SetPublishedViewer(context.Context, int64, int64, time.Duration) error
}

type Publisher interface {
	Publish(context.Context, string, any) error
}
type Metrics interface{ StatsBroadcasted(result string) }

type Snapshot struct {
	ViewerCount int64 `json:"viewer_count"`
	LikeCount   int64 `json:"like_count"`
	LikeDelta   int64 `json:"like_delta"`
}

type Aggregator struct {
	log          *slog.Logger
	store        Store
	publisher    Publisher
	metrics      Metrics
	interval     time.Duration
	activeWindow time.Duration
	batch        int64
}

func NewAggregator(log *slog.Logger, store Store, publisher Publisher, interval, activeWindow time.Duration, batch int64, metrics ...Metrics) *Aggregator {
	var m Metrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	return &Aggregator{log: log, store: store, publisher: publisher, metrics: m, interval: interval, activeWindow: activeWindow, batch: batch}
}

func (a *Aggregator) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := a.Tick(ctx); err != nil {
				a.log.ErrorContext(ctx, "stats aggregation failed", "error", err)
			}
		}
	}
}

func (a *Aggregator) Tick(ctx context.Context) error {
	rooms, err := a.store.ActiveRooms(ctx, time.Now().Add(-a.activeWindow), a.batch)
	if err != nil {
		return err
	}
	for _, roomID := range rooms {
		delta, err := a.store.TakeLikeDelta(ctx, roomID)
		if err != nil {
			a.log.ErrorContext(ctx, "take like delta", "room_id", roomID, "error", err)
			continue
		}
		snap, err := a.store.Snapshot(ctx, roomID)
		if err != nil {
			if delta > 0 {
				_ = a.store.RestoreLikeDelta(ctx, roomID, delta)
			}
			a.log.ErrorContext(ctx, "load room stats", "room_id", roomID, "error", err)
			continue
		}
		snap.LikeDelta = delta
		previous, seen, err := a.store.LastPublishedViewer(ctx, roomID)
		if err != nil {
			if delta > 0 {
				_ = a.store.RestoreLikeDelta(ctx, roomID, delta)
			}
			a.log.ErrorContext(ctx, "load last published viewer", "room_id", roomID, "error", err)
			continue
		}
		viewerChanged := !seen || previous != snap.ViewerCount
		if delta == 0 && !viewerChanged {
			continue
		}
		wire := realtime.NewPriorityEvent(idgen.New(), "stats", roomID, "P4", snap)
		if err := a.publisher.Publish(ctx, realtime.RoomStats(roomID), wire); err != nil {
			if delta > 0 {
				_ = a.store.RestoreLikeDelta(ctx, roomID, delta)
			}
			if a.metrics != nil {
				a.metrics.StatsBroadcasted("failed")
			}
			a.log.ErrorContext(ctx, "publish room stats", "room_id", roomID, "error", err)
			continue
		}
		if a.metrics != nil {
			a.metrics.StatsBroadcasted("success")
		}
		if err := a.store.SetPublishedViewer(ctx, roomID, snap.ViewerCount, 2*a.activeWindow); err != nil {
			a.log.ErrorContext(ctx, "store published viewer", "room_id", roomID, "error", err)
		}
	}
	return nil
}
