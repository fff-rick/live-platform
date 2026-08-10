package danmaku

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/example/live-platform/internal/mq"
)

type AsyncProducer interface {
	ProduceAsync(context.Context, string, string, []byte)
}

type KafkaProducer struct {
	producer AsyncProducer
	topic    string
	log      *slog.Logger
}

func NewKafkaProducer(producer AsyncProducer, topic string, log *slog.Logger) *KafkaProducer {
	return &KafkaProducer{producer: producer, topic: topic, log: log}
}

func (p *KafkaProducer) ProduceDanmaku(ctx context.Context, e Event) error {
	envelope, err := mq.NewEnvelopeContext(ctx, e.MessageID, "danmaku.sent", e.RoomID, e)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	p.producer.ProduceAsync(ctx, p.topic, strconv.FormatInt(e.RoomID, 10), raw)
	return nil
}

type PersistenceDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type PersistenceHandler struct {
	db  PersistenceDB
	log *slog.Logger
}

func NewPersistenceHandler(db PersistenceDB, log *slog.Logger) *PersistenceHandler {
	return &PersistenceHandler{db: db, log: log}
}

func (h *PersistenceHandler) Handle(ctx context.Context, rec mq.Record) error {
	var envelope mq.Envelope
	if err := json.Unmarshal(rec.Value, &envelope); err != nil {
		return mq.Permanent(fmt.Errorf("decode danmaku envelope: %w", err))
	}
	if envelope.EventType != "danmaku.sent" {
		return mq.Permanent(fmt.Errorf("unexpected danmaku event type %q", envelope.EventType))
	}
	var e Event
	if err := json.Unmarshal(envelope.Payload, &e); err != nil {
		return mq.Permanent(fmt.Errorf("decode danmaku payload: %w", err))
	}
	if e.MessageID == "" || e.RoomID <= 0 || e.UserID <= 0 || e.Content == "" {
		return mq.Permanent(fmt.Errorf("invalid danmaku payload"))
	}
	res, err := h.db.ExecContext(ctx, `
INSERT INTO danmaku_records(message_id, room_id, user_id, nickname, content, created_at)
VALUES(?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE message_id=message_id`, e.MessageID, e.RoomID, e.UserID, e.Nickname, e.Content, e.CreatedAt)
	if err != nil {
		return err
	}
	if h.log != nil {
		if n, _ := res.RowsAffected(); n > 0 {
			h.log.Debug("danmaku persisted", "message_id", e.MessageID, "room_id", e.RoomID)
		}
	}
	return nil
}
