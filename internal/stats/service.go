package stats

import "context"

type SnapshotStore interface {
	Snapshot(context.Context, int64) (Snapshot, error)
}

type Service struct{ store SnapshotStore }

func NewService(store SnapshotStore) *Service { return &Service{store: store} }

func (s *Service) Get(ctx context.Context, roomID int64) (Snapshot, error) {
	return s.store.Snapshot(ctx, roomID)
}
