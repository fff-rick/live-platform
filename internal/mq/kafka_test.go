package mq

import (
	"context"
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

type contextKey string

func TestDetachedAsyncContextSurvivesRequestCancellation(t *testing.T) {
	parent := context.WithValue(context.Background(), contextKey("trace-value"), "preserved")
	requestCtx, cancel := context.WithCancel(parent)
	detached := detachedAsyncContext(requestCtx)
	cancel()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached context should not inherit request cancellation: %v", err)
	}
	if got := detached.Value(contextKey("trace-value")); got != "preserved" {
		t.Fatalf("context values must be preserved, got %v", got)
	}
}

func TestKafkaProduceErrorReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{context.Canceled, "context_canceled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{kgo.ErrMaxBuffered, "max_buffered"},
		{kgo.ErrClientClosed, "client_closed"},
		{errors.New("broker failure"), "broker_or_other"},
	}
	for _, tc := range cases {
		if got := kafkaProduceErrorReason(tc.err); got != tc.want {
			t.Fatalf("reason(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}
