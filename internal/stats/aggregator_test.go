package stats

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	rooms []int64
	delta map[int64]int64
	snap  map[int64]Snapshot
	last  map[int64]int64
}

func (f *fakeStore) ActiveRooms(context.Context, time.Time, int64) ([]int64, error) {
	return f.rooms, nil
}
func (f *fakeStore) TakeLikeDelta(_ context.Context, id int64) (int64, error) {
	d := f.delta[id]
	f.delta[id] = 0
	return d, nil
}
func (f *fakeStore) RestoreLikeDelta(_ context.Context, id, delta int64) error {
	f.delta[id] += delta
	return nil
}
func (f *fakeStore) Snapshot(_ context.Context, id int64) (Snapshot, error) { return f.snap[id], nil }
func (f *fakeStore) LastPublishedViewer(_ context.Context, id int64) (int64, bool, error) {
	v, ok := f.last[id]
	return v, ok, nil
}
func (f *fakeStore) SetPublishedViewer(_ context.Context, id, v int64, _ time.Duration) error {
	if f.last == nil {
		f.last = map[int64]int64{}
	}
	f.last[id] = v
	return nil
}

type fakePublisher struct{ count int }

func (f *fakePublisher) Publish(context.Context, string, any) error { f.count++; return nil }

func TestAggregatorPublishesDeltaAndViewerChanges(t *testing.T) {
	store := &fakeStore{rooms: []int64{1}, delta: map[int64]int64{1: 10}, snap: map[int64]Snapshot{1: {ViewerCount: 2, LikeCount: 100}}}
	pub := &fakePublisher{}
	a := NewAggregator(slog.New(slog.NewTextHandler(io.Discard, nil)), store, pub, time.Second, time.Minute, 100)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.count != 1 {
		t.Fatalf("publish count=%d", pub.count)
	}
	if err := a.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.count != 1 {
		t.Fatalf("unchanged snapshot should not publish, count=%d", pub.count)
	}
	store.snap[1] = Snapshot{ViewerCount: 3, LikeCount: 100}
	if err := a.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.count != 2 {
		t.Fatalf("viewer change should publish, count=%d", pub.count)
	}
}
