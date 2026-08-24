package like

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"
)

type snapshotSource struct{}

func (snapshotSource) ActiveRooms(context.Context, time.Time, int64) ([]int64, error) {
	return []int64{1}, nil
}
func (snapshotSource) LikeSnapshot(context.Context, int64) (int64, error) { return 42, nil }

type snapshotWriter struct{ roomID, count int64 }

func (w *snapshotWriter) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	w.roomID, w.count = args[0].(int64), args[1].(int64)
	return nil, nil
}

func TestSnapshotterWritesCurrentCount(t *testing.T) {
	w := &snapshotWriter{}
	s := NewSnapshotter(slog.New(slog.NewTextHandler(io.Discard, nil)), snapshotSource{}, w, time.Second, time.Minute, 10)
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.roomID != 1 || w.count != 42 {
		t.Fatalf("snapshot=%d/%d", w.roomID, w.count)
	}
}
