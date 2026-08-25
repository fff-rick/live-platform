package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/live-platform/internal/mq"
)

type Producer interface {
	ProduceSync(context.Context, string, string, []byte) error
}
type Metrics interface {
	OutboxPendingSet(int64, time.Duration)
	OutboxPublished(result string)
	OutboxRetried()
}

type Publisher struct {
	log      *slog.Logger
	repo     *Repository
	producer Producer
	metrics  Metrics
	workerID string
	interval time.Duration
	lease    time.Duration
	timeout  time.Duration
	batch    int
}

func NewPublisher(log *slog.Logger, repo *Repository, producer Producer, workerID string, interval time.Duration, batch int, lease, timeout time.Duration, metrics ...Metrics) *Publisher {
	var m Metrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	return &Publisher{log: log, repo: repo, producer: producer, metrics: m, workerID: workerID, interval: interval, batch: batch, lease: lease, timeout: timeout}
}

func (p *Publisher) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			p.dispatch(ctx)
			timer.Reset(p.interval)
		}
	}
}

func (p *Publisher) dispatch(ctx context.Context) {
	events, err := p.repo.Claim(ctx, p.workerID, p.batch, p.lease)
	if err != nil {
		p.log.ErrorContext(ctx, "claim outbox failed", "error", err)
		p.updatePending(ctx)
		return
	}
	for _, e := range events {
		// Reattach the original request trace stored in the outbox payload. The Kafka
		// producer creates the next producer span and rewrites the envelope trace context.
		eventCtx := mq.ContextFromEnvelope(ctx, e.Payload)
		produceCtx, cancel := context.WithTimeout(eventCtx, p.timeout)
		err := p.producer.ProduceSync(produceCtx, e.Topic, e.PartitionKey, e.Payload)
		cancel()
		if err != nil {
			delay := retryDelay(e.RetryCount + 1)
			if markErr := p.repo.MarkFailed(ctx, e.ID, p.workerID, delay, err); markErr != nil {
				p.log.ErrorContext(eventCtx, "mark outbox failed", "event_id", e.EventID, "error", markErr)
			}
			if p.metrics != nil {
				p.metrics.OutboxPublished("failed")
				p.metrics.OutboxRetried()
			}
			p.log.WarnContext(eventCtx, "outbox publish failed", "event_id", e.EventID, "topic", e.Topic, "retry_after", delay.String(), "error", err)
			continue
		}
		if err := p.repo.MarkPublished(ctx, e.ID, p.workerID); err != nil {
			if p.metrics != nil {
				p.metrics.OutboxPublished("mark_failed")
			}
			p.log.ErrorContext(eventCtx, "mark outbox published failed", "event_id", e.EventID, "error", err)
			continue
		}
		if p.metrics != nil {
			p.metrics.OutboxPublished("success")
		}
		p.log.InfoContext(eventCtx, "outbox published", "event_id", e.EventID, "topic", e.Topic)
	}
	p.updatePending(ctx)
}

func (p *Publisher) updatePending(ctx context.Context) {
	if p.metrics == nil {
		return
	}
	n, oldestAge, err := p.repo.PendingStats(ctx)
	if err != nil {
		p.log.WarnContext(ctx, "count pending outbox failed", "error", err)
		return
	}
	p.metrics.OutboxPendingSet(n, oldestAge)
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Second
	for i := 1; i < attempt && d < 5*time.Minute; i++ {
		d *= 2
	}
	if d > 5*time.Minute {
		return 5 * time.Minute
	}
	return d
}
