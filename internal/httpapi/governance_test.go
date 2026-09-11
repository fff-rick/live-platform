package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/example/live-platform/internal/realtime"
)

type fakeGovernanceStore struct {
	roomID, userID int64
	banned         bool
	removed        bool
	err            error
}

func (f *fakeGovernanceStore) RemoveViewer(_ context.Context, roomID, userID int64) (int64, error) {
	f.roomID, f.userID, f.removed = roomID, userID, true
	return 0, f.err
}

func (f *fakeGovernanceStore) EnforceLikeBan(_ context.Context, roomID, userID int64, banned bool) error {
	f.roomID, f.userID, f.banned = roomID, userID, banned
	return f.err
}

type fakeRealtimeController struct {
	publishedChannel string
	publishedEvent   realtime.Event
	unsubscribed     []string
	err              error
}

func (f *fakeRealtimeController) Ping(context.Context) error { return nil }
func (f *fakeRealtimeController) Publish(_ context.Context, channel string, value any) error {
	f.publishedChannel = channel
	f.publishedEvent, _ = value.(realtime.Event)
	return f.err
}
func (f *fakeRealtimeController) Unsubscribe(_ context.Context, userID, channel string) error {
	f.unsubscribed = append(f.unsubscribed, userID+"@"+channel)
	return f.err
}

func TestEnforceBanEvictsViewerAndRoomSubscriptions(t *testing.T) {
	governance := &fakeGovernanceStore{}
	rt := &fakeRealtimeController{}
	s := &Server{governance: governance, centrifugo: rt}

	if err := s.enforceBan(context.Background(), 7, 42, "spam"); err != nil {
		t.Fatal(err)
	}
	if !governance.banned || !governance.removed || governance.roomID != 7 || governance.userID != 42 {
		t.Fatalf("governance=%+v", governance)
	}
	if rt.publishedChannel != realtime.Personal(42) || rt.publishedEvent.Type != "room_banned" {
		t.Fatalf("channel=%q event=%+v", rt.publishedChannel, rt.publishedEvent)
	}
	want := map[string]bool{
		"42@" + realtime.RoomStream(7): true,
		"42@" + realtime.RoomStats(7):  true,
	}
	for _, item := range rt.unsubscribed {
		delete(want, item)
	}
	if len(want) != 0 {
		t.Fatalf("missing unsubscribe calls=%v", want)
	}
}

func TestEnforceBanAggregatesSideEffectFailures(t *testing.T) {
	sideEffectErr := errors.New("dependency unavailable")
	s := &Server{
		governance: &fakeGovernanceStore{err: sideEffectErr},
		centrifugo: &fakeRealtimeController{err: sideEffectErr},
	}
	if err := s.enforceBan(context.Background(), 7, 42, ""); !errors.Is(err, sideEffectErr) {
		t.Fatalf("err=%v", err)
	}
}
