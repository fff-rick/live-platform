package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client           *redis.Client
	activeRoomShards int64
}

type Config struct {
	URL              string
	Addr             string
	Password         string
	DB               int
	ActiveRoomShards int64
}

func Open(cfg Config) (*Store, error) {
	var opts *redis.Options
	var err error
	if cfg.URL != "" {
		opts, err = redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, err
		}
	} else {
		opts = &redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB}
	}
	if cfg.ActiveRoomShards <= 0 {
		cfg.ActiveRoomShards = 16
	}
	return &Store{client: redis.NewClient(opts), activeRoomShards: cfg.ActiveRoomShards}, nil
}
func (s *Store) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }
func (s *Store) Close() error                   { return s.client.Close() }

type FixedWindowLimiter struct{ client *redis.Client }

func NewFixedWindowLimiter(store *Store) *FixedWindowLimiter {
	return &FixedWindowLimiter{client: store.client}
}

var fixedWindowScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local requested = tonumber(ARGV[3])
if current + requested > tonumber(ARGV[1]) then
  return 0
end
local next = redis.call('INCRBY', KEYS[1], requested)
if next == requested then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 1
`)

func (l *FixedWindowLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return l.AllowN(ctx, key, int64(limit), 1, window)
}

// AllowN consumes n units from one fixed window. It is used for batched likes
// so the quota represents clicks rather than HTTP requests.
func (l *FixedWindowLimiter) AllowN(ctx context.Context, key string, limit, n int64, window time.Duration) (bool, error) {
	res, err := fixedWindowScript.Run(ctx, l.client, []string{key}, limit, window.Milliseconds(), n).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func roomTag(roomID int64) string           { return "{" + formatInt(roomID) + "}" }
func roomLikeTotalKey(roomID int64) string  { return "live:room:" + roomTag(roomID) + ":like:total" }
func roomLikeDeltaKey(roomID int64) string  { return "live:room:" + roomTag(roomID) + ":like:delta" }
func roomViewersKey(roomID int64) string    { return "live:room:" + roomTag(roomID) + ":viewers" }
func roomViewerJoinKey(roomID int64) string { return "live:room:" + roomTag(roomID) + ":viewer-join" }
func roomViewerGiftKey(roomID int64) string { return "live:room:" + roomTag(roomID) + ":viewer-gift" }
func roomLastViewerKey(roomID int64) string {
	return "live:room:" + roomTag(roomID) + ":stats:last_viewer"
}
func activeStatsRoomsKey(shard int64) string        { return fmt.Sprintf("live:stats:rooms:%d", shard) }
func (s *Store) activeRoomShard(roomID int64) int64 { return roomID % s.activeRoomShards }

func roomDanmakuRateKey(roomID int64) string {
	return "live:room:" + roomTag(roomID) + ":danmaku:rolling"
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
return total
`)

func (s *Store) AddLikes(ctx context.Context, roomID, count int64) (int64, error) {
	now := time.Now().UnixMilli()
	// The index write deliberately precedes the room-local script. This keeps
	// the Lua call single-slot in Redis Cluster; a stray active entry is harmless.
	if err := s.client.ZAdd(ctx, activeStatsRoomsKey(s.activeRoomShard(roomID)), redis.Z{Score: float64(now), Member: roomID}).Err(); err != nil {
		return 0, err
	}
	return addLikesScript.Run(ctx, s.client,
		[]string{roomLikeTotalKey(roomID), roomLikeDeltaKey(roomID)}, count,
	).Int64()
}

func likeRoomStatusKey(roomID int64) string { return "live:room:" + roomTag(roomID) + ":like:status" }
func likeBanKey(roomID, userID int64) string {
	return "live:room:" + roomTag(roomID) + ":like:ban:" + formatInt(userID)
}
func LikeRateKey(roomID, userID int64) string {
	return "live:room:" + roomTag(roomID) + ":like:rate:" + formatInt(userID)
}

