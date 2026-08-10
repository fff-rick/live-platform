package eventdedup

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrBusy = errors.New("event is being processed by another consumer")

type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Store struct{ db DB }

func NewStore(db DB) *Store { return &Store{db: db} }

// Begin returns done=true when this event was already fully processed.
func (s *Store) Begin(ctx context.Context, group, eventID string, lease time.Duration) (done bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var status int
	var lockedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT status, locked_at FROM processed_events WHERE consumer_group=? AND event_id=? FOR UPDATE`, group, eventID).Scan(&status, &lockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO processed_events(consumer_group,event_id,status,locked_at,created_at) VALUES(?,?,0,NOW(3),NOW(3))`, group, eventID)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status == 1 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if lockedAt.Valid && lockedAt.Time.After(time.Now().UTC().Add(-lease)) {
		return false, ErrBusy
	}
	if _, err := tx.ExecContext(ctx, `UPDATE processed_events SET locked_at=NOW(3), last_error=NULL WHERE consumer_group=? AND event_id=?`, group, eventID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Store) Done(ctx context.Context, group, eventID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE processed_events SET status=1, processed_at=NOW(3), locked_at=NULL, last_error=NULL WHERE consumer_group=? AND event_id=?`, group, eventID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("processed event lock lost")
	}
	return tx.Commit()
}

func (s *Store) Fail(ctx context.Context, group, eventID string, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 512 {
			msg = msg[:512]
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE processed_events SET locked_at=NULL, last_error=? WHERE consumer_group=? AND event_id=? AND status=0`, msg, group, eventID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("processed event lock lost")
	}
	return tx.Commit()
}
