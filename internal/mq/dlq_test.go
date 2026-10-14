package mq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingSyncProducer struct {
	topic string
	key   string
	value []byte
	err   error
	calls int
}

func (p *recordingSyncProducer) ProduceRawSync(_ context.Context, topic, key string, value []byte) error {
	p.calls++
	p.topic, p.key, p.value = topic, key, append([]byte(nil), value...)
	return p.err
}

type recordingDeadLetterWriter struct {
	dead  DeadLetter
	err   error
	calls int
}

func (w *recordingDeadLetterWriter) WriteDeadLetter(_ context.Context, dead DeadLetter) error {
	w.calls++
	w.dead = dead
	return w.err
}

func validDeadLetter() DeadLetter {
	return DeadLetter{
		SourceTopic: "live.gift.v1", SourcePartition: 2, SourceOffset: 19,
		ConsumerGroup: "live-gift-realtime-v1", Key: []byte("9"), Value: []byte(`{"event_id":"evt-1"}`),
		Error: "invalid gift payload", FailedAt: time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC),
	}
}

func TestKafkaDeadLetterWriterPreservesSourceRecord(t *testing.T) {
	producer := &recordingSyncProducer{}
	writer := NewKafkaDeadLetterWriter(producer)
	dead := validDeadLetter()
	if err := writer.WriteDeadLetter(context.Background(), dead); err != nil {
		t.Fatal(err)
	}
	if producer.topic != "live.gift.v1.dlq" || producer.key != "9" || producer.calls != 1 {
		t.Fatalf("topic=%q key=%q calls=%d", producer.topic, producer.key, producer.calls)
	}
	var got DeadLetter
	if err := json.Unmarshal(producer.value, &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceOffset != dead.SourceOffset || string(got.Value) != string(dead.Value) || got.Error != dead.Error {
		t.Fatalf("dead letter changed source record: %+v", got)
	}
}

func TestConsumerPermanentRecordRequiresSuccessfulDLQ(t *testing.T) {
	record := Record{Topic: "live.danmaku.v1", Partition: 1, Offset: 7, Key: []byte("42"), Value: []byte("bad")}
	consumer := &Consumer{group: "live-danmaku-persist-v1"}
	if err := consumer.writeDeadLetter(context.Background(), record, Permanent(errors.New("invalid"))); err == nil {
		t.Fatal("expected missing DLQ writer to fail")
	}

	writer := &recordingDeadLetterWriter{}
	consumer.deadLetters = writer
	if err := consumer.writeDeadLetter(context.Background(), record, Permanent(errors.New("invalid"))); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || writer.dead.SourceTopic != record.Topic || writer.dead.SourceOffset != record.Offset {
		t.Fatalf("dead letter=%+v calls=%d", writer.dead, writer.calls)
	}
}

func TestReplayJSONLinesPreservesOriginalTopicKeyAndValue(t *testing.T) {
	dead := validDeadLetter()
	raw, err := json.Marshal(dead)
	if err != nil {
		t.Fatal(err)
	}
	producer := &recordingSyncProducer{}
	var audit bytes.Buffer
	count, err := ReplayJSONLines(context.Background(), bytes.NewReader(append(raw, '\n')), producer, "operator@example.com", &audit)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || producer.topic != dead.SourceTopic || producer.key != string(dead.Key) || string(producer.value) != string(dead.Value) {
		t.Fatalf("count=%d topic=%q key=%q value=%s", count, producer.topic, producer.key, producer.value)
	}
	if !strings.Contains(audit.String(), `"actor":"operator@example.com"`) || !strings.Contains(audit.String(), `"source_offset":19`) {
		t.Fatalf("missing replay audit: %s", audit.String())
	}
}

func TestReplayJSONLinesStopsOnInvalidRecord(t *testing.T) {
	producer := &recordingSyncProducer{}
	if _, err := ReplayJSONLines(context.Background(), strings.NewReader(`{"source_topic":"live.gift.v1"}`), producer, "operator", nil); err == nil {
		t.Fatal("expected invalid DLQ record to fail")
	}
	if producer.calls != 0 {
		t.Fatal("invalid record must not be produced")
	}
}
