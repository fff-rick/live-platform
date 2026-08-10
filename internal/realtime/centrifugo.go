package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Publisher interface {
	Publish(context.Context, string, any) error
}
type PublishMetrics interface {
	RealtimePublished(result string, duration time.Duration)
}

type Centrifugo struct {
	endpoint string
	health   string
	apiKey   string
	client   *http.Client
	metrics  PublishMetrics
}

func NewCentrifugo(apiURL, apiKey string, metrics ...PublishMetrics) *Centrifugo {
	apiBase := strings.TrimRight(apiURL, "/")
	base := strings.TrimSuffix(apiBase, "/api")
	var m PublishMetrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	return &Centrifugo{
		endpoint: apiBase + "/publish",
		health:   base + "/health",
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 3 * time.Second},
		metrics:  m,
	}
}

type publishRequest struct {
	Channel string `json:"channel"`
	Data    any    `json:"data"`
}
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type publishResponse struct {
	Error *apiError `json:"error,omitempty"`
}

func (c *Centrifugo) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.health, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("centrifugo health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("centrifugo health status=%d", resp.StatusCode)
	}
	return nil
}

func (c *Centrifugo) Publish(ctx context.Context, channel string, data any) error {
	ctx, span := otel.Tracer("live-platform/realtime").Start(ctx, "centrifugo.publish", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("messaging.destination.name", channel)))
	started := time.Now()
	result := "success"
	defer func() {
		if c.metrics != nil {
			c.metrics.RealtimePublished(result, time.Since(started))
		}
		span.End()
	}()

	body, err := json.Marshal(publishRequest{Channel: channel, Data: data})
	if err != nil {
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("marshal publish: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("new publish request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Centrifugo-Error-Mode", "transport")
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := c.client.Do(req)
	if err != nil {
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("centrifugo publish: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("read centrifugo response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = fmt.Errorf("centrifugo publish status=%d body=%s", resp.StatusCode, string(raw))
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	var out publishResponse
	if len(raw) > 0 && json.Unmarshal(raw, &out) == nil && out.Error != nil {
		err = fmt.Errorf("centrifugo error %d: %s", out.Error.Code, out.Error.Message)
		result = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
