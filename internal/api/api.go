// Package api serves the JSON API and cover images.
package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	DB       *sql.DB
	CoverDir string
}

// Handler returns the router for /api, /covers and /healthz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/games", s.listGames)
	mux.HandleFunc("GET /api/facets", s.facets)
	mux.Handle("GET /covers/", http.StripPrefix("/covers/", cacheForever(http.FileServer(http.Dir(filepath.Clean(s.CoverDir))))))
	return mux
}

// cacheForever is safe because a cover file name is the game id and is only rewritten on a cover change
// (the UI appends ?v=<updated_at>).
func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

func fail(w http.ResponseWriter, err error) {
	log.Printf("api error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
