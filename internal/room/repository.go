package room

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, anchorID int64, title string) (Room, error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO live_rooms(anchor_id, title, status, created_at, updated_at)
VALUES (?, ?, 'PREPARING', NOW(3), NOW(3))`, anchorID, title)
	if err != nil {
		return Room{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Room{}, err
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByID(ctx context.Context, id int64) (Room, error) {
	var v Room
	var started, ended sql.NullTime
	err := r.db.QueryRowContext(ctx, `
SELECT r.id, r.anchor_id, u.nickname, r.title, r.status, r.started_at, r.ended_at, r.created_at
FROM live_rooms r
JOIN users u ON u.id = r.anchor_id
WHERE r.id = ? LIMIT 1`, id).
		Scan(&v.ID, &v.AnchorID, &v.AnchorNickname, &v.Title, &v.Status, &started, &ended, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, err
	}
	if started.Valid {
		t := started.Time
		v.StartedAt = &t
	}
	if ended.Valid {
		t := ended.Time
		v.EndedAt = &t
	}
	return v, nil
}

func (r *Repository) List(ctx context.Context, status Status, limit int) ([]Room, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	query := `
SELECT r.id, r.anchor_id, u.nickname, r.title, r.status, r.started_at, r.ended_at, r.created_at
FROM live_rooms r
JOIN users u ON u.id = r.anchor_id`
	args := make([]any, 0, 2)
	if status != "" {
		query += ` WHERE r.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY COALESCE(r.started_at, r.created_at) DESC, r.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Room, 0, limit)
	for rows.Next() {
		var v Room
		var started, ended sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnchorID, &v.AnchorNickname, &v.Title, &v.Status, &started, &ended, &v.CreatedAt); err != nil {
			return nil, err
		}
		if started.Valid {
			t := started.Time
			v.StartedAt = &t
		}
		if ended.Valid {
			t := ended.Time
			v.EndedAt = &t
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ChangeStatus(ctx context.Context, id, anchorID int64, from, to Status) error {
	query := `UPDATE live_rooms SET status=?, updated_at=NOW(3)`
	if to == StatusLiving {
		query += `, started_at=NOW(3), ended_at=NULL`
	}
	if to == StatusClosed {
		query += `, ended_at=NOW(3)`
	}
	query += ` WHERE id=? AND anchor_id=? AND status=?`
	res, err := r.db.ExecContext(ctx, query, to, id, anchorID, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidState
	}
	return nil
}

func (r *Repository) Join(ctx context.Context, roomID, userID int64) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO live_sessions(room_id, user_id, joined_at, last_seen_at, status)
VALUES (?, ?, NOW(3), NOW(3), 1)
ON DUPLICATE KEY UPDATE last_seen_at=NOW(3), status=1`, roomID, userID)
	return err
}

func (r *Repository) IsBanned(ctx context.Context, roomID, userID int64) (bool, error) {
	var banned int
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM room_bans WHERE room_id=? AND user_id=?)`, roomID, userID).Scan(&banned)
	return banned == 1, err
}

func (r *Repository) Ban(ctx context.Context, roomID, userID, createdBy int64, reason string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO room_bans(room_id, user_id, created_by, reason, created_at)
VALUES (?, ?, ?, ?, NOW(3))
ON DUPLICATE KEY UPDATE created_by=VALUES(created_by), reason=VALUES(reason), created_at=NOW(3)`, roomID, userID, createdBy, reason)
	return err
}

func (r *Repository) Unban(ctx context.Context, roomID, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM room_bans WHERE room_id=? AND user_id=?`, roomID, userID)
	return err
}

func (r *Repository) IsMuted(ctx context.Context, roomID, userID int64) (bool, error) {
	var muted int
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM room_mutes
  WHERE room_id=? AND user_id=? AND (muted_until IS NULL OR muted_until > NOW(3))
)`, roomID, userID).Scan(&muted)
	return muted == 1, err
}

func (r *Repository) Mute(ctx context.Context, roomID, userID, createdBy int64, until *time.Time, reason string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO room_mutes(room_id, user_id, muted_until, created_by, reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE muted_until=VALUES(muted_until), created_by=VALUES(created_by), reason=VALUES(reason), updated_at=NOW(3)`,
		roomID, userID, until, createdBy, reason)
	return err
}

func (r *Repository) Unmute(ctx context.Context, roomID, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM room_mutes WHERE room_id=? AND user_id=?`, roomID, userID)
	return err
}
