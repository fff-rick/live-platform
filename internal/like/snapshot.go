package like

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// SnapshotSource provides the current Redis count for recently active rooms.
// The snapshot is a recovery checkpoint, not the synchronous source of truth.
type SnapshotSource interface {
	ActiveRooms(context.Context, time.Time, int64) ([]int64, error)
	LikeSnapshot(context.Context, int64) (int64, error)
	RestoreLikeSnapshot(context.Context, int64, int64) error
}

type SnapshotDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type Checkpoint struct {
	RoomID    int64
	LikeCount int64
}

type CheckpointStore interface {
	LoadAfter(context.Context, int64, int64) ([]Checkpoint, error)
	Save(context.Context, int64, int64) error
}

type MySQLCheckpointStore struct {
	db SnapshotDB
}

func NewMySQLCheckpointStore(db SnapshotDB) *MySQLCheckpointStore {
	return &MySQLCheckpointStore{db: db}
}

func (s *MySQLCheckpointStore) LoadAfter(ctx context.Context, afterRoomID, limit int64) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT room_id, like_count FROM room_like_snapshots
WHERE room_id > ? ORDER BY room_id LIMIT ?`, afterRoomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checkpoints := make([]Checkpoint, 0)
	for rows.Next() {
		var checkpoint Checkpoint
		if err := rows.Scan(&checkpoint.RoomID, &checkpoint.LikeCount); err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checkpoints, nil
}

func (s *MySQLCheckpointStore) Save(ctx context.Context, roomID, count int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO room_like_snapshots(room_id, like_count, updated_at)
VALUES (?, ?, NOW(3)) ON DUPLICATE KEY UPDATE like_count=GREATEST(like_count, VALUES(like_count)), updated_at=VALUES(updated_at)`, roomID, count)
	return err
}

type Snapshotter struct {
	log                    *slog.Logger
	source                 SnapshotSource
	checkpoints            CheckpointStore
	interval, activeWindow time.Duration
	batch                  int64
}

func NewSnapshotter(log *slog.Logger, source SnapshotSource, checkpoints CheckpointStore, interval, activeWindow time.Duration, batch int64) *Snapshotter {
	return &Snapshotter{log: log, source: source, checkpoints: checkpoints, interval: interval, activeWindow: activeWindow, batch: batch}
}

func (s *Snapshotter) Run(ctx context.Context) error {
	if err := s.Recover(ctx); err != nil {
		return fmt.Errorf("recover like snapshots: %w", err)
	}
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

// Recover 在定时快照启动前用 MySQL 检查点补齐缺失的 Redis 点赞总数。
func (s *Snapshotter) Recover(ctx context.Context) error {
	batch := s.batch
	if batch <= 0 {
		batch = 1000
	}
	var recoveryErrors []error
	var afterRoomID int64
	for {
		checkpoints, err := s.checkpoints.LoadAfter(ctx, afterRoomID, batch)
		if err != nil {
			recoveryErrors = append(recoveryErrors, err)
			return errors.Join(recoveryErrors...)
		}
		for _, checkpoint := range checkpoints {
			if err := s.source.RestoreLikeSnapshot(ctx, checkpoint.RoomID, checkpoint.LikeCount); err != nil {
				s.log.ErrorContext(ctx, "restore like snapshot", "room_id", checkpoint.RoomID, "error", err)
				recoveryErrors = append(recoveryErrors, fmt.Errorf("room %d: %w", checkpoint.RoomID, err))
			}
			afterRoomID = checkpoint.RoomID
		}
		if int64(len(checkpoints)) < batch {
			break
		}
	}
	return errors.Join(recoveryErrors...)
}

func (s *Snapshotter) Tick(ctx context.Context) error {
	rooms, err := s.source.ActiveRooms(ctx, time.Now().Add(-s.activeWindow), s.batch)
	if err != nil {
		return err
	}
	var snapshotErrors []error
	for _, roomID := range rooms {
		count, err := s.source.LikeSnapshot(ctx, roomID)
		if err != nil {
			s.log.ErrorContext(ctx, "read like snapshot", "room_id", roomID, "error", err)
			snapshotErrors = append(snapshotErrors, fmt.Errorf("room %d read: %w", roomID, err))
			continue
		}
		if err := s.checkpoints.Save(ctx, roomID, count); err != nil {
			s.log.ErrorContext(ctx, "write like snapshot", "room_id", roomID, "error", err)
			snapshotErrors = append(snapshotErrors, fmt.Errorf("room %d write: %w", roomID, err))
		}
	}
	return errors.Join(snapshotErrors...)
}
