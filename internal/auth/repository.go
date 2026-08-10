package auth

import (
	"context"
	"database/sql"
	"errors"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, username, nickname, passwordHash string) (User, error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO users(username, nickname, password_hash, status, created_at, updated_at)
VALUES (?, ?, ?, 1, NOW(3), NOW(3))`, username, nickname, passwordHash)
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, `
SELECT id, username, nickname, password_hash, status, created_at
FROM users WHERE username = ? LIMIT 1`, username).
		Scan(&u.ID, &u.Username, &u.Nickname, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

func (r *Repository) ByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, `
SELECT id, username, nickname, password_hash, status, created_at
FROM users WHERE id = ? LIMIT 1`, id).
		Scan(&u.ID, &u.Username, &u.Nickname, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}
