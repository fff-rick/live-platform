package traffic

import (
	"context"
	"math"
	"testing"
	"time"
)

type fakeSampler struct{ viewers, rate int64 }

func (f fakeSampler) DanmakuPressure(context.Context, int64, string, time.Duration) (int64, int64, error) {
	return f.viewers, f.rate, nil
}

func cfg() Config {
	return Config{
		HotViewers: 100, ProtectViewers: 200,
		HotDanmakuRate: 50, ProtectDanmakuRate: 100,
		HotSampleRate: .5, ProtectSampleRate: .2,
		RateWindow:      time.Second,
		AdaptiveEnabled: true, TargetFanoutRate: 25_000,
		HotFanoutRate: 30_000, ProtectFanoutRate: 40_000, MinSampleRate: .05,
	}
}

func TestPolicyModes(t *testing.T) {
	cases := []struct {
		name          string
		viewers, rate int64
		want          Mode
	}{
		{"normal", 10, 10, ModeNormal},
		{"hot viewers", 100, 10, ModeHot},
		{"hot rate", 10, 50, ModeHot},
		{"protect viewers", 200, 10, ModeProtect},
		{"protect rate", 10, 100, ModeProtect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPolicy(fakeSampler{tc.viewers, tc.rate}, cfg(), nil)
			mode, _, _, _, err := p.Decide(context.Background(), 1, "m1")
			if err != nil {
				t.Fatal(err)
			}
			if Mode(mode) != tc.want {
				t.Fatalf("got %s want %s", mode, tc.want)
			}
		})
	}
}

func TestPolicyFanoutModesIndependentOfLegacyThresholds(t *testing.T) {
	c := cfg()
	c.HotViewers, c.ProtectViewers = 1_000_000, 2_000_000
	c.HotDanmakuRate, c.ProtectDanmakuRate = 1_000_000, 2_000_000
	for _, tc := range []struct {
		viewers, rate int64
		want          Mode
	}{
		{1000, 30, ModeHot},
		{2000, 20, ModeProtect},
	} {
		p := NewPolicy(fakeSampler{tc.viewers, tc.rate}, c, nil)
		mode, _, _, _, err := p.Decide(context.Background(), 1, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if Mode(mode) != tc.want {
			t.Fatalf("viewers=%d rate=%d got=%s want=%s", tc.viewers, tc.rate, mode, tc.want)
		}
	}
}

func TestAdaptiveSampleTargetsSafeFanout(t *testing.T) {
	p := NewPolicy(fakeSampler{viewers: 5000, rate: 20}, cfg(), nil)
	mode, _, sample, fanout, err := p.Decide(context.Background(), 1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if Mode(mode) != ModeProtect {
		t.Fatalf("mode=%s", mode)
	}
	if fanout != 100000 {
		t.Fatalf("fanout=%v", fanout)
	}
	if math.Abs(sample-.25) > .0001 {
		t.Fatalf("sample=%v want .25", sample)
	}
}

func TestAdaptiveSampleHasFloor(t *testing.T) {
	c := cfg()
	p := NewPolicy(fakeSampler{viewers: 100_000, rate: 100}, c, nil)
	_, _, sample, _, err := p.Decide(context.Background(), 1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if sample != c.MinSampleRate {
		t.Fatalf("sample=%v want floor=%v", sample, c.MinSampleRate)
	}
}

func TestLegacySamplingWhenAdaptiveDisabled(t *testing.T) {
	c := cfg()
	c.AdaptiveEnabled = false
	p := NewPolicy(fakeSampler{viewers: 2000, rate: 20}, c, nil)
	mode, _, sample, _, err := p.Decide(context.Background(), 1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if Mode(mode) != ModeProtect || sample != c.ProtectSampleRate {
		t.Fatalf("mode=%s sample=%v", mode, sample)
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
