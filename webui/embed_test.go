package webui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPAAndAssets(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/room/42", "/studio"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "LiveFlow") {
			t.Fatalf("path=%s code=%d", path, w.Code)
		}
	}
	assets := map[string]string{"/styles.css": "--pink", "/gift-request.mjs": "GiftRequestStore", "/message-dedup.mjs": "rememberMessage"}
	for path, marker := range assets {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), marker) {
			t.Fatalf("asset=%s code=%d", path, w.Code)
		}
		if path == "/gift-request.mjs" && !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("asset=%s content-type=%q", path, w.Header().Get("Content-Type"))
		}
	}
}

func TestHomeHeroCopy(t *testing.T) {
	app, err := fs.ReadFile(files, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	copy := string(app)
	if !strings.Contains(copy, "Live Platform") || !strings.Contains(copy, "To show your beatiful life!") {
		t.Fatal("home hero copy is missing")
	}
	if strings.Contains(copy, "REALTIME · HIGH CONCURRENCY") || strings.Contains(copy, "高并发直播互动，") || strings.Contains(copy, "实时读取后端 LIVING 房间与在线数据") {
		t.Fatal("old home hero copy is still present")
	}
}
