package gift

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/live-platform/internal/eventdedup"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/realtime"
)

type DedupStore interface {
	Begin(context.Context, string, string, time.Duration) (bool, error)
	Done(context.Context, string, string) error
	Fail(context.Context, string, string, error) error
}

type realtimeGiftPayload struct {
	OrderNo     string `json:"order_no"`
	UserID      int64  `json:"user_id"`
	AnchorID    int64  `json:"anchor_id"`
	GiftID      int64  `json:"gift_id"`
	GiftName    string `json:"gift_name"`
	Count       int64  `json:"count"`
	UnitPrice   int64  `json:"unit_price"`
	TotalAmount int64  `json:"total_amount"`
}

func validRealtimeGiftPayload(raw json.RawMessage) bool {
	var p realtimeGiftPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}
	return p.OrderNo != "" && p.UserID > 0 && p.AnchorID > 0 && p.GiftID > 0 && p.GiftName != "" && p.Count > 0 && p.UnitPrice >= 0 && p.TotalAmount >= 0
}

type ConsumerHandler struct {
	group     string
	dedup     DedupStore
	publisher realtime.Publisher
	lease     time.Duration
}

func NewConsumerHandler(group string, dedup DedupStore, publisher realtime.Publisher, lease time.Duration) *ConsumerHandler {
	return &ConsumerHandler{group: group, dedup: dedup, publisher: publisher, lease: lease}
}

func (h *ConsumerHandler) Handle(ctx context.Context, rec mq.Record) error {
	var envelope mq.Envelope
	if err := json.Unmarshal(rec.Value, &envelope); err != nil {
		return mq.Permanent(fmt.Errorf("decode gift envelope: %w", err))
	}
	if envelope.EventType != "gift.sent" || envelope.EventID == "" || envelope.RoomID <= 0 {
		return mq.Permanent(fmt.Errorf("invalid gift envelope"))
	}
	done, err := h.dedup.Begin(ctx, h.group, envelope.EventID, h.lease)
	if err != nil {
		if err == eventdedup.ErrBusy {
			return err
		}
		return fmt.Errorf("begin gift dedup: %w", err)
	}
	if done {
		return nil
	}

	if len(envelope.Payload) == 0 || !validRealtimeGiftPayload(envelope.Payload) {
		err := fmt.Errorf("invalid gift payload")
		// Poison records are committed by the Kafka loop. Mark the event terminal as well
		// so processed_events does not retain a stale in-progress lock forever.
		if doneErr := h.dedup.Done(ctx, h.group, envelope.EventID); doneErr != nil {
			return fmt.Errorf("mark invalid gift event processed: %w", doneErr)
		}
		return mq.Permanent(err)
	}
	wire := realtime.Event{
		EventID:   envelope.EventID,
		Type:      "gift",
		RoomID:    envelope.RoomID,
		Timestamp: envelope.CreatedAt.UnixMilli(),
		Data:      json.RawMessage(envelope.Payload),
	}
	if err := h.publisher.Publish(ctx, realtime.RoomStream(envelope.RoomID), wire); err != nil {
		_ = h.dedup.Fail(ctx, h.group, envelope.EventID, err)
		return fmt.Errorf("publish gift realtime: %w", err)
	}
	if err := h.dedup.Done(ctx, h.group, envelope.EventID); err != nil {
		return fmt.Errorf("mark gift processed: %w", err)
	}
	return nil
}
