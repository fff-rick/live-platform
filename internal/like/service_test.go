package like

import (
	"context"
	"errors"
	"testing"

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
