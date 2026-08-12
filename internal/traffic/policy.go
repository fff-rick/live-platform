package traffic

import (
	"context"
	"hash/fnv"
	"time"
)

type Mode string

const (
	ModeNormal  Mode = "NORMAL"
	ModeHot     Mode = "HOT"
	ModeProtect Mode = "PROTECT"
)

type Sampler interface {
	DanmakuPressure(context.Context, int64, time.Duration) (viewerCount, rate int64, err error)
}

type Metrics interface {
	DanmakuDegraded(mode, action string)
}

type Config struct {
	HotViewers         int64
	ProtectViewers     int64
	HotDanmakuRate     int64
	ProtectDanmakuRate int64
	HotSampleRate      float64
	ProtectSampleRate  float64
	RateWindow         time.Duration
}

type Policy struct {
	store   Sampler
	cfg     Config
	metrics Metrics
}

func NewPolicy(store Sampler, cfg Config, metrics Metrics) *Policy {
	return &Policy{store: store, cfg: cfg, metrics: metrics}
}

func (p *Policy) Decide(ctx context.Context, roomID int64, messageID string) (string, bool, error) {
	viewers, rate, err := p.store.DanmakuPressure(ctx, roomID, p.cfg.RateWindow)
	if err != nil {
		return "", false, err
	}
	mode := ModeNormal
	sample := 1.0
	switch {
	case viewers >= p.cfg.ProtectViewers || rate >= p.cfg.ProtectDanmakuRate:
		mode, sample = ModeProtect, p.cfg.ProtectSampleRate
	case viewers >= p.cfg.HotViewers || rate >= p.cfg.HotDanmakuRate:
		mode, sample = ModeHot, p.cfg.HotSampleRate
	}
	broadcast := keep(messageID, sample)
	if p.metrics != nil {
		action := "broadcast"
		if !broadcast {
			action = "sampled"
		}
		p.metrics.DanmakuDegraded(string(mode), action)
	}
	return string(mode), broadcast, nil
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
	// 10,000 deterministic buckets are enough for traffic degradation sampling.
	bucket := h.Sum64() % 10000
	return float64(bucket) < sample*10000
}
