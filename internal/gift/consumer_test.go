package gift

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/example/live-platform/internal/mq"
)

type fakeDedup struct {
	done                 bool
	beginErr             error
	doneCalls, failCalls int
}

func (f *fakeDedup) Begin(context.Context, string, string, time.Duration) (bool, error) {
	return f.done, f.beginErr
}
func (f *fakeDedup) Done(context.Context, string, string) error        { f.doneCalls++; return nil }
func (f *fakeDedup) Fail(context.Context, string, string, error) error { f.failCalls++; return nil }

type consumerPublisher struct {
	calls int
	err   error
	data  any
}

func (f *consumerPublisher) Publish(_ context.Context, _ string, data any) error {
	f.calls++
	f.data = data
	return f.err
}

func giftRecord(t *testing.T) mq.Record {
	t.Helper()
	env, err := mq.NewEnvelope("event-1", "gift.sent", 9, map[string]any{"order_no": "G1", "user_id": 10, "anchor_id": 20, "gift_id": 1, "gift_name": "Rose", "count": 1, "unit_price": 100, "total_amount": 100})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return mq.Record{Topic: "live.gift.v1", Value: raw}
}

func TestGiftConsumerPublishesAndMarksDone(t *testing.T) {
	d := &fakeDedup{}
	p := &consumerPublisher{}
	h := NewConsumerHandler("gift-group", d, p, 30*time.Second)
	if err := h.Handle(context.Background(), giftRecord(t)); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || d.doneCalls != 1 || d.failCalls != 0 {
		t.Fatalf("publish=%d done=%d fail=%d", p.calls, d.doneCalls, d.failCalls)
	}
}

func TestGiftConsumerSkipsProcessed(t *testing.T) {
	d := &fakeDedup{done: true}
	p := &consumerPublisher{}
	h := NewConsumerHandler("gift-group", d, p, 30*time.Second)
	if err := h.Handle(context.Background(), giftRecord(t)); err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 || d.doneCalls != 0 {
		t.Fatalf("duplicate published")
	}
}

func TestGiftConsumerReleasesDedupOnPublishFailure(t *testing.T) {
	d := &fakeDedup{}
	p := &consumerPublisher{err: errors.New("down")}
	h := NewConsumerHandler("gift-group", d, p, 30*time.Second)
	if err := h.Handle(context.Background(), giftRecord(t)); err == nil {
		t.Fatal("expected error")
	}
	if d.failCalls != 1 || d.doneCalls != 0 {
		t.Fatalf("done=%d fail=%d", d.doneCalls, d.failCalls)
	}
}

func TestGiftConsumerDiscardsInvalidPayloadAndMarksDone(t *testing.T) {
	d := &fakeDedup{}
	p := &consumerPublisher{}
	h := NewConsumerHandler("gift-group", d, p, 30*time.Second)
	env := mq.Envelope{EventID: "event-bad", EventType: "gift.sent", RoomID: 9, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`"not-a-gift-object"`)}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	err = h.Handle(context.Background(), mq.Record{Topic: "live.gift.v1", Value: raw})
	if err == nil || !mq.IsPermanent(err) {
		t.Fatalf("expected permanent error, got %v", err)
	}
	if p.calls != 0 || d.doneCalls != 1 || d.failCalls != 0 {
		t.Fatalf("publish=%d done=%d fail=%d", p.calls, d.doneCalls, d.failCalls)
	}
}
