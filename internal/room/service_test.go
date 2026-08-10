package room

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepo struct {
	v      Room
	banned bool
}

func (f *fakeRepo) Create(_ context.Context, anchor int64, title string) (Room, error) {
	f.v = Room{ID: 1, AnchorID: anchor, Title: title, Status: StatusPreparing}
	return f.v, nil
}
func (f *fakeRepo) ByID(context.Context, int64) (Room, error) { return f.v, nil }
func (f *fakeRepo) ChangeStatus(_ context.Context, _ int64, _ int64, from, to Status) error {
	if f.v.Status != from {
		return ErrInvalidState
	}
	f.v.Status = to
	return nil
}
func (*fakeRepo) Join(context.Context, int64, int64) error                            { return nil }
func (*fakeRepo) IsMuted(context.Context, int64, int64) (bool, error)                 { return false, nil }
func (f *fakeRepo) IsBanned(context.Context, int64, int64) (bool, error)              { return f.banned, nil }
func (*fakeRepo) Ban(context.Context, int64, int64, int64, string) error              { return nil }
func (*fakeRepo) Unban(context.Context, int64, int64) error                           { return nil }
func (*fakeRepo) Mute(context.Context, int64, int64, int64, *time.Time, string) error { return nil }
func (*fakeRepo) Unmute(context.Context, int64, int64) error                          { return nil }

func TestRoomLifecycle(t *testing.T) {
	r := &fakeRepo{}
	s := NewService(r)
	v, err := s.Create(context.Background(), 7, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusPreparing {
		t.Fatal(v.Status)
	}
	v, err = s.Start(context.Background(), 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusLiving {
		t.Fatal(v.Status)
	}
	v, err = s.Stop(context.Background(), 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusClosed {
		t.Fatal(v.Status)
	}
}

func TestJoinRejectsBannedUser(t *testing.T) {
	r := &fakeRepo{v: Room{ID: 1, AnchorID: 7, Status: StatusLiving}, banned: true}
	s := NewService(r)
	if _, err := s.Join(context.Background(), 1, 9); !errors.Is(err, ErrBanned) {
		t.Fatalf("err=%v", err)
	}
}

func TestAnchorCannotModerateSelf(t *testing.T) {
	r := &fakeRepo{v: Room{ID: 1, AnchorID: 7, Status: StatusLiving}}
	s := NewService(r)
	if err := s.Mute(context.Background(), 1, 7, 7, time.Minute, ""); !errors.Is(err, ErrSelfModeration) {
		t.Fatalf("mute err=%v", err)
	}
	if err := s.Ban(context.Background(), 1, 7, 7, ""); !errors.Is(err, ErrSelfModeration) {
		t.Fatalf("ban err=%v", err)
	}
}
