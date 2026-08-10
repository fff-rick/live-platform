package observability

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type TraceConfig struct {
	Enabled     bool
	ServiceName string
	Environment string
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

func InitTracer(ctx context.Context, cfg TraceConfig) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("deployment.environment", cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}
	ratio := cfg.SampleRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func TraceHTTP(next http.Handler) http.Handler {
	tracer := otel.Tracer("live-platform/http")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /metrics is deliberately not traced to avoid self-observability noise.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		parent := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(parent, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("url.path", r.URL.Path),
		))
		sw := &traceStatusWriter{ResponseWriter: w, status: http.StatusOK}
		tracedReq := r.WithContext(ctx)
		next.ServeHTTP(sw, tracedReq)
		if tracedReq.Pattern != "" {
			span.SetName(r.Method + " " + tracedReq.Pattern)
			span.SetAttributes(attribute.String("http.route", tracedReq.Pattern))
		}
		span.SetAttributes(attribute.Int("http.response.status_code", sw.status))
		span.End()
	})
}

type traceStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *traceStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
