package mq

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRoomEnvelopeContract(t *testing.T) {
	envelope, err := NewEnvelope("evt-1", EventTypeGiftSent, 9, map[string]any{"order_no": "G1"})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	if err := envelope.ValidateRoomEnvelope(); err != nil {
		t.Fatalf("validate envelope: %v", err)
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	for _, field := range []string{"event_id", "event_type", "room_id", "created_at", "payload"} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("wire contract lost required field %q: %s", field, raw)
		}
	}
}

func TestValidateRoomEnvelopeRejectsInvalidContract(t *testing.T) {
	validPayload := json.RawMessage(`{"x":1}`)
	cases := []struct {
		name string
		e    Envelope
	}{
		{"missing event id", Envelope{EventType: EventTypeDanmakuSent, RoomID: 1, CreatedAt: time.Now(), Payload: validPayload}},
		{"missing event type", Envelope{EventID: "evt", RoomID: 1, CreatedAt: time.Now(), Payload: validPayload}},
		{"missing room", Envelope{EventID: "evt", EventType: EventTypeDanmakuSent, CreatedAt: time.Now(), Payload: validPayload}},
		{"missing time", Envelope{EventID: "evt", EventType: EventTypeDanmakuSent, RoomID: 1, Payload: validPayload}},
		{"invalid payload", Envelope{EventID: "evt", EventType: EventTypeDanmakuSent, RoomID: 1, CreatedAt: time.Now(), Payload: json.RawMessage(`{`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.e.ValidateRoomEnvelope(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
