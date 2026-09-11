package redisstore

import (
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestLikeCountResultTreatsMissingKeyAsZero(t *testing.T) {
	count, err := likeCountResult(0, redis.Nil)
	if err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestLikeCountResultPreservesRedisError(t *testing.T) {
	want := errors.New("redis unavailable")
	_, err := likeCountResult(0, want)
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
