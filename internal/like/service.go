package like

import (
	"context"
	"errors"

	"github.com/example/live-platform/internal/room"
)

var ErrInvalidCount = errors.New("like count must be between 1 and 100")

type Store interface {
	AddLikes(context.Context, int64, int64) (int64, error)
}

type RoomService interface {
	Get(context.Context, int64) (room.Room, error)
	IsBanned(context.Context, int64, int64) (bool, error)
}

type Service struct {
	rooms RoomService
	store Store
}

func NewService(rooms RoomService, store Store) *Service {
	return &Service{rooms: rooms, store: store}
}

type Result struct {
	Accepted int64 `json:"accepted"`
	Total    int64 `json:"total"`
}

func (s *Service) Add(ctx context.Context, roomID, userID, count int64) (Result, error) {
	if count < 1 || count > 100 {
		return Result{}, ErrInvalidCount
	}
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return Result{}, err
	}
	if v.Status != room.StatusLiving {
		return Result{}, room.ErrNotLiving
	}
	banned, err := s.rooms.IsBanned(ctx, roomID, userID)
	if err != nil {
		return Result{}, err
	}
	if banned {
		return Result{}, room.ErrBanned
	}
	total, err := s.store.AddLikes(ctx, roomID, count)
	if err != nil {
		return Result{}, err
	}
	return Result{Accepted: count, Total: total}, nil
}
