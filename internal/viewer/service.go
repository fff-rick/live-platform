package viewer

import (
	"context"
	"time"
)

type Store interface {
	TouchViewer(context.Context, int64, int64, time.Duration) (int64, error)
}

type Service struct {
	store Store
	ttl   time.Duration
}

func NewService(store Store, ttl time.Duration) *Service {
	return &Service{store: store, ttl: ttl}
}

type Result struct {
	ViewerCount int64 `json:"viewer_count"`
	ExpiresIn   int64 `json:"expires_in_seconds"`
}

func (s *Service) Touch(ctx context.Context, roomID, userID int64) (Result, error) {
	count, err := s.store.TouchViewer(ctx, roomID, userID, s.ttl)
	if err != nil {
		return Result{}, err
	}
	return Result{ViewerCount: count, ExpiresIn: int64(s.ttl / time.Second)}, nil
}
