package gift

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/room"
)

var (
	ErrInvalidCount     = errors.New("gift count is outside the allowed range")
	ErrInvalidRequestID = errors.New("invalid idempotency key")
	ErrOrderForbidden   = errors.New("gift order does not belong to user")
	ErrRateLimited      = errors.New("gift request rate limited")
)

type Store interface {
	ListActive(context.Context) ([]Gift, error)
	ByOrderNo(context.Context, string) (Order, error)
	ByRequestID(context.Context, string) (Order, error)
	Create(context.Context, CreateParams) (Order, bool, error)
}

type RoomService interface {
	Get(context.Context, int64) (room.Room, error)
	IsBanned(context.Context, int64, int64) (bool, error)
}

type Limiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}

type Config struct {
	MaxCountPerRequest int64
	UserRateLimit      int
	UserRateWindow     time.Duration
}

type Service struct {
	store     Store
	rooms     RoomService
	limiter   Limiter
	cfg       Config
	giftTopic string
}

func NewService(store Store, rooms RoomService, limiter Limiter, cfg Config, giftTopic string) *Service {
	if cfg.MaxCountPerRequest <= 0 {
		cfg.MaxCountPerRequest = 100
	}
	if cfg.UserRateLimit <= 0 {
		cfg.UserRateLimit = 10
	}
	if cfg.UserRateWindow <= 0 {
		cfg.UserRateWindow = time.Second
	}
	return &Service{store: store, rooms: rooms, limiter: limiter, cfg: cfg, giftTopic: giftTopic}
}

func (s *Service) List(ctx context.Context) ([]Gift, error) { return s.store.ListActive(ctx) }

func (s *Service) Order(ctx context.Context, orderNo string, userID int64) (Order, error) {
	v, err := s.store.ByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return Order{}, err
	}
	if v.UserID != userID {
		return Order{}, ErrOrderForbidden
	}
	return v, nil
}

func (s *Service) Send(ctx context.Context, roomID, userID, giftID, count int64, requestID string) (SendResult, error) {
	ctx, span := otel.Tracer("live-platform/gift").Start(ctx, "gift.transaction")
	span.SetAttributes(attribute.Int64("live.room_id", roomID), attribute.Int64("live.user_id", userID), attribute.Int64("live.gift_id", giftID), attribute.Int64("live.gift_count", count))
	defer span.End()

	if count < 1 || count > s.cfg.MaxCountPerRequest {
		return SendResult{}, ErrInvalidCount
	}
	requestID = strings.TrimSpace(requestID)
	if !validRequestID(requestID) {
		return SendResult{}, ErrInvalidRequestID
	}

	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		if replay, ok, replayErr := s.replay(ctx, roomID, userID, giftID, count, requestID); ok || replayErr != nil {
			return replay, replayErr
		}
		return SendResult{}, err
	}
	if v.Status != room.StatusLiving {
		if replay, ok, replayErr := s.replay(ctx, roomID, userID, giftID, count, requestID); ok || replayErr != nil {
			return replay, replayErr
		}
		return SendResult{}, room.ErrNotLiving
	}
	banned, err := s.rooms.IsBanned(ctx, roomID, userID)
	if err != nil {
		return SendResult{}, err
	}
	if banned {
		if replay, ok, replayErr := s.replay(ctx, roomID, userID, giftID, count, requestID); ok || replayErr != nil {
			return replay, replayErr
		}
		return SendResult{}, room.ErrBanned
	}

	if s.limiter != nil {
		allowed, err := s.limiter.Allow(ctx, "live:limit:gift:user:"+strconv.FormatInt(userID, 10), s.cfg.UserRateLimit, s.cfg.UserRateWindow)
		if err != nil {
			return SendResult{}, err
		}
		if !allowed {
			if replay, ok, replayErr := s.replay(ctx, roomID, userID, giftID, count, requestID); ok || replayErr != nil {
				return replay, replayErr
			}
			return SendResult{}, ErrRateLimited
		}
	}

	id := idgen.New()
	order, replay, err := s.store.Create(ctx, CreateParams{
		OrderNo: "G" + id, RequestID: requestID, TransactionNo: "WT" + idgen.New(), EventID: idgen.New(), GiftTopic: s.giftTopic,
		UserID: userID, AnchorID: v.AnchorID, RoomID: roomID, GiftID: giftID, Count: count,
	})
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Order: order, IdempotentReplay: replay, EventQueued: !replay}, nil
}

func (s *Service) replay(ctx context.Context, roomID, userID, giftID, count int64, requestID string) (SendResult, bool, error) {
	existing, err := s.store.ByRequestID(ctx, requestID)
	if errors.Is(err, ErrOrderNotFound) {
		return SendResult{}, false, nil
	}
	if err != nil {
		return SendResult{}, false, err
	}
	if !sameRequest(existing, roomID, userID, giftID, count) {
		return SendResult{}, false, ErrIdempotencyConflict
	}
	return SendResult{Order: existing, IdempotentReplay: true, EventQueued: false}, true, nil
}

func sameRequest(existing Order, roomID, userID, giftID, count int64) bool {
	return existing.UserID == userID && existing.RoomID == roomID && existing.GiftID == giftID && existing.GiftCount == count
}

func validRequestID(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':' || r == '.' {
			continue
		}
		return false
	}
	return true
}
