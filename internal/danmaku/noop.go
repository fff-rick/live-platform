package danmaku

import "context"

type NoopProducer struct{}

func (NoopProducer) ProduceDanmaku(context.Context, Event) error { return nil }
