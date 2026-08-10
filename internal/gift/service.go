package gift

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/room"
)

var (
	ErrInvalidCount     = errors.New("gift count must be between 1 and 100")
	ErrInvalidRequestID = errors.New("invalid idempotency key")
	ErrOrderForbidden   = errors.New("gift order does not belong to user")
)

type Store interface {
	ListActive(context.Context) ([]Gift, error)
	ByOrderNo(context.Context, string) (Order, error)
	Create(context.Context, CreateParams) (Order, bool, error)
}

type RoomService interface {
	Get(context.Context, int64) (room.Room, error)
	IsBanned(context.Context, int64, int64) (bool, error)
}

type Service struct {
	store     Store
	rooms     RoomService
	giftTopic string
}

func NewService(store Store, rooms RoomService, giftTopic string) *Service {
	return &Service{store: store, rooms: rooms, giftTopic: giftTopic}
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
	if count < 1 || count > 100 {
		return SendResult{}, ErrInvalidCount
	}
	requestID = strings.TrimSpace(requestID)
	if !validRequestID(requestID) {
		return SendResult{}, ErrInvalidRequestID
	}
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return SendResult{}, err
	}
	if v.Status != room.StatusLiving {
		return SendResult{}, room.ErrNotLiving
	}
	banned, err := s.rooms.IsBanned(ctx, roomID, userID)
	if err != nil {
		return SendResult{}, err
	}
	if banned {
		return SendResult{}, room.ErrBanned
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
