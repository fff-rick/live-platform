package wallet

import (
	"context"
	"database/sql"
	"errors"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Balance(ctx context.Context, userID int64) (Balance, error) {
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO wallets(user_id, balance, version, updated_at)
VALUES (?, 0, 0, NOW(3))
ON DUPLICATE KEY UPDATE user_id=user_id`, userID); err != nil {
		return Balance{}, err
	}
	var v Balance
	if err := r.db.QueryRowContext(ctx, `SELECT user_id, balance FROM wallets WHERE user_id=?`, userID).
		Scan(&v.UserID, &v.Balance); err != nil {
		return Balance{}, err
	}
	return v, nil
}

func (r *Repository) Credit(ctx context.Context, userID, amount int64, transactionNo, bizID string) (Balance, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Balance{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO wallets(user_id, balance, version, updated_at)
VALUES (?, 0, 0, NOW(3))
ON DUPLICATE KEY UPDATE user_id=user_id`, userID); err != nil {
		return Balance{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE wallets SET balance=balance+?, version=version+1, updated_at=NOW(3)
WHERE user_id=?`, amount, userID); err != nil {
		return Balance{}, err
	}
	var after int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE user_id=?`, userID).Scan(&after); err != nil {
		return Balance{}, err
	}
	before := after - amount
	if _, err := tx.ExecContext(ctx, `
INSERT INTO wallet_transactions(transaction_no, user_id, biz_type, biz_id, amount, balance_before, balance_after, created_at)
VALUES (?, ?, 'DEV_CREDIT', ?, ?, ?, ?, NOW(3))`, transactionNo, userID, bizID, amount, before, after); err != nil {
		return Balance{}, err
	}
	if err := tx.Commit(); err != nil {
		return Balance{}, err
	}
	return Balance{UserID: userID, Balance: after}, nil
}

func (r *Repository) Transactions(ctx context.Context, userID int64, limit int) ([]Transaction, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT transaction_no, user_id, biz_type, biz_id, amount, balance_before, balance_after, created_at
FROM wallet_transactions WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Transaction
	for rows.Next() {
		var v Transaction
		if err := rows.Scan(&v.TransactionNo, &v.UserID, &v.BizType, &v.BizID, &v.Amount, &v.BalanceBefore, &v.BalanceAfter, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("wallet not found")
