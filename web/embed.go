package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

func Handler(role string) http.Handler {
	dist, _ := fs.Sub(assets, "dist")
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			http.Redirect(w, r, "/"+role, http.StatusFound)
		case r.URL.Path == "/"+role:
			data, err := fs.ReadFile(dist, "index.html")
			if err != nil {
				http.Error(w, "请先用 make build 构建 ClassRelay", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(data)
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
