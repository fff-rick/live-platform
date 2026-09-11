package identityclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/example/live-platform/internal/room"
)

func TestGetRoomAccessUsesSingleRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s", r.Method)
		}
		if r.URL.Path != "/internal/v1/rooms/12/access/34" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"room":{"room_id":12,"status":"LIVING"},"banned":false,"muted":true}`))
	}))
	defer server.Close()

	access, err := New(server.URL).GetRoomAccess(context.Background(), 12, 34)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d", calls.Load())
	}
	if access.Room.ID != 12 || access.Room.Status != room.StatusLiving || access.Banned || !access.Muted {
		t.Fatalf("access=%+v", access)
	}
}
