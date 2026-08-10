package mq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Metrics interface {
	KafkaProduced(topic, result string, duration time.Duration)
	KafkaConsumed(group, topic, result string)
	KafkaLag(group, topic string, partition int32, lag int64)
	KafkaBuffered(client, direction string, records int64)
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}
func IsPermanent(err error) bool { var p permanentError; return errors.As(err, &p) }

type Producer struct {
	client  *kgo.Client
	log     *slog.Logger
	metrics Metrics
	name    string
}

func NewProducer(brokers []string, log *slog.Logger, metrics ...Metrics) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, errors.New("kafka brokers are required")
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("live-producer"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.RecordRetries(10),
		kgo.RecordDeliveryTimeout(10*time.Second),
		kgo.MaxBufferedRecords(10000),
	)
	if err != nil {
		return nil, err
	}
	var m Metrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	return &Producer{client: cl, log: log, metrics: m, name: "producer"}, nil
}

func (p *Producer) Close() { p.client.Close() }

func (p *Producer) ProduceSync(ctx context.Context, topic, key string, value []byte) error {
	ctx, span := otel.Tracer("live-platform/kafka").Start(ctx, "kafka.produce "+topic, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("messaging.destination.name", topic)))
	started := time.Now()
	value = InjectTrace(value, ctx)
	rec := &kgo.Record{Topic: topic, Key: []byte(key), Value: value}
	err := p.client.ProduceSync(ctx, rec).FirstErr()
	result := "success"
	if err != nil {
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	if p.metrics != nil {
		p.metrics.KafkaProduced(topic, result, time.Since(started))
		p.metrics.KafkaBuffered(p.name, "produce", int64(p.client.BufferedProduceRecords()))
	}
	span.End()
	if err != nil {
		return fmt.Errorf("kafka produce topic=%s: %w", topic, err)
	}
	return nil
}

func (p *Producer) ProduceAsync(ctx context.Context, topic, key string, value []byte) {
	ctx, span := otel.Tracer("live-platform/kafka").Start(ctx, "kafka.produce "+topic, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("messaging.destination.name", topic)))
	started := time.Now()
	value = InjectTrace(value, ctx)
	p.client.TryProduce(ctx, &kgo.Record{Topic: topic, Key: []byte(key), Value: value}, func(r *kgo.Record, err error) {
		result := "success"
		if err != nil {
			result = "failed"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if p.log != nil {
				p.log.ErrorContext(ctx, "kafka async produce failed", "topic", topic, "key", key, "error", err)
			}
		}
		if p.metrics != nil {
			p.metrics.KafkaProduced(topic, result, time.Since(started))
			p.metrics.KafkaBuffered(p.name, "produce", int64(p.client.BufferedProduceRecords()))
		}
		span.End()
	})
}

type Record struct {
	Topic string
	Key   []byte
	Value []byte
}
type Handler interface {
	Handle(context.Context, Record) error
}

type Consumer struct {
	client  *kgo.Client
	log     *slog.Logger
	group   string
	metrics Metrics
}

func NewConsumer(brokers []string, group string, topics []string, log *slog.Logger, metrics ...Metrics) (*Consumer, error) {
	if len(brokers) == 0 || group == "" || len(topics) == 0 {
		return nil, errors.New("kafka brokers, group and topics are required")
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("live-consumer-"+group),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return nil, err
	}
	var m Metrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	return &Consumer{client: cl, log: log, group: group, metrics: m}, nil
}

func (c *Consumer) Close() { c.client.CloseAllowingRebalance() }

func (c *Consumer) Run(ctx context.Context, h Handler) error {
	type tp struct {
		topic     string
		partition int32
	}
	for {
		fetches := c.client.PollRecords(ctx, 100)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.metrics != nil {
			c.metrics.KafkaBuffered(c.group, "fetch", int64(c.client.BufferedFetchRecords()))
		}
		high := make(map[tp]int64)
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			high[tp{p.Topic, p.Partition}] = p.HighWatermark
		})
		for _, fe := range fetches.Errors() {
			if c.log != nil {
				c.log.WarnContext(ctx, "kafka fetch error", "group", c.group, "topic", fe.Topic, "partition", fe.Partition, "error", fe.Err)
			}
			if c.metrics != nil {
				c.metrics.KafkaConsumed(c.group, fe.Topic, "fetch_error")
			}
		}
		iter := fetches.RecordIter()
		for !iter.Done() {
			rec := iter.Next()
			parent := ContextFromEnvelope(ctx, rec.Value)
			recCtx, span := otel.Tracer("live-platform/kafka").Start(parent, "kafka.consume "+rec.Topic, trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
				attribute.String("messaging.destination.name", rec.Topic),
				attribute.String("messaging.consumer.group.name", c.group),
				attribute.Int("messaging.destination.partition.id", int(rec.Partition)),
				attribute.Int64("messaging.kafka.message.offset", rec.Offset),
			))
			result := "success"
			for {
				err := h.Handle(recCtx, Record{Topic: rec.Topic, Key: rec.Key, Value: rec.Value})
				if err == nil {
					result = "success"
					break
				}
				if IsPermanent(err) {
					result = "discarded"
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					if c.log != nil {
						c.log.ErrorContext(recCtx, "discard permanent kafka record", "group", c.group, "topic", rec.Topic, "partition", rec.Partition, "offset", rec.Offset, "error", err)
					}
					break
				}
				result = "retry"
				span.RecordError(err)
				if c.metrics != nil {
					c.metrics.KafkaConsumed(c.group, rec.Topic, "retry")
				}
				if c.log != nil {
					c.log.WarnContext(recCtx, "retry kafka record", "group", c.group, "topic", rec.Topic, "partition", rec.Partition, "offset", rec.Offset, "error", err)
				}
				select {
				case <-ctx.Done():
					span.End()
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
			if err := c.client.CommitRecords(ctx, rec); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.End()
				c.client.AllowRebalance()
				if c.metrics != nil {
					c.metrics.KafkaConsumed(c.group, rec.Topic, "commit_error")
				}
				return fmt.Errorf("commit group=%s topic=%s partition=%d offset=%d: %w", c.group, rec.Topic, rec.Partition, rec.Offset, err)
			}
			if c.metrics != nil {
				c.metrics.KafkaConsumed(c.group, rec.Topic, result)
				lag := high[tp{rec.Topic, rec.Partition}] - (rec.Offset + 1)
				if lag < 0 {
					lag = 0
				}
				c.metrics.KafkaLag(c.group, rec.Topic, rec.Partition, lag)
			}
			span.SetAttributes(attribute.String("messaging.process.result", result), attribute.String("messaging.partition", strconv.Itoa(int(rec.Partition))))
			span.End()
		}
		c.client.AllowRebalance()
	}
}
