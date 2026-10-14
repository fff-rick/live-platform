package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHandlerServesSPAAndAssets(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/room/42", "/studio"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "LUMI LIVE") {
			t.Fatalf("path=%s code=%d", path, w.Code)
		}
	}
	assets := map[string]string{"/styles.css": "--pink", "/lumi.css": "LUMI LIVE", "/gift-request.mjs": "GiftRequestStore", "/message-dedup.mjs": "rememberMessage"}
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

func TestHandlerServesFrontendAssets(t *testing.T) {
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	h := Handler()
	for _, asset := range []struct {
		path        string
		contentType string
	}{
		{path: "/front_con/ava/布偶.svg", contentType: "image/svg+xml"},
		{path: "/front_con/ava/female.png", contentType: "image/png"},
		{path: "/front_con/ava/male.png", contentType: "image/png"},
		{path: "/front_con/background/hot.png", contentType: "image/png"},
		{path: "/front_con/icon/like.png", contentType: "image/png"},
		{path: "/front_con/logo/project.png", contentType: "image/png"},
	} {
		r := httptest.NewRequest(http.MethodGet, asset.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("asset=%s code=%d size=%d", asset.path, w.Code, w.Body.Len())
		}
		if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, asset.contentType) {
			t.Fatalf("asset=%s content-type=%q", asset.path, contentType)
		}
	}
}

func TestHomeHeroCopy(t *testing.T) {
	app, err := fs.ReadFile(files, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	copy := string(app)
	if !strings.Contains(copy, "LUMI LIVE") || !strings.Contains(copy, "To show your beautiful life!") {
		t.Fatal("home hero copy is missing")
	}
	if strings.Contains(copy, "LiveFlow") || strings.Contains(copy, "Live Platform") || strings.Contains(copy, "beatiful") {
		t.Fatal("old home hero copy is still present")
	}
}
