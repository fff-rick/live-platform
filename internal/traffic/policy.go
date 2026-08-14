package traffic

import (
	"context"
	"hash/fnv"
	"math"
	"time"
)

type Mode string

const (
	ModeNormal  Mode = "NORMAL"
	ModeHot     Mode = "HOT"
	ModeProtect Mode = "PROTECT"
)

type Sampler interface {
	DanmakuPressure(context.Context, int64, string, time.Duration) (viewerCount, rate int64, err error)
}

type Metrics interface {
	DanmakuDegraded(mode, action string)
}

type policyMetrics interface {
	DanmakuPolicyObserved(mode string, estimatedFanout, sampleRate float64)
}

type Config struct {
	HotViewers         int64
	ProtectViewers     int64
	HotDanmakuRate     int64
	ProtectDanmakuRate int64
	HotSampleRate      float64
	ProtectSampleRate  float64
	RateWindow         time.Duration

	AdaptiveEnabled   bool
	TargetFanoutRate  float64
	HotFanoutRate     float64
	ProtectFanoutRate float64
	MinSampleRate     float64
}

type Policy struct {
	store   Sampler
	cfg     Config
	metrics Metrics
}

func NewPolicy(store Sampler, cfg Config, metrics Metrics) *Policy {
	return &Policy{store: store, cfg: cfg, metrics: metrics}
}

// Decide returns the degradation mode, whether this message should be broadcast,
// the effective sample rate and the estimated fan-out deliveries per second.
//
// The adaptive controller is intentionally capacity based: once the measured
// pressure crosses HOT/PROTECT, it aims to keep realtime fan-out close to the
// configured target instead of applying a fixed 50%/20% drop regardless of room
// size. Fixed sample rates remain as the fallback when adaptive mode is disabled
// or there is not enough fan-out information yet.
func (p *Policy) Decide(ctx context.Context, roomID int64, messageID string) (string, bool, float64, float64, error) {
	viewers, observed, err := p.store.DanmakuPressure(ctx, roomID, messageID, p.cfg.RateWindow)
	if err != nil {
		return "", false, 0, 0, err
	}

	ratePerSecond := float64(observed)
	if p.cfg.RateWindow > 0 {
		ratePerSecond = float64(observed) / p.cfg.RateWindow.Seconds()
	}
	if ratePerSecond < 0 {
		ratePerSecond = 0
	}
	estimatedFanout := float64(viewers) * ratePerSecond

	mode := ModeNormal
	switch {
	case viewers >= p.cfg.ProtectViewers || observed >= p.cfg.ProtectDanmakuRate || estimatedFanout >= p.cfg.ProtectFanoutRate:
		mode = ModeProtect
	case viewers >= p.cfg.HotViewers || observed >= p.cfg.HotDanmakuRate || estimatedFanout >= p.cfg.HotFanoutRate:
		mode = ModeHot
	}

	sample := 1.0
	if mode != ModeNormal {
		if p.cfg.AdaptiveEnabled && estimatedFanout > 0 && p.cfg.TargetFanoutRate > 0 {
			sample = p.cfg.TargetFanoutRate / estimatedFanout
			sample = math.Min(1, math.Max(p.cfg.MinSampleRate, sample))
		} else if mode == ModeProtect {
			sample = p.cfg.ProtectSampleRate
		} else {
			sample = p.cfg.HotSampleRate
		}
	}

	broadcast := keep(messageID, sample)
	if p.metrics != nil {
		action := "broadcast"
		if !broadcast {
			action = "sampled"
		}
		p.metrics.DanmakuDegraded(string(mode), action)
		if m, ok := p.metrics.(policyMetrics); ok {
			m.DanmakuPolicyObserved(string(mode), estimatedFanout, sample)
		}
	}
	return string(mode), broadcast, sample, estimatedFanout, nil
}

func keep(key string, sample float64) bool {
	if sample >= 1 {
		return true
	}
	if sample <= 0 {
		return false
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	bucket := h.Sum64() % 10000
	return float64(bucket) < sample*10000
}
