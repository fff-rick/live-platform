package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user disabled")
	ErrInvalidInput       = errors.New("invalid auth input")
)

type UserRepository interface {
	Create(context.Context, string, string, string) (User, error)
	ByUsername(context.Context, string) (User, error)
	ByID(context.Context, int64) (User, error)
}

type Service struct {
	repo   UserRepository
	tokens *TokenManager
}

func NewService(repo UserRepository, tokens *TokenManager) *Service {
	return &Service{repo: repo, tokens: tokens}
}

type AuthResult struct {
	User      User   `json:"user"`
	Token     string `json:"access_token"`
	ExpiresAt int64  `json:"expire_at"`
}

func (s *Service) Register(ctx context.Context, username, nickname, password string) (AuthResult, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	nickname = strings.TrimSpace(nickname)
	if !validUsername(username) {
		return AuthResult{}, fmt.Errorf("%w: username must be 3-32 letters, digits or underscore", ErrInvalidInput)
	}
	if nickname == "" || len([]rune(nickname)) > 32 {
		return AuthResult{}, fmt.Errorf("%w: nickname must be 1-32 characters", ErrInvalidInput)
	}
	if len(password) < 8 || len(password) > 128 {
		return AuthResult{}, fmt.Errorf("%w: password must be 8-128 characters", ErrInvalidInput)
	}
	if _, err := s.repo.ByUsername(ctx, username); err == nil {
		return AuthResult{}, ErrUsernameTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return AuthResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return AuthResult{}, err
	}
	u, err := s.repo.Create(ctx, username, nickname, hash)
	if err != nil {
		// A concurrent registration may beat our pre-check. The DB unique index remains authoritative.
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return AuthResult{}, ErrUsernameTaken
		}
		return AuthResult{}, err
	}
	return s.result(u)
}

func (s *Service) Login(ctx context.Context, username, password string) (AuthResult, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	u, err := s.repo.ByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}
	if u.Status != 1 {
		return AuthResult{}, ErrUserDisabled
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return AuthResult{}, ErrInvalidCredentials
	}
	return s.result(u)
}

func (s *Service) User(ctx context.Context, id int64) (User, error) { return s.repo.ByID(ctx, id) }

func (s *Service) result(u User) (AuthResult, error) {
	tok, exp, err := s.tokens.Issue(u.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: u, Token: tok, ExpiresAt: exp.Unix()}, nil
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_') {
			return false
		}
	}
	return true
}
