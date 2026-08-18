package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// Files contains the production user-facing single-page application.
//
//go:embed index.html app.js styles.css
var files embed.FS

// Handler serves static assets and falls back to index.html for SPA routes.
func Handler() http.Handler {
	static := http.FileServer(http.FS(mustSub(files, ".")))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "." || clean == "" {
			serveIndex(w, r)
			return
		}
		if strings.HasPrefix(clean, "svg/") {
			http.StripPrefix("/svg/", http.FileServer(http.FS(os.DirFS("svg")))).ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(files, clean); err == nil {
			static.ServeHTTP(w, r)
			return
		}
		// Frontend routes such as /room/123 and /studio are handled by the SPA.
		serveIndex(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	b, err := files.ReadFile("index.html")
	if err != nil {
		http.Error(w, "web app unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func mustSub(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
