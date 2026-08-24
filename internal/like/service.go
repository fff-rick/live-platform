package like

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/live-platform/internal/room"
)

var ErrInvalidCount = errors.New("like count must be between 1 and 100")
var ErrRateLimited = errors.New("like rate limit exceeded")

type Store interface {
	AddLikes(context.Context, int64, int64) (int64, error)
}

type RoomService interface {
	Get(context.Context, int64) (room.Room, error)
	IsBanned(context.Context, int64, int64) (bool, error)
}
type Limiter interface {
	AllowN(context.Context, string, int64, int64, time.Duration) (bool, error)
}
type EligibilityCache interface {
	CachedLikeRoomStatus(context.Context, int64) (string, bool, error)
	CacheLikeRoomStatus(context.Context, int64, string, time.Duration) error
	CachedLikeBan(context.Context, int64, int64) (bool, bool, error)
	CacheLikeBan(context.Context, int64, int64, bool, time.Duration) error
}

type Config struct {
	UserRateLimit  int64
	UserRateWindow time.Duration
	RoomCacheTTL   time.Duration
	BanCacheTTL    time.Duration
}

type Service struct {
	rooms   RoomService
	store   Store
	limiter Limiter
	cache   EligibilityCache
	config  Config
}

func NewService(rooms RoomService, store Store, config ...Config) *Service {
	s := &Service{rooms: rooms, store: store}
	if len(config) > 0 {
		s.config = config[0]
	}
	if v, ok := store.(Limiter); ok {
		s.limiter = v
	}
	if v, ok := store.(EligibilityCache); ok {
		s.cache = v
	}
	return s
}

type Result struct {
	Accepted int64 `json:"accepted"`
	Total    int64 `json:"total"`
}

func (s *Service) Add(ctx context.Context, roomID, userID, count int64) (Result, error) {
	if count < 1 || count > 100 {
		return Result{}, ErrInvalidCount
	}
	status, err := s.roomStatus(ctx, roomID)
	if err != nil {
		return Result{}, err
	}
	if status != room.StatusLiving {
		return Result{}, room.ErrNotLiving
	}
	banned, err := s.isBanned(ctx, roomID, userID)
	if err != nil {
		return Result{}, err
	}
	if banned {
		return Result{}, room.ErrBanned
	}
	if s.limiter != nil && s.config.UserRateLimit > 0 {
		allowed, err := s.limiter.AllowN(ctx, fmt.Sprintf("live:room:{%d}:like:rate:%d", roomID, userID), s.config.UserRateLimit, count, s.config.UserRateWindow)
		if err != nil {
			return Result{}, err
		}
		if !allowed {
			return Result{}, ErrRateLimited
		}
	}
	total, err := s.store.AddLikes(ctx, roomID, count)
	if err != nil {
		return Result{}, err
	}
	return Result{Accepted: count, Total: total}, nil
}

func (s *Service) roomStatus(ctx context.Context, roomID int64) (room.Status, error) {
	if s.cache != nil {
		if raw, ok, err := s.cache.CachedLikeRoomStatus(ctx, roomID); err != nil {
			return "", err
		} else if ok {
			return room.Status(raw), nil
		}
	}
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return "", err
	}
	if s.cache != nil && s.config.RoomCacheTTL > 0 {
		_ = s.cache.CacheLikeRoomStatus(ctx, roomID, string(v.Status), s.config.RoomCacheTTL)
	}
	return v.Status, nil
}

func (s *Service) isBanned(ctx context.Context, roomID, userID int64) (bool, error) {
	if s.cache != nil {
		if banned, ok, err := s.cache.CachedLikeBan(ctx, roomID, userID); err != nil {
			return false, err
		} else if ok {
			return banned, nil
		}
	}
	banned, err := s.rooms.IsBanned(ctx, roomID, userID)
	if err != nil {
		return false, err
	}
	if s.cache != nil && s.config.BanCacheTTL > 0 {
		_ = s.cache.CacheLikeBan(ctx, roomID, userID, banned, s.config.BanCacheTTL)
	}
	return banned, nil
}
