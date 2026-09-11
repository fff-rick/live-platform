package like

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type snapshotSource struct {
	rooms       []int64
	counts      map[int64]int64
	readErrors  map[int64]error
	restored    map[int64]int64
	restoreErrs map[int64]error
}

func (s *snapshotSource) ActiveRooms(context.Context, time.Time, int64) ([]int64, error) {
	return s.rooms, nil
}

func (s *snapshotSource) LikeSnapshot(_ context.Context, roomID int64) (int64, error) {
	return s.counts[roomID], s.readErrors[roomID]
}

func (s *snapshotSource) RestoreLikeSnapshot(_ context.Context, roomID, count int64) error {
	if err := s.restoreErrs[roomID]; err != nil {
		return err
	}
	if s.restored == nil {
		s.restored = make(map[int64]int64)
	}
	s.restored[roomID] = count
	return nil
}

type checkpointStore struct {
	loaded     []Checkpoint
	loadErr    error
	saved      map[int64]int64
	saveErrors map[int64]error
}

func (s *checkpointStore) LoadAfter(_ context.Context, afterRoomID, limit int64) ([]Checkpoint, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	out := make([]Checkpoint, 0, limit)
	for _, checkpoint := range s.loaded {
		if checkpoint.RoomID > afterRoomID {
			out = append(out, checkpoint)
			if int64(len(out)) == limit {
				break
			}
		}
	}
	return out, nil
}

func (s *checkpointStore) Save(_ context.Context, roomID, count int64) error {
	if err := s.saveErrors[roomID]; err != nil {
		return err
	}
	if s.saved == nil {
		s.saved = make(map[int64]int64)
	}
	s.saved[roomID] = count
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSnapshotterWritesCurrentCount(t *testing.T) {
	source := &snapshotSource{rooms: []int64{1}, counts: map[int64]int64{1: 42}}
	checkpoints := &checkpointStore{}
	s := NewSnapshotter(testLogger(), source, checkpoints, time.Second, time.Minute, 10)
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if checkpoints.saved[1] != 42 {
		t.Fatalf("snapshot=%d", checkpoints.saved[1])
	}
}

func TestSnapshotterContinuesAfterRoomFailure(t *testing.T) {
	readErr := errors.New("redis unavailable")
	source := &snapshotSource{
		rooms:      []int64{1, 2, 3},
		counts:     map[int64]int64{1: 10, 3: 30},
		readErrors: map[int64]error{2: readErr},
	}
	checkpoints := &checkpointStore{}
	s := NewSnapshotter(testLogger(), source, checkpoints, time.Second, time.Minute, 10)

	err := s.Tick(context.Background())
	if !errors.Is(err, readErr) {
		t.Fatalf("err=%v", err)
	}
	if checkpoints.saved[1] != 10 || checkpoints.saved[3] != 30 {
		t.Fatalf("saved=%v", checkpoints.saved)
	}
	if _, ok := checkpoints.saved[2]; ok {
		t.Fatalf("failed room was saved: %v", checkpoints.saved)
	}
}

func TestSnapshotterContinuesAfterCheckpointWriteFailure(t *testing.T) {
	writeErr := errors.New("mysql unavailable")
	source := &snapshotSource{
		rooms:  []int64{1, 2, 3},
		counts: map[int64]int64{1: 10, 2: 20, 3: 30},
	}
	checkpoints := &checkpointStore{saveErrors: map[int64]error{2: writeErr}}
	s := NewSnapshotter(testLogger(), source, checkpoints, time.Second, time.Minute, 10)

	err := s.Tick(context.Background())
	if !errors.Is(err, writeErr) {
		t.Fatalf("err=%v", err)
	}
	if checkpoints.saved[1] != 10 || checkpoints.saved[3] != 30 {
		t.Fatalf("saved=%v", checkpoints.saved)
	}
}

func TestSnapshotterRecoversAllCheckpoints(t *testing.T) {
	source := &snapshotSource{}
	checkpoints := &checkpointStore{loaded: []Checkpoint{{RoomID: 1, LikeCount: 12}, {RoomID: 2, LikeCount: 34}}}
	s := NewSnapshotter(testLogger(), source, checkpoints, time.Second, time.Minute, 1)

	if err := s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if source.restored[1] != 12 || source.restored[2] != 34 {
		t.Fatalf("restored=%v", source.restored)
	}
}

func TestSnapshotterRecoveryContinuesAfterRoomFailure(t *testing.T) {
	restoreErr := errors.New("redis write failed")
	source := &snapshotSource{restoreErrs: map[int64]error{2: restoreErr}}
	checkpoints := &checkpointStore{loaded: []Checkpoint{
		{RoomID: 1, LikeCount: 12},
		{RoomID: 2, LikeCount: 34},
		{RoomID: 3, LikeCount: 56},
	}}
	s := NewSnapshotter(testLogger(), source, checkpoints, time.Second, time.Minute, 2)

	err := s.Recover(context.Background())
	if !errors.Is(err, restoreErr) {
		t.Fatalf("err=%v", err)
	}
	if source.restored[1] != 12 || source.restored[3] != 56 {
		t.Fatalf("restored=%v", source.restored)
	}
}
