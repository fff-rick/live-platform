package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublish(t *testing.T) {
	var apiKey, channel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-API-Key")
		var body struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		channel = body.Channel
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{}}`))
	}))
	defer srv.Close()

	c := NewCentrifugo(srv.URL, "key-1")
	if err := c.Publish(context.Background(), "room:1:stream", map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if apiKey != "key-1" {
		t.Fatalf("api key=%q", apiKey)
	}
	if channel != "room:1:stream" {
		t.Fatalf("channel=%q", channel)
	}
}
