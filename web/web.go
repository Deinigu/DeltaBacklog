// Package web embeds the single-page frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var files embed.FS

// Handler serves the embedded static site. Unknown paths return 404 (routing is hash-based).
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The service worker and shell must always be revalidated so deploys take effect.
		if r.URL.Path == "/" || r.URL.Path == "/sw.js" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		fileServer.ServeHTTP(w, r)
	})
}
