package mq

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const DeadLetterTopicSuffix = ".dlq"

// DeadLetter preserves enough source metadata to audit and replay one Kafka
// record without changing its original key or payload.
type DeadLetter struct {
	SourceTopic     string    `json:"source_topic"`
	SourcePartition int32     `json:"source_partition"`
	SourceOffset    int64     `json:"source_offset"`
	ConsumerGroup   string    `json:"consumer_group"`
	Key             []byte    `json:"key,omitempty"`
	Value           []byte    `json:"value"`
	Error           string    `json:"error"`
	FailedAt        time.Time `json:"failed_at"`
}

func NewDeadLetter(group string, record Record, cause error) DeadLetter {
	message := "permanent consumer error"
	if cause != nil {
		message = cause.Error()
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	return DeadLetter{
		SourceTopic: record.Topic, SourcePartition: record.Partition, SourceOffset: record.Offset,
		ConsumerGroup: group, Key: record.Key, Value: record.Value, Error: message, FailedAt: time.Now().UTC(),
	}
}

func (d DeadLetter) Validate() error {
	if strings.TrimSpace(d.SourceTopic) == "" || strings.HasSuffix(d.SourceTopic, DeadLetterTopicSuffix) {
		return errors.New("source_topic must be a non-DLQ topic")
	}
	if strings.TrimSpace(d.ConsumerGroup) == "" {
		return errors.New("consumer_group is required")
	}
	if d.SourcePartition < 0 || d.SourceOffset < 0 {
		return errors.New("source partition and offset must be non-negative")
	}
	if d.Value == nil {
		return errors.New("value is required")
	}
	if strings.TrimSpace(d.Error) == "" || d.FailedAt.IsZero() {
		return errors.New("error and failed_at are required")
	}
	return nil
}

type DeadLetterWriter interface {
	WriteDeadLetter(context.Context, DeadLetter) error
}

type RawSyncProducer interface {
	ProduceRawSync(context.Context, string, string, []byte) error
}

type KafkaDeadLetterWriter struct{ producer RawSyncProducer }

func NewKafkaDeadLetterWriter(producer RawSyncProducer) *KafkaDeadLetterWriter {
	return &KafkaDeadLetterWriter{producer: producer}
}

func (w *KafkaDeadLetterWriter) WriteDeadLetter(ctx context.Context, dead DeadLetter) error {
	if w == nil || w.producer == nil {
		return errors.New("DLQ producer is required")
	}
	if err := dead.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(dead)
	if err != nil {
		return fmt.Errorf("encode dead letter: %w", err)
	}
	key := string(dead.Key)
	if key == "" {
		key = strconv.Itoa(int(dead.SourcePartition)) + ":" + strconv.FormatInt(dead.SourceOffset, 10)
	}
	if err := w.producer.ProduceRawSync(ctx, dead.SourceTopic+DeadLetterTopicSuffix, key, raw); err != nil {
		return fmt.Errorf("publish dead letter: %w", err)
	}
	return nil
}

// ReplayJSONLines republishes exported DLQ records to their original topics.
// Each successful replay emits one JSON audit line to auditOut.
func ReplayJSONLines(ctx context.Context, input io.Reader, producer RawSyncProducer, actor string, auditOut io.Writer) (int, error) {
	if strings.TrimSpace(actor) == "" {
		return 0, errors.New("replay actor is required")
	}
	if producer == nil {
		return 0, errors.New("replay producer is required")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	count := 0
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var dead DeadLetter
		if err := json.Unmarshal(scanner.Bytes(), &dead); err != nil {
			return count, fmt.Errorf("decode DLQ line %d: %w", line, err)
		}
		if err := dead.Validate(); err != nil {
			return count, fmt.Errorf("validate DLQ line %d: %w", line, err)
		}
		if err := producer.ProduceRawSync(ctx, dead.SourceTopic, string(dead.Key), dead.Value); err != nil {
			return count, fmt.Errorf("replay DLQ line %d: %w", line, err)
		}
		count++
		if auditOut != nil {
			audit := map[string]any{
				"action": "dlq_replay", "actor": actor, "replayed_at": time.Now().UTC(),
				"source_topic": dead.SourceTopic, "source_partition": dead.SourcePartition, "source_offset": dead.SourceOffset,
			}
			if err := json.NewEncoder(auditOut).Encode(audit); err != nil {
				return count, fmt.Errorf("write replay audit: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read DLQ input: %w", err)
	}
	return count, nil
}
