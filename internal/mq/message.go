package mq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Event type values are part of the Kafka wire contract. Keep existing values
// stable; introduce a new value instead of changing one already consumed.
const (
	EventTypeDanmakuSent = "danmaku.sent"
	EventTypeGiftSent    = "gift.sent"
)

type Envelope struct {
	EventID   string            `json:"event_id"`
	EventType string            `json:"event_type"`
	RoomID    int64             `json:"room_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Trace     map[string]string `json:"trace,omitempty"`
	Payload   json.RawMessage   `json:"payload"`
}

// ValidateRoomEnvelope validates fields shared by the room-scoped event
// contracts. Payload-specific validation remains the responsibility of each
// consumer because every event type owns a different payload schema.
func (e Envelope) ValidateRoomEnvelope() error {
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("event_id is required")
	}
	if strings.TrimSpace(e.EventType) == "" {
		return errors.New("event_type is required")
	}
	if e.RoomID <= 0 {
		return errors.New("room_id must be positive")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return errors.New("payload must be valid JSON")
	}
	return nil
}

func NewEnvelope(eventID, eventType string, roomID int64, payload any) (Envelope, error) {
	return NewEnvelopeContext(context.Background(), eventID, eventType, roomID, payload)
}

func NewEnvelopeContext(ctx context.Context, eventID, eventType string, roomID int64, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	e := Envelope{EventID: eventID, EventType: eventType, RoomID: roomID, CreatedAt: time.Now().UTC(), Payload: raw}
	e.Trace = injectTraceMap(ctx)
	return e, nil
}

func ContextFromEnvelope(parent context.Context, raw []byte) context.Context {
	var e Envelope
	if json.Unmarshal(raw, &e) != nil || len(e.Trace) == 0 {
		return parent
	}
	return otel.GetTextMapPropagator().Extract(parent, propagation.MapCarrier(e.Trace))
}

func InjectTrace(raw []byte, ctx context.Context) []byte {
	var e Envelope
	if json.Unmarshal(raw, &e) != nil {
		return raw
	}
	e.Trace = injectTraceMap(ctx)
	out, err := json.Marshal(e)
	if err != nil {
		return raw
	}
	return out
}

func injectTraceMap(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	return map[string]string(carrier)
}
