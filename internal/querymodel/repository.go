package querymodel

import (
	"context"
	"database/sql"
	"errors"

	"github.com/example/live-platform/internal/auth"
)

type DB interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) User(ctx context.Context, id int64) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRowContext(ctx, `
SELECT user_id, nickname, avatar, status, created_at
FROM user_display_projection WHERE user_id=? LIMIT 1`, id).
		Scan(&u.ID, &u.Nickname, &u.Avatar, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, err
}
