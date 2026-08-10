package wallet

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	balance Balance
	credits int
}

func (f *fakeStore) Balance(context.Context, int64) (Balance, error)                 { return f.balance, nil }
func (f *fakeStore) Transactions(context.Context, int64, int) ([]Transaction, error) { return nil, nil }
func (f *fakeStore) Credit(_ context.Context, userID, amount int64, _, _ string) (Balance, error) {
	f.credits++
	f.balance = Balance{UserID: userID, Balance: f.balance.Balance + amount}
	return f.balance, nil
}

func TestDevCreditDisabled(t *testing.T) {
	s := NewService(&fakeStore{}, false)
	_, err := s.DevCredit(context.Background(), 1, 100)
	if !errors.Is(err, ErrCreditDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestDevCreditValidationAndSuccess(t *testing.T) {
	f := &fakeStore{}
	s := NewService(f, true)
	if _, err := s.DevCredit(context.Background(), 1, 0); !errors.Is(err, ErrInvalidCredit) {
		t.Fatalf("err=%v", err)
	}
	v, err := s.DevCredit(context.Background(), 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if v.Balance != 500 || f.credits != 1 {
		t.Fatalf("v=%+v credits=%d", v, f.credits)
	}
}
