package outbox

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Event struct {
	ID            int64
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	Topic         string
	PartitionKey  string
	Payload       []byte
	RetryCount    int
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Claim(ctx context.Context, workerID string, batch int, lease time.Duration) ([]Event, error) {
	if batch <= 0 {
		batch = 100
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	staleBefore := time.Now().UTC().Add(-lease)
	rows, err := tx.QueryContext(ctx, `
SELECT id, event_id, aggregate_type, aggregate_id, event_type, topic, partition_key, payload, retry_count
FROM outbox_events
WHERE ((status=0 AND (next_retry_at IS NULL OR next_retry_at<=NOW(3)))
    OR (status=2 AND locked_at<?))
ORDER BY id
LIMIT ?
FOR UPDATE SKIP LOCKED`, staleBefore, batch)
	if err != nil {
		return nil, err
	}
	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventID, &e.AggregateType, &e.AggregateID, &e.EventType, &e.Topic, &e.PartitionKey, &e.Payload, &e.RetryCount); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status=2, locked_by=?, locked_at=NOW(3) WHERE id=?`, workerID, e.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) MarkPublished(ctx context.Context, id int64, workerID string) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE outbox_events
SET status=1, published_at=NOW(3), locked_by=NULL, locked_at=NULL, last_error=NULL
WHERE id=? AND status=2 AND locked_by=?`, id, workerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("outbox publish lock lost")
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, id int64, workerID string, retryAfter time.Duration, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 512 {
			msg = msg[:512]
		}
	}
	next := time.Now().UTC().Add(retryAfter)
	res, err := r.db.ExecContext(ctx, `
UPDATE outbox_events
SET status=0, retry_count=retry_count+1, next_retry_at=?, locked_by=NULL, locked_at=NULL, last_error=?
WHERE id=? AND status=2 AND locked_by=?`, next, strings.TrimSpace(msg), id, workerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("outbox failure lock lost")
	}
	return nil
}

func (r *Repository) PendingCount(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status<>1`).Scan(&n)
	return n, err
}
