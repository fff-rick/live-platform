package gift

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/example/live-platform/internal/mq"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var (
	ErrGiftNotFound        = errors.New("gift not found")
	ErrGiftOffline         = errors.New("gift is offline")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrOrderNotFound       = errors.New("gift order not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for different request parameters")
)

type DB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

type CreateParams struct {
	OrderNo       string
	RequestID     string
	TransactionNo string
	EventID       string
	GiftTopic     string
	UserID        int64
	AnchorID      int64
	RoomID        int64
	GiftID        int64
	Count         int64
}

func (r *Repository) ListActive(ctx context.Context) ([]Gift, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, price, status
FROM gifts WHERE status=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []Gift
	for rows.Next() {
		var v Gift
		if err := rows.Scan(&v.ID, &v.Name, &v.Price, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ByOrderNo(ctx context.Context, orderNo string) (Order, error) {
	return scanOrder(r.db.QueryRowContext(ctx, orderSelect+` WHERE o.order_no=? LIMIT 1`, orderNo))
}

func (r *Repository) ByRequestID(ctx context.Context, requestID string) (Order, error) {
	return scanOrder(r.db.QueryRowContext(ctx, orderSelect+` WHERE o.request_id=? LIMIT 1`, requestID))
}

func (r *Repository) Create(ctx context.Context, p CreateParams) (Order, bool, error) {
	ctx, txSpan := otel.Tracer("live-platform/gift/repository").Start(ctx, "gift.db.transaction")
	txSpan.SetAttributes(attribute.Int64("live.user_id", p.UserID), attribute.Int64("live.room_id", p.RoomID), attribute.Int64("live.gift_id", p.GiftID))
	defer txSpan.End()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var g Gift
	if err := tx.QueryRowContext(ctx, `SELECT id, name, price, status FROM gifts WHERE id=? LIMIT 1`, p.GiftID).
		Scan(&g.ID, &g.Name, &g.Price, &g.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, false, ErrGiftNotFound
		}
		return Order{}, false, err
	}
	if g.Status != 1 {
		return Order{}, false, ErrGiftOffline
	}
	if p.Count <= 0 || g.Price < 0 || (p.Count > 0 && g.Price > math.MaxInt64/p.Count) {
		return Order{}, false, errors.New("gift amount overflow")
	}
	total := g.Price * p.Count

	_, orderSpan := otel.Tracer("live-platform/gift/repository").Start(ctx, "gift.db.order_insert")
	_, err = tx.ExecContext(ctx, `
INSERT INTO gift_orders(order_no, request_id, user_id, anchor_id, room_id, gift_id, gift_count, unit_price, total_amount, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, NOW(3), NOW(3))`,
		p.OrderNo, p.RequestID, p.UserID, p.AnchorID, p.RoomID, p.GiftID, p.Count, g.Price, total)
	if err != nil {
		orderSpan.RecordError(err)
		orderSpan.SetStatus(codes.Error, err.Error())
	}
	orderSpan.End()
	if err != nil {
		if isDuplicateRequest(err) {
			_ = tx.Rollback()
			existing, readErr := r.ByRequestID(ctx, p.RequestID)
			if readErr != nil {
				return Order{}, false, readErr
			}
			if existing.UserID != p.UserID || existing.RoomID != p.RoomID || existing.GiftID != p.GiftID || existing.GiftCount != p.Count {
				return Order{}, false, ErrIdempotencyConflict
			}
			return existing, true, nil
		}
		return Order{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO wallets(user_id, balance, version, updated_at)
VALUES (?, 0, 0, NOW(3))
ON DUPLICATE KEY UPDATE user_id=user_id`, p.UserID); err != nil {
		return Order{}, false, err
	}
	_, walletSpan := otel.Tracer("live-platform/gift/repository").Start(ctx, "gift.db.wallet_update")
	res, err := tx.ExecContext(ctx, `
UPDATE wallets
SET balance=balance-?, version=version+1, updated_at=NOW(3)
WHERE user_id=? AND balance>=?`, total, p.UserID, total)
	if err != nil {
		walletSpan.RecordError(err)
		walletSpan.SetStatus(codes.Error, err.Error())
	}
	walletSpan.End()
	if err != nil {
		return Order{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Order{}, false, err
	}
	if n != 1 {
		return Order{}, false, ErrInsufficientBalance
	}
	var balanceAfter int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE user_id=?`, p.UserID).Scan(&balanceAfter); err != nil {
		return Order{}, false, err
	}
	balanceBefore := balanceAfter + total
	if _, err := tx.ExecContext(ctx, `
INSERT INTO wallet_transactions(transaction_no, user_id, biz_type, biz_id, amount, balance_before, balance_after, created_at)
VALUES (?, ?, 'GIFT', ?, ?, ?, ?, NOW(3))`,
		p.TransactionNo, p.UserID, p.OrderNo, -total, balanceBefore, balanceAfter); err != nil {
		return Order{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE gift_orders SET status=1, updated_at=NOW(3) WHERE order_no=?`, p.OrderNo); err != nil {
		return Order{}, false, err
	}
	if p.EventID == "" || p.GiftTopic == "" {
		return Order{}, false, errors.New("gift outbox event configuration is required")
	}
	envelope, err := mq.NewEnvelopeContext(ctx, p.EventID, "gift.sent", p.RoomID, map[string]any{
		"order_no": p.OrderNo, "user_id": p.UserID, "anchor_id": p.AnchorID, "gift_id": p.GiftID,
		"gift_name": g.Name, "count": p.Count, "unit_price": g.Price, "total_amount": total,
	})
	if err != nil {
		return Order{}, false, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Order{}, false, err
	}
	_, outboxSpan := otel.Tracer("live-platform/gift/repository").Start(ctx, "gift.db.outbox_insert")
	_, outboxErr := tx.ExecContext(ctx, `
INSERT INTO outbox_events(event_id, aggregate_type, aggregate_id, event_type, topic, partition_key, payload, status, retry_count, next_retry_at, created_at)
VALUES (?, 'GIFT_ORDER', ?, 'gift.sent', ?, ?, ?, 0, 0, NOW(3), NOW(3))`,
		p.EventID, p.OrderNo, p.GiftTopic, strconv.FormatInt(p.RoomID, 10), payload)
	if outboxErr != nil {
		outboxSpan.RecordError(outboxErr)
		outboxSpan.SetStatus(codes.Error, outboxErr.Error())
	}
	outboxSpan.End()
	if outboxErr != nil {
		return Order{}, false, outboxErr
	}

	_, commitSpan := otel.Tracer("live-platform/gift/repository").Start(ctx, "gift.db.commit")
	commitErr := tx.Commit()
	if commitErr != nil {
		commitSpan.RecordError(commitErr)
		commitSpan.SetStatus(codes.Error, commitErr.Error())
	}
	commitSpan.End()
	if commitErr != nil {
		return Order{}, false, commitErr
	}
	created, err := r.ByOrderNo(ctx, p.OrderNo)
	if err != nil {
		return Order{}, false, err
	}
	return created, false, nil
}

const orderSelect = `
SELECT o.order_no, o.request_id, o.user_id, o.anchor_id, o.room_id, o.gift_id,
       g.name, o.gift_count, o.unit_price, o.total_amount,
       CASE o.status WHEN 1 THEN 'SUCCESS' ELSE 'PENDING' END, o.created_at
FROM gift_orders o
JOIN gifts g ON g.id=o.gift_id`

func scanOrder(row *sql.Row) (Order, error) {
	var v Order
	err := row.Scan(&v.OrderNo, &v.RequestID, &v.UserID, &v.AnchorID, &v.RoomID, &v.GiftID,
		&v.GiftName, &v.GiftCount, &v.UnitPrice, &v.TotalAmount, &v.Status, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return v, err
}

func isDuplicateRequest(err error) bool {
	var e *mysqlDriver.MySQLError
	if !errors.As(err, &e) || e.Number != 1062 {
		return false
	}
	msg := strings.ToLower(e.Message)
	return strings.Contains(msg, "uk_gift_orders_request_id") || strings.Contains(msg, "request_id")
}
