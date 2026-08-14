package redisstore

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct{ client *redis.Client }

func Open(addr, password string, db int) *Store {
	return &Store{client: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})}
}
func (s *Store) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }
func (s *Store) Close() error                   { return s.client.Close() }

type FixedWindowLimiter struct{ client *redis.Client }

func NewFixedWindowLimiter(store *Store) *FixedWindowLimiter {
	return &FixedWindowLimiter{client: store.client}
}

var fixedWindowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return current <= tonumber(ARGV[1])
`)

func (l *FixedWindowLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	res, err := fixedWindowScript.Run(ctx, l.client, []string{key}, limit, window.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

const activeStatsRoomsKey = "live:stats:rooms"

func roomLikeTotalKey(roomID int64) string { return "live:room:" + formatInt(roomID) + ":like:total" }
func roomLikeDeltaKey(roomID int64) string { return "live:room:" + formatInt(roomID) + ":like:delta" }
func roomViewersKey(roomID int64) string   { return "live:room:" + formatInt(roomID) + ":viewers" }
func roomLastViewerKey(roomID int64) string {
	return "live:room:" + formatInt(roomID) + ":stats:last_viewer"
}

func roomDanmakuRateKey(roomID int64) string {
	return "live:room:" + formatInt(roomID) + ":danmaku:rolling"
}

var danmakuPressureScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local viewers = redis.call('ZCARD', KEYS[1])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('PEXPIRE', KEYS[2], ARGV[5])
local rate = redis.call('ZCARD', KEYS[2])
return {viewers, rate}
`)

// DanmakuPressure uses a rolling ZSET window instead of a fixed INCR window.
// This avoids a saw-tooth controller that would reset its pressure estimate at
// every window boundary. messageID is the unique ZSET member for this request.
func (s *Store) DanmakuPressure(ctx context.Context, roomID int64, messageID string, window time.Duration) (int64, int64, error) {
	now := time.Now().UnixMilli()
	windowMS := window.Milliseconds()
	vals, err := danmakuPressureScript.Run(ctx, s.client, []string{roomViewersKey(roomID), roomDanmakuRateKey(roomID)}, now, now-windowMS, now, messageID, windowMS*2).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(vals) != 2 {
		return 0, 0, redis.Nil
	}
	return vals[0], vals[1], nil
}

var addLikesScript = redis.NewScript(`
local total = redis.call('INCRBY', KEYS[1], ARGV[1])
redis.call('INCRBY', KEYS[2], ARGV[1])
redis.call('ZADD', KEYS[3], ARGV[3], ARGV[2])
return total
`)

func (s *Store) AddLikes(ctx context.Context, roomID, count int64) (int64, error) {
	now := time.Now().UnixMilli()
	return addLikesScript.Run(ctx, s.client,
		[]string{roomLikeTotalKey(roomID), roomLikeDeltaKey(roomID), activeStatsRoomsKey},
		count, roomID, now,
	).Int64()
}

var touchViewerScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[1], ARGV[4])
return redis.call('ZCARD', KEYS[1])
`)

func (s *Store) TouchViewer(ctx context.Context, roomID, userID int64, ttl time.Duration) (int64, error) {
	now := time.Now().UnixMilli()
	expires := now + ttl.Milliseconds()
	return touchViewerScript.Run(ctx, s.client,
		[]string{roomViewersKey(roomID), activeStatsRoomsKey},
		now, userID, expires, roomID,
	).Int64()
}

func (s *Store) TakeLikeDelta(ctx context.Context, roomID int64) (int64, error) {
	v, err := s.client.GetDel(ctx, roomLikeDeltaKey(roomID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func (s *Store) RestoreLikeDelta(ctx context.Context, roomID, delta int64) error {
	if delta <= 0 {
		return nil
	}
	pipe := s.client.TxPipeline()
	pipe.IncrBy(ctx, roomLikeDeltaKey(roomID), delta)
	pipe.ZAdd(ctx, activeStatsRoomsKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: roomID})
	_, err := pipe.Exec(ctx)
	return err
}

var snapshotScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local viewers = redis.call('ZCARD', KEYS[1])
local likes = redis.call('GET', KEYS[2])
if not likes then likes = 0 end
return {viewers, likes}
`)

// EngagementSnapshot is the Redis representation used by the stats adapter.
type EngagementSnapshot struct {
	ViewerCount int64
	LikeCount   int64
}

func (s *Store) EngagementSnapshot(ctx context.Context, roomID int64) (EngagementSnapshot, error) {
	vals, err := snapshotScript.Run(ctx, s.client,
		[]string{roomViewersKey(roomID), roomLikeTotalKey(roomID)}, time.Now().UnixMilli(),
	).Int64Slice()
	if err != nil {
		return EngagementSnapshot{}, err
	}
	if len(vals) != 2 {
		return EngagementSnapshot{}, redis.Nil
	}
	return EngagementSnapshot{ViewerCount: vals[0], LikeCount: vals[1]}, nil
}

func (s *Store) ActiveRooms(ctx context.Context, since time.Time, limit int64) ([]int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, activeStatsRoomsKey, "-inf", formatInt(since.UnixMilli()))
	cmd := pipe.ZRevRange(ctx, activeStatsRoomsKey, 0, limit-1)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	raw, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(raw))
	for _, v := range raw {
		id, err := parseInt(v)
		if err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out, nil
}

func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

func parseInt(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}

func (s *Store) LastPublishedViewer(ctx context.Context, roomID int64) (int64, bool, error) {
	v, err := s.client.Get(ctx, roomLastViewerKey(roomID)).Int64()
	if err == redis.Nil {
		return 0, false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetPublishedViewer(ctx context.Context, roomID, viewerCount int64, ttl time.Duration) error {
	return s.client.Set(ctx, roomLastViewerKey(roomID), viewerCount, ttl).Err()
}
