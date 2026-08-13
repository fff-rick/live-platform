package main

import (
	"testing"
	"time"
)

func TestSummarizeLatency(t *testing.T) {
	got := summarizeLatency([]time.Duration{5 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})
	if got.P50 != "3ms" || got.P95 != "5ms" || got.P99 != "5ms" || got.Max != "5ms" {
		t.Fatalf("unexpected summary: %+v", got)
	}
}
