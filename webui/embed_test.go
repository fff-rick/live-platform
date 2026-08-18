package webui

import (
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
	r := httptest.NewRequest("GET", "/styles.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "--pink") {
		t.Fatalf("asset code=%d", w.Code)
	}
}
