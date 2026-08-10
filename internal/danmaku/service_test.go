package danmaku

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/room"
)

type fakeRooms struct {
	muted  bool
	banned bool
	status room.Status
}

func (f fakeRooms) Get(context.Context, int64) (room.Room, error) {
	return room.Room{ID: 1, Status: f.status}, nil
}
func (f fakeRooms) IsMuted(context.Context, int64, int64) (bool, error)  { return f.muted, nil }
func (f fakeRooms) IsBanned(context.Context, int64, int64) (bool, error) { return f.banned, nil }

type fakeUsers struct{}

func (fakeUsers) User(context.Context, int64) (auth.User, error) {
	return auth.User{ID: 7, Nickname: "tester"}, nil
}

type fakeLimiter struct {
	allowed bool
	err     error
}

func (f fakeLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return f.allowed, f.err
}

type fakePublisher struct{ count int }

func (f *fakePublisher) Publish(context.Context, string, any) error { f.count++; return nil }

func TestSendDanmakuSuccess(t *testing.T) {
	p := &fakePublisher{}
	s := NewService(fakeRooms{status: room.StatusLiving}, fakeUsers{}, fakeLimiter{allowed: true}, NewSensitiveFilter([]string{"bad"}), p, NoopProducer{})
	e, err := s.Send(context.Background(), 1, 7, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if e.Content != "hello" || e.Nickname != "tester" {
		t.Fatalf("event=%+v", e)
	}
	if p.count != 1 {
		t.Fatalf("publish count=%d", p.count)
	}
}

func TestSendDanmakuGuards(t *testing.T) {
	tests := []struct {
		name    string
		rooms   fakeRooms
		limiter fakeLimiter
		text    string
		want    error
	}{
		{"invalid", fakeRooms{status: room.StatusLiving}, fakeLimiter{allowed: true}, "", ErrInvalidContent},
		{"closed", fakeRooms{status: room.StatusClosed}, fakeLimiter{allowed: true}, "hello", room.ErrNotLiving},
		{"banned", fakeRooms{status: room.StatusLiving, banned: true}, fakeLimiter{allowed: true}, "hello", room.ErrBanned},
		{"muted", fakeRooms{status: room.StatusLiving, muted: true}, fakeLimiter{allowed: true}, "hello", ErrMuted},
		{"rate", fakeRooms{status: room.StatusLiving}, fakeLimiter{allowed: false}, "hello", ErrRateLimited},
		{"sensitive", fakeRooms{status: room.StatusLiving}, fakeLimiter{allowed: true}, "contains bad word", ErrSensitive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(tc.rooms, fakeUsers{}, tc.limiter, NewSensitiveFilter([]string{"bad"}), &fakePublisher{}, NoopProducer{})
			_, err := s.Send(context.Background(), 1, 7, tc.text)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}
