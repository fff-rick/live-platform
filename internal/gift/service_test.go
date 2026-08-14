package gift

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/live-platform/internal/room"
)

type fakeStore struct {
	order         Order
	replay        bool
	err           error
	request       Order
	requestErr    error
	requestChecks int
	last          CreateParams
}

func (f *fakeStore) ListActive(context.Context) ([]Gift, error) {
	return []Gift{{ID: 1, Name: "Rose", Price: 100, Status: 1}}, nil
}
func (f *fakeStore) ByOrderNo(context.Context, string) (Order, error) { return f.order, f.err }
func (f *fakeStore) ByRequestID(context.Context, string) (Order, error) {
	f.requestChecks++
	if f.requestErr != nil {
		return Order{}, f.requestErr
	}
	if f.request.OrderNo == "" {
		return Order{}, ErrOrderNotFound
	}
	return f.request, nil
}
func (f *fakeStore) Create(_ context.Context, p CreateParams) (Order, bool, error) {
	f.last = p
	return f.order, f.replay, f.err
}

type fakeRooms struct {
	v      room.Room
	banned bool
	err    error
}

func (f fakeRooms) Get(context.Context, int64) (room.Room, error)        { return f.v, f.err }
func (f fakeRooms) IsBanned(context.Context, int64, int64) (bool, error) { return f.banned, f.err }

type fakeLimiter struct{ allowed bool }

func (f fakeLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return f.allowed, nil
}

func testConfig() Config {
	return Config{MaxCountPerRequest: 100, UserRateLimit: 10, UserRateWindow: time.Second}
}

func TestSendQueuesGiftEventInTransaction(t *testing.T) {
	store := &fakeStore{order: Order{OrderNo: "G1", UserID: 7, RoomID: 9, GiftID: 1, GiftName: "Rose", GiftCount: 2, TotalAmount: 200, Status: "SUCCESS"}}
	svc := NewService(store, fakeRooms{v: room.Room{ID: 9, AnchorID: 11, Status: room.StatusLiving}}, fakeLimiter{allowed: true}, testConfig(), "live.gift.v1")

	got, err := svc.Send(context.Background(), 9, 7, 1, 2, "req-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if got.IdempotentReplay || !got.EventQueued {
		t.Fatalf("unexpected result: %+v", got)
	}
	if store.last.AnchorID != 11 || store.last.UserID != 7 || store.last.RoomID != 9 || store.last.EventID == "" || store.last.GiftTopic != "live.gift.v1" {
		t.Fatalf("bad params: %+v", store.last)
	}
	if store.requestChecks != 0 {
		t.Fatalf("normal accepted request should not pre-read idempotency row, checks=%d", store.requestChecks)
	}
}

func TestSendReplayBypassesRateLimit(t *testing.T) {
	existing := Order{OrderNo: "G1", RequestID: "req-12345678", UserID: 7, RoomID: 9, GiftID: 1, GiftCount: 1, Status: "SUCCESS"}
	store := &fakeStore{request: existing}
	svc := NewService(store, fakeRooms{}, fakeLimiter{allowed: false}, testConfig(), "live.gift.v1")
	got, err := svc.Send(context.Background(), 9, 7, 1, 1, "req-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IdempotentReplay || got.EventQueued || got.Order.OrderNo != "G1" {
		t.Fatalf("unexpected replay: %+v", got)
	}
}

func TestSendRejectsRateLimit(t *testing.T) {
	svc := NewService(&fakeStore{}, fakeRooms{v: room.Room{ID: 9, AnchorID: 11, Status: room.StatusLiving}}, fakeLimiter{allowed: false}, testConfig(), "live.gift.v1")
	_, err := svc.Send(context.Background(), 9, 7, 1, 1, "req-12345678")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err=%v", err)
	}
}

func TestSendRejectsInvalidInputAndAccess(t *testing.T) {
	cases := []struct {
		name  string
		count int64
		req   string
		rooms fakeRooms
		want  error
	}{
		{"count", 0, "req-12345678", fakeRooms{}, ErrInvalidCount},
		{"too many", 101, "req-12345678", fakeRooms{}, ErrInvalidCount},
		{"request", 1, "bad key!", fakeRooms{}, ErrInvalidRequestID},
		{"not living", 1, "req-12345678", fakeRooms{v: room.Room{Status: room.StatusClosed}}, room.ErrNotLiving},
		{"banned", 1, "req-12345678", fakeRooms{v: room.Room{Status: room.StatusLiving}, banned: true}, room.ErrBanned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&fakeStore{}, tc.rooms, fakeLimiter{allowed: true}, testConfig(), "live.gift.v1")
			_, err := svc.Send(context.Background(), 1, 2, 1, tc.count, tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestOrderOwnership(t *testing.T) {
	svc := NewService(&fakeStore{order: Order{OrderNo: "G1", UserID: 7}}, fakeRooms{}, fakeLimiter{allowed: true}, testConfig(), "live.gift.v1")
	if _, err := svc.Order(context.Background(), "G1", 8); !errors.Is(err, ErrOrderForbidden) {
		t.Fatalf("err=%v", err)
	}
}
