package wallet

import (
	"context"
	"errors"

	"github.com/example/live-platform/internal/idgen"
)

var (
	ErrInvalidCredit  = errors.New("credit amount must be between 1 and 100000000")
	ErrCreditDisabled = errors.New("development wallet credit is disabled")
)

type Store interface {
	Balance(context.Context, int64) (Balance, error)
	Credit(context.Context, int64, int64, string, string) (Balance, error)
	Transactions(context.Context, int64, int) ([]Transaction, error)
}

type Service struct {
	store            Store
	devCreditEnabled bool
}

func NewService(store Store, devCreditEnabled bool) *Service {
	return &Service{store: store, devCreditEnabled: devCreditEnabled}
}

func (s *Service) Balance(ctx context.Context, userID int64) (Balance, error) {
	return s.store.Balance(ctx, userID)
}

func (s *Service) Transactions(ctx context.Context, userID int64) ([]Transaction, error) {
	return s.store.Transactions(ctx, userID, 50)
}

func (s *Service) DevCredit(ctx context.Context, userID, amount int64) (Balance, error) {
	if !s.devCreditEnabled {
		return Balance{}, ErrCreditDisabled
	}
	if amount < 1 || amount > 100_000_000 {
		return Balance{}, ErrInvalidCredit
	}
	id := idgen.New()
	return s.store.Credit(ctx, userID, amount, "WT"+id, "DEV"+id)
}
