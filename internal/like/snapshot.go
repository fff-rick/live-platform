package like

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// SnapshotSource provides the current Redis count for recently active rooms.
// The snapshot is a recovery checkpoint, not the synchronous source of truth.
type SnapshotSource interface {
	ActiveRooms(context.Context, time.Time, int64) ([]int64, error)
	LikeSnapshot(context.Context, int64) (int64, error)
}

type SnapshotWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Snapshotter struct {
	log                    *slog.Logger
	source                 SnapshotSource
	db                     SnapshotWriter
	interval, activeWindow time.Duration
	batch                  int64
}

func NewSnapshotter(log *slog.Logger, source SnapshotSource, db SnapshotWriter, interval, activeWindow time.Duration, batch int64) *Snapshotter {
	return &Snapshotter{log: log, source: source, db: db, interval: interval, activeWindow: activeWindow, batch: batch}
}

func (s *Snapshotter) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				s.log.ErrorContext(ctx, "snapshot likes", "error", err)
			}
		}
	}
}

func (s *Snapshotter) Tick(ctx context.Context) error {
	rooms, err := s.source.ActiveRooms(ctx, time.Now().Add(-s.activeWindow), s.batch)
	if err != nil {
		return err
	}
	for _, roomID := range rooms {
		count, err := s.source.LikeSnapshot(ctx, roomID)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO room_like_snapshots(room_id, like_count, updated_at)
VALUES (?, ?, NOW(3)) ON DUPLICATE KEY UPDATE like_count=GREATEST(like_count, VALUES(like_count)), updated_at=VALUES(updated_at)`, roomID, count); err != nil {
			return err
		}
	}
	return nil
}
