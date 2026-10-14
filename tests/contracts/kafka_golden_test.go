package contracts_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/mq"
)

type sqlResult int64

func (r sqlResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r sqlResult) RowsAffected() (int64, error) { return int64(r), nil }

type archiveDB struct{ calls int }

func (d *archiveDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.calls++
	return sqlResult(1), nil
}

type dedupStore struct{ done int }

func (*dedupStore) Begin(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}
func (d *dedupStore) Done(context.Context, string, string) error {
	d.done++
	return nil
}
func (*dedupStore) Fail(context.Context, string, string, error) error { return nil }

type realtimePublisher struct{ calls int }

func (p *realtimePublisher) Publish(context.Context, string, any) error {
	p.calls++
	return nil
}

func goldenRecord(t *testing.T, name string) mq.Record {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var envelope mq.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode golden %s: %v", name, err)
	}
	if err := envelope.ValidateRoomEnvelope(); err != nil {
		t.Fatalf("validate golden %s: %v", name, err)
	}
	return mq.Record{Topic: "live." + envelope.EventType[:len(envelope.EventType)-len(".sent")] + ".v1", Key: []byte("9"), Value: raw}
}

func TestDanmakuSentV1GoldenIsAcceptedByCurrentConsumer(t *testing.T) {
	db := &archiveDB{}
	handler := danmaku.NewPersistenceHandler(db, nil)
	if err := handler.Handle(context.Background(), goldenRecord(t, "danmaku.sent.v1.json")); err != nil {
		t.Fatal(err)
	}
	if db.calls != 1 {
		t.Fatalf("archive writes=%d want 1", db.calls)
	}
}

func TestGiftSentV1GoldenIsAcceptedByCurrentConsumer(t *testing.T) {
	dedup := &dedupStore{}
	publisher := &realtimePublisher{}
	handler := gift.NewConsumerHandler("live-gift-realtime-v1", dedup, publisher, 30*time.Second)
	if err := handler.Handle(context.Background(), goldenRecord(t, "gift.sent.v1.json")); err != nil {
		t.Fatal(err)
	}
	if dedup.done != 1 || publisher.calls != 1 {
		t.Fatalf("dedup done=%d publish=%d want 1/1", dedup.done, publisher.calls)
	}
}