func (s *Store) CachedLikeRoomStatus(ctx context.Context, roomID int64) (string, bool, error) {
	v, err := s.client.Get(ctx, likeRoomStatusKey(roomID)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) CacheLikeRoomStatus(ctx context.Context, roomID int64, status string, ttl time.Duration) error {
	return s.client.Set(ctx, likeRoomStatusKey(roomID), status, ttl).Err()
}

func (s *Store) CachedLikeBan(ctx context.Context, roomID, userID int64) (bool, bool, error) {
	v, err := s.client.Get(ctx, likeBanKey(roomID, userID)).Int()
	if err == redis.Nil {
		return false, false, nil
	}
	return v == 1, err == nil, err
}

func (s *Store) CacheLikeBan(ctx context.Context, roomID, userID int64, banned bool, ttl time.Duration) error {
	v := 0
	if banned {
		v = 1
	}
	return s.client.Set(ctx, likeBanKey(roomID, userID), v, ttl).Err()
}

var touchViewerScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[2])
redis.call('ZADD', KEYS[2], 'NX', ARGV[1], ARGV[2])
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for _, user in ipairs(expired) do redis.call('ZREM', KEYS[2], user) end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
return redis.call('ZCARD', KEYS[1])
`)

func (s *Store) TouchViewer(ctx context.Context, roomID, userID int64, ttl time.Duration) (int64, error) {
	now := time.Now().UnixMilli()
	expires := now + ttl.Milliseconds()
	if err := s.client.ZAdd(ctx, activeStatsRoomsKey(s.activeRoomShard(roomID)), redis.Z{Score: float64(now), Member: roomID}).Err(); err != nil {
		return 0, err
	}
	return touchViewerScript.Run(ctx, s.client,
		[]string{roomViewersKey(roomID), roomViewerJoinKey(roomID)}, now, userID, expires,
	).Int64()
}

func (s *Store) RemoveViewer(ctx context.Context, roomID, userID int64) (int64, error) {
	pipe := s.client.TxPipeline()
	pipe.ZRem(ctx, roomViewersKey(roomID), formatInt(userID))
	pipe.ZRem(ctx, roomViewerJoinKey(roomID), formatInt(userID))
	count := pipe.ZCard(ctx, roomViewersKey(roomID))
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return count.Val(), nil
}

func (s *Store) AddViewerGiftValue(ctx context.Context, roomID, userID, value int64) error {
	return s.client.ZIncrBy(ctx, roomViewerGiftKey(roomID), float64(value), formatInt(userID)).Err()
}

var topViewersScript = redis.NewScript(`
local active = redis.call('ZRANGEBYSCORE', KEYS[1], ARGV[1], '+inf')
local rows = {}
for _, user in ipairs(active) do
 table.insert(rows, {user, tonumber(redis.call('ZSCORE', KEYS[2], user) or '0'), tonumber(redis.call('ZSCORE', KEYS[3], user) or '0')})
end
table.sort(rows, function(a,b) if a[2] == b[2] then return a[3] < b[3] end return a[2] > b[2] end)
local out = {}; for i=1,math.min(#rows,tonumber(ARGV[2])) do table.insert(out, rows[i][1]); table.insert(out, tostring(rows[i][2])) end
return out
`)

func (s *Store) TopViewers(ctx context.Context, roomID, limit int64) ([]int64, error) {
	vals, err := topViewersScript.Run(ctx, s.client, []string{roomViewersKey(roomID), roomViewerGiftKey(roomID), roomViewerJoinKey(roomID)}, time.Now().UnixMilli(), limit).StringSlice()
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(vals)/2)
	for i := 0; i+1 < len(vals); i += 2 {
		id, e := parseInt(vals[i])
		if e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Store) OnlineViewers(ctx context.Context, roomID, limit int64) ([]int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return s.TopViewers(ctx, roomID, limit)
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
	if err := s.client.ZAdd(ctx, activeStatsRoomsKey(s.activeRoomShard(roomID)), redis.Z{Score: float64(time.Now().UnixMilli()), Member: roomID}).Err(); err != nil {
		return err
	}
	return s.client.IncrBy(ctx, roomLikeDeltaKey(roomID), delta).Err()
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

func (s *Store) LikeSnapshot(ctx context.Context, roomID int64) (int64, error) {
	return s.client.Get(ctx, roomLikeTotalKey(roomID)).Int64()
}

func (s *Store) ActiveRooms(ctx context.Context, since time.Time, limit int64) ([]int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	out := make([]int64, 0, limit)
	perShard := (limit + s.activeRoomShards - 1) / s.activeRoomShards
	for shard := int64(0); shard < s.activeRoomShards && int64(len(out)) < limit; shard++ {
		pipe := s.client.TxPipeline()
		pipe.ZRemRangeByScore(ctx, activeStatsRoomsKey(shard), "-inf", formatInt(since.UnixMilli()))
		cmd := pipe.ZRevRange(ctx, activeStatsRoomsKey(shard), 0, perShard-1)
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
		raw, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		for _, v := range raw {
			id, err := parseInt(v)
			if err == nil && id > 0 {
				out = append(out, id)
				if int64(len(out)) == limit {
					break
				}
			}
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
