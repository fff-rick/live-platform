package like

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/live-platform/internal/room"
)

type fakeRooms struct {
	status room.Status
	banned bool
}

func (f fakeRooms) Get(context.Context, int64) (room.Room, error) {
	return room.Room{ID: 1, Status: f.status}, nil
}
func (f fakeRooms) IsBanned(context.Context, int64, int64) (bool, error) { return f.banned, nil }

type fakeStore struct{ total int64 }

func (f *fakeStore) AddLikes(_ context.Context, _ int64, count int64) (int64, error) {
	f.total += count
	return f.total, nil
}

type limitedStore struct {
	fakeStore
	allowed  bool
	consumed int64
}

func (s *limitedStore) AllowN(_ context.Context, _ string, _ int64, n int64, _ time.Duration) (bool, error) {
	s.consumed += n
	return s.allowed, nil
}

type cachedStore struct {
	fakeStore
	status            string
	banned            bool
	statusHit, banHit bool
}

func (s *cachedStore) AllowN(context.Context, string, int64, int64, time.Duration) (bool, error) {
	return true, nil
}
func (s *cachedStore) CachedLikeRoomStatus(context.Context, int64) (string, bool, error) {
	return s.status, s.statusHit, nil
}
func (s *cachedStore) CacheLikeRoomStatus(_ context.Context, _ int64, v string, _ time.Duration) error {
	s.status, s.statusHit = v, true
	return nil
}
func (s *cachedStore) CachedLikeBan(context.Context, int64, int64) (bool, bool, error) {
	return s.banned, s.banHit, nil
}
func (s *cachedStore) CacheLikeBan(_ context.Context, _ int64, _ int64, v bool, _ time.Duration) error {
	s.banned, s.banHit = v, true
	return nil
}

func TestAddLikes(t *testing.T) {
	st := &fakeStore{total: 10}
	s := NewService(fakeRooms{status: room.StatusLiving}, st)
	got, err := s.Add(context.Background(), 1, 7, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accepted != 5 || got.Total != 15 {
		t.Fatalf("got=%+v", got)
	}
}

func TestAddLikesRateLimitedByLogicalClicks(t *testing.T) {
	st := &limitedStore{allowed: false}
	s := NewService(fakeRooms{status: room.StatusLiving}, st, Config{UserRateLimit: 10, UserRateWindow: time.Second})
	_, err := s.Add(context.Background(), 1, 7, 8)
	if !errors.Is(err, ErrRateLimited) || st.consumed != 8 {
		t.Fatalf("err=%v consumed=%d", err, st.consumed)
	}
}

func TestAddLikesUsesEligibilityCache(t *testing.T) {
	st := &cachedStore{status: string(room.StatusLiving), banned: false, statusHit: true, banHit: true}
	s := NewService(fakeRooms{status: room.StatusClosed, banned: true}, st, Config{RoomCacheTTL: time.Second, BanCacheTTL: time.Second})
	got, err := s.Add(context.Background(), 1, 7, 2)
	if err != nil || got.Total != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestAddLikesGuards(t *testing.T) {
	cases := []struct {
		name  string
		count int64
		rooms fakeRooms
		want  error
	}{
		{"zero", 0, fakeRooms{status: room.StatusLiving}, ErrInvalidCount},
		{"too_many", 101, fakeRooms{status: room.StatusLiving}, ErrInvalidCount},
		{"closed", 1, fakeRooms{status: room.StatusClosed}, room.ErrNotLiving},
		{"banned", 1, fakeRooms{status: room.StatusLiving, banned: true}, room.ErrBanned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewService(tc.rooms, &fakeStore{}).Add(context.Background(), 1, 7, tc.count)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}
