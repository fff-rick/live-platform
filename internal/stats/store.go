package stats

import (
	"context"
	"time"

	"github.com/example/live-platform/internal/store/redisstore"
)

type RedisStore struct{ store *redisstore.Store }

func NewRedisStore(store *redisstore.Store) *RedisStore { return &RedisStore{store: store} }

func (r *RedisStore) ActiveRooms(ctx context.Context, since time.Time, limit int64) ([]int64, error) {
	return r.store.ActiveRooms(ctx, since, limit)
}
func (r *RedisStore) TakeLikeDelta(ctx context.Context, roomID int64) (int64, error) {
	return r.store.TakeLikeDelta(ctx, roomID)
}
func (r *RedisStore) RestoreLikeDelta(ctx context.Context, roomID, delta int64) error {
	return r.store.RestoreLikeDelta(ctx, roomID, delta)
}
func (r *RedisStore) LastPublishedViewer(ctx context.Context, roomID int64) (int64, bool, error) {
	return r.store.LastPublishedViewer(ctx, roomID)
}
func (r *RedisStore) SetPublishedViewer(ctx context.Context, roomID, viewerCount int64, ttl time.Duration) error {
	return r.store.SetPublishedViewer(ctx, roomID, viewerCount, ttl)
}
func (r *RedisStore) Snapshot(ctx context.Context, roomID int64) (Snapshot, error) {
	v, err := r.store.EngagementSnapshot(ctx, roomID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ViewerCount: v.ViewerCount, LikeCount: v.LikeCount}, nil
}
