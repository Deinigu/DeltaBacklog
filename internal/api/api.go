// Package api serves the JSON API and cover images.
package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"time"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	DB         *sql.DB
	CoverDir   string
	Meta       Meta     // IGDB / SteamGridDB lookups; nil disables quick add's metadata
	CoverHosts []string // hosts a client-supplied cover URL may point at; defaults to the metadata CDNs

	cache respCache // the encoded /api/games and /api/facets responses
}

// Warm builds the cached read responses now and again shortly after every change, in the background, so that
// a page load never waits for them. Call it once after the server is set up.
func (s *Server) Warm() {
	build := func() {
		s.cache.get("games", func() (any, error) { return LoadGames(s.DB) })
		s.cache.get("facets", s.buildFacets)
	}
	s.cache.mu.Lock()
	s.cache.warm = build
	if s.cache.warmDelay == 0 {
		s.cache.warmDelay = 400 * time.Millisecond
	}
	s.cache.mu.Unlock()
	go build()
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
	mux.HandleFunc("PATCH /api/games/{id}", s.patchGame)
	mux.HandleFunc("POST /api/games/reorder", s.reorderGames)
	mux.HandleFunc("POST /api/games", s.createGame)
	mux.HandleFunc("DELETE /api/games/{id}", s.deleteGame)
	mux.HandleFunc("POST /api/games/{id}/cover", s.setCover)
	mux.HandleFunc("GET /api/lookup", s.lookup)
	mux.HandleFunc("GET /api/covers/search", s.searchCovers)
	mux.HandleFunc("GET /api/facets", s.facets)
	mux.HandleFunc("GET /api/link/queue", s.linkQueue)
	mux.HandleFunc("POST /api/link/accept", s.linkAccept)
	mux.HandleFunc("POST /api/link/none", s.linkNone)
	mux.HandleFunc("GET /api/views", s.listViews)
	mux.HandleFunc("POST /api/views", s.createView)
	mux.HandleFunc("PUT /api/views/order", s.reorderViews)
	mux.HandleFunc("PATCH /api/views/{id}", s.patchView)
	mux.HandleFunc("DELETE /api/views/{id}", s.deleteView)
	mux.Handle("GET /covers/", http.StripPrefix("/covers/", cacheForever(http.FileServer(http.Dir(filepath.Clean(s.CoverDir))))))
	return s.invalidating(mux)
}

// cacheForever is safe because a cover file name is the game id and is only rewritten on a cover change
// (the UI appends ?v=<cover_v>).
func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=2592000")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

// writeJSONStatus sets the content type before the status: headers added after WriteHeader are never sent.
func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

func fail(w http.ResponseWriter, err error) {
	log.Printf("api error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
