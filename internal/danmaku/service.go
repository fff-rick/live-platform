package danmaku

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/room"
)

var (
	ErrRateLimited    = errors.New("danmaku rate limited")
	ErrMuted          = errors.New("user is muted")
	ErrSensitive      = errors.New("danmaku contains sensitive content")
	ErrInvalidContent = errors.New("invalid danmaku content")
)

type Limiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}

type EventProducer interface {
	ProduceDanmaku(context.Context, Event) error
}

type RoomService interface {
	Get(context.Context, int64) (room.Room, error)
	IsMuted(context.Context, int64, int64) (bool, error)
	IsBanned(context.Context, int64, int64) (bool, error)
}

type Publisher interface {
	Publish(context.Context, string, any) error
}

type TrafficPolicy interface {
	Decide(context.Context, int64, string) (mode string, broadcast bool, err error)
}

type UserService interface {
	User(context.Context, int64) (auth.User, error)
}

type Event struct {
	MessageID   string    `json:"message_id"`
	RoomID      int64     `json:"room_id"`
	UserID      int64     `json:"user_id"`
	Nickname    string    `json:"nickname"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	Broadcasted bool      `json:"broadcasted"`
	TrafficMode string    `json:"traffic_mode"`
}

type Service struct {
	rooms     RoomService
	users     UserService
	limiter   Limiter
	filter    *SensitiveFilter
	publisher Publisher
	producer  EventProducer
	traffic   TrafficPolicy
}

func NewService(rooms RoomService, users UserService, limiter Limiter, filter *SensitiveFilter, publisher Publisher, producer EventProducer, policies ...TrafficPolicy) *Service {
	var traffic TrafficPolicy
	if len(policies) > 0 {
		traffic = policies[0]
	}
	return &Service{rooms: rooms, users: users, limiter: limiter, filter: filter, publisher: publisher, producer: producer, traffic: traffic}
}

func (s *Service) Send(ctx context.Context, roomID, userID int64, content string) (Event, error) {
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > 200 {
		return Event{}, ErrInvalidContent
	}
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return Event{}, err
	}
	if v.Status != room.StatusLiving {
		return Event{}, room.ErrNotLiving
	}
	banned, err := s.rooms.IsBanned(ctx, roomID, userID)
	if err != nil {
		return Event{}, err
	}
	if banned {
		return Event{}, room.ErrBanned
	}
	muted, err := s.rooms.IsMuted(ctx, roomID, userID)
	if err != nil {
		return Event{}, err
	}
	if muted {
		return Event{}, ErrMuted
	}
	allowed, err := s.limiter.Allow(ctx, "live:limit:danmaku:user:"+itoa(userID), 5, 10*time.Second)
	if err != nil {
		return Event{}, err
	}
	if !allowed {
		return Event{}, ErrRateLimited
	}
	if s.filter.Contains(content) {
		return Event{}, ErrSensitive
	}
	u, err := s.users.User(ctx, userID)
	if err != nil {
		return Event{}, err
	}
	e := Event{MessageID: idgen.New(), RoomID: roomID, UserID: userID, Nickname: u.Nickname, Content: content, CreatedAt: time.Now().UTC(), Broadcasted: true, TrafficMode: "NORMAL"}
	if s.traffic != nil {
		mode, broadcast, err := s.traffic.Decide(ctx, roomID, e.MessageID)
		if err != nil {
			return Event{}, err
		}
		e.Broadcasted = broadcast
		if mode != "" {
			e.TrafficMode = mode
		}
	}
	if e.Broadcasted {
		wire := realtime.NewPriorityEvent(idgen.New(), "danmaku", roomID, "P3", e)
		if err := s.publisher.Publish(ctx, realtime.RoomStream(roomID), wire); err != nil {
			return Event{}, err
		}
	}
	if s.producer != nil {
		// Persistence is best-effort and must not block the realtime broadcast path.
		_ = s.producer.ProduceDanmaku(ctx, e)
	}
	return e, nil
}

func itoa(v int64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}
