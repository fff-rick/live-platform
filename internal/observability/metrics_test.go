package observability

import (
	"testing"
	"time"
)

func TestOutboxPendingMetrics(t *testing.T) {
	m := NewMetrics("test")
	m.OutboxPendingSet(3, 90*time.Second)

	if got := gaugeValue(t, m, "live_outbox_pending"); got != 3 {
		t.Fatalf("pending=%v want 3", got)
	}
	if got := gaugeValue(t, m, "live_outbox_oldest_pending_age_seconds"); got != 90 {
		t.Fatalf("oldest age=%v want 90", got)
	}
}

func gaugeValue(t *testing.T, m *Metrics, name string) float64 {
	t.Helper()
	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name && len(family.Metric) == 1 && family.Metric[0].Gauge != nil {
			return family.Metric[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("gauge %q not found", name)
	return 0
}
