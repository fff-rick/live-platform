package traffic

import (
	"context"
	"testing"
	"time"
)

type fakeSampler struct{ viewers, rate int64 }

func (f fakeSampler) DanmakuPressure(context.Context, int64, time.Duration) (int64, int64, error) {
	return f.viewers, f.rate, nil
}

type metricSpy struct{ mode, action string }

func (m *metricSpy) DanmakuDegraded(mode, action string) { m.mode, m.action = mode, action }

func cfg() Config {
	return Config{HotViewers: 100, ProtectViewers: 200, HotDanmakuRate: 50, ProtectDanmakuRate: 100, HotSampleRate: 1, ProtectSampleRate: 1, RateWindow: time.Second}
}

func TestPolicyModes(t *testing.T) {
	cases := []struct {
		name          string
		viewers, rate int64
		want          Mode
	}{
		{"normal", 10, 10, ModeNormal}, {"hot viewers", 100, 10, ModeHot}, {"hot rate", 10, 50, ModeHot}, {"protect viewers", 200, 10, ModeProtect}, {"protect rate", 10, 100, ModeProtect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPolicy(fakeSampler{tc.viewers, tc.rate}, cfg(), nil)
			mode, _, err := p.Decide(context.Background(), 1, "m1")
			if err != nil {
				t.Fatal(err)
			}
			if Mode(mode) != tc.want {
				t.Fatalf("got %s want %s", mode, tc.want)
			}
		})
	}
}

func TestKeepDeterministic(t *testing.T) {
	a := keep("same", 0.5)
	for i := 0; i < 100; i++ {
		if keep("same", 0.5) != a {
			t.Fatal("sampling must be deterministic")
		}
	}
}
