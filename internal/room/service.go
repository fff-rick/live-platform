package room

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound       = errors.New("room not found")
	ErrNotLiving      = errors.New("room is not living")
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidState   = errors.New("invalid room state")
	ErrBanned         = errors.New("user is banned from room")
	ErrSelfModeration = errors.New("anchor cannot moderate self")
	ErrInvalidInput   = errors.New("invalid room input")
	ErrUserNotFound   = errors.New("target user not found")
)

type RoomRepository interface {
	Create(context.Context, int64, string) (Room, error)
	ByID(context.Context, int64) (Room, error)
	List(context.Context, Status, int) ([]Room, error)
	ChangeStatus(context.Context, int64, int64, Status, Status) error
	Join(context.Context, int64, int64) error
	IsMuted(context.Context, int64, int64) (bool, error)
	IsBanned(context.Context, int64, int64) (bool, error)
	ListMutes(context.Context, int64) ([]Mute, error)
	ListBans(context.Context, int64) ([]Ban, error)
	Ban(context.Context, int64, int64, int64, string) error
	Unban(context.Context, int64, int64) error
	Mute(context.Context, int64, int64, int64, *time.Time, string) error
	Unmute(context.Context, int64, int64) error
}

type UserDirectory interface {
	Nickname(context.Context, int64) (string, bool, error)
}

type Service struct {
	repo  RoomRepository
	users UserDirectory
}

func NewService(repo RoomRepository, users ...UserDirectory) *Service {
	s := &Service{repo: repo}
	if len(users) > 0 {
		s.users = users[0]
	}
	return s
}

func (s *Service) Create(ctx context.Context, anchorID int64, title string) (Room, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 100 {
		return Room{}, fmt.Errorf("%w: title must be 1-100 characters", ErrInvalidInput)
	}
	v, err := s.repo.Create(ctx, anchorID, title)
	return s.withAnchor(ctx, v, err)
}

func (s *Service) Get(ctx context.Context, id int64) (Room, error) {
	v, err := s.repo.ByID(ctx, id)
	return s.withAnchor(ctx, v, err)
}

func (s *Service) GetRoomAccess(ctx context.Context, roomID, userID int64) (RoomAccess, error) {
	v, err := s.Get(ctx, roomID)
	if err != nil {
		return RoomAccess{}, err
	}
	banned, err := s.repo.IsBanned(ctx, roomID, userID)
	if err != nil {
		return RoomAccess{}, err
	}
	muted, err := s.repo.IsMuted(ctx, roomID, userID)
	if err != nil {
		return RoomAccess{}, err
	}
	return RoomAccess{Room: v, Banned: banned, Muted: muted}, nil
}

func (s *Service) List(ctx context.Context, status Status, limit int) ([]Room, error) {
	if status != "" && status != StatusPreparing && status != StatusLiving && status != StatusClosed {
		return nil, fmt.Errorf("%w: unsupported room status", ErrInvalidInput)
	}
	items, err := s.repo.List(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i], err = s.withAnchor(ctx, items[i], nil)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) Start(ctx context.Context, id, actorID int64) (Room, error) {
	v, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Room{}, err
	}
	if v.AnchorID != actorID {
		return Room{}, ErrForbidden
	}
	if err := s.repo.ChangeStatus(ctx, id, actorID, StatusPreparing, StatusLiving); err != nil {
		return Room{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Stop(ctx context.Context, id, actorID int64) (Room, error) {
	v, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Room{}, err
	}
	if v.AnchorID != actorID {
		return Room{}, ErrForbidden
	}
	if err := s.repo.ChangeStatus(ctx, id, actorID, StatusLiving, StatusClosed); err != nil {
		return Room{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Join(ctx context.Context, id, userID int64) (Room, error) {
	v, err := s.Get(ctx, id)
	if err != nil {
		return Room{}, err
	}
	if v.Status != StatusLiving {
		return Room{}, ErrNotLiving
	}
	banned, err := s.repo.IsBanned(ctx, id, userID)
	if err != nil {
		return Room{}, err
	}
	if banned {
		return Room{}, ErrBanned
	}
	if err := s.repo.Join(ctx, id, userID); err != nil {
		return Room{}, err
	}
	return v, nil
}

func (s *Service) IsMuted(ctx context.Context, roomID, userID int64) (bool, error) {
	return s.repo.IsMuted(ctx, roomID, userID)
}

func (s *Service) IsBanned(ctx context.Context, roomID, userID int64) (bool, error) {
	return s.repo.IsBanned(ctx, roomID, userID)
}

func (s *Service) Mute(ctx context.Context, roomID, actorID, targetID int64, duration time.Duration, reason string) error {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return err
	}
	if targetID == actorID {
		return ErrSelfModeration
	}
	reason, err := s.validateModerationTarget(ctx, targetID, reason)
	if err != nil {
		return err
	}
	var until *time.Time
	if duration > 0 {
		t := time.Now().UTC().Add(duration)
		until = &t
	}
	return s.repo.Mute(ctx, roomID, targetID, actorID, until, reason)
}

func (s *Service) Unmute(ctx context.Context, roomID, actorID, targetID int64) error {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return err
	}
	return s.repo.Unmute(ctx, roomID, targetID)
}

func (s *Service) Ban(ctx context.Context, roomID, actorID, targetID int64, reason string) error {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return err
	}
	if targetID == actorID {
		return ErrSelfModeration
	}
	reason, err := s.validateModerationTarget(ctx, targetID, reason)
	if err != nil {
		return err
	}
	return s.repo.Ban(ctx, roomID, targetID, actorID, reason)
}

func (s *Service) Unban(ctx context.Context, roomID, actorID, targetID int64) error {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return err
	}
	return s.repo.Unban(ctx, roomID, targetID)
}

func (s *Service) ListMutes(ctx context.Context, roomID, actorID int64) ([]Mute, error) {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListMutes(ctx, roomID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Nickname, _, err = s.nickname(ctx, items[i].UserID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) ListBans(ctx context.Context, roomID, actorID int64) ([]Ban, error) {
	if err := s.requireAnchor(ctx, roomID, actorID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListBans(ctx, roomID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Nickname, _, err = s.nickname(ctx, items[i].UserID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// requireAnchor 有意不限制房间状态：治理名单跨开播生命周期生效，主播可预封禁或下播后清理。
func (s *Service) requireAnchor(ctx context.Context, roomID, actorID int64) error {
	v, err := s.repo.ByID(ctx, roomID)
	if err != nil {
		return err
	}
	if v.AnchorID != actorID {
		return ErrForbidden
	}
	return nil
}

func (s *Service) validateModerationTarget(ctx context.Context, targetID int64, reason string) (string, error) {
	_, exists, err := s.nickname(ctx, targetID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrUserNotFound
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 255 {
		return "", fmt.Errorf("%w: reason must not exceed 255 characters", ErrInvalidInput)
	}
	return reason, nil
}

func (s *Service) withAnchor(ctx context.Context, v Room, err error) (Room, error) {
	if err != nil || s.users == nil {
		return v, err
	}
	v.AnchorNickname, _, err = s.nickname(ctx, v.AnchorID)
	return v, err
}

func (s *Service) nickname(ctx context.Context, userID int64) (string, bool, error) {
	if s.users == nil {
		return "", false, errors.New("room user directory is not configured")
	}
	return s.users.Nickname(ctx, userID)
}
