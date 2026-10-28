// Package api serves the JSON API and cover images.
package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	DB               *sql.DB
	CoverDir         string
	Meta             Meta     // IGDB / SteamGridDB lookups; nil disables quick add's metadata
	ShotDir          string   // screenshot originals and thumbnails; the server never creates this folder itself (see shotStorageOK)
	ShotMaxFile      int64    // largest accepted screenshot, bytes (default 25 MB)
	ShotMaxRequest   int64    // largest upload request, bytes (default 200 MB)
	ShotDecodeBudget int64    // most memory one screenshot may need to be decoded for its thumbnail, bytes (default 48 MB)
	HLTB             HLTB     // HowLongToBeat lookups; nil means the real site (see hltbClient)
	CoverHosts       []string // hosts a client-supplied cover URL may point at; defaults to the metadata CDNs

	// PosterFetch and PosterPause replace the poster download and the pause between two posters (tests).
	PosterFetch func(url, dir string, gameID int64) (string, error)
	// MissingPosterFetch replaces the download of a poster for a game the library does not have (Series page; tests). Its
	// own field: PosterFetch writes p<game id>.jpg, which must never be used for an IGDB id.
	MissingPosterFetch func(url, dir string, igdbID int64) (string, error)
	PosterPause        time.Duration

	// Collections replaces the IGDB client the Series page reads collections with (tests); nil means Meta.
	Collections collections.Source
	// Similar replaces the IGDB client the Similar page reads similar games with (tests); nil means Meta.
	Similar collections.SimilarSource
	// Deals is the IsThereAnyDeal client the Deals page reads prices with; nil means the feature is off (tests set a fake).
	Deals deals.Source

	hltbOnce   sync.Once
	posterOnce sync.Once
	posters    *posters.Queue // background poster worker, created on first use (see posterQ)
	bg         sync.WaitGroup // background best-effort work (release dates of a new link); tests wait for it
	seriesJob  seriesJob      // the background "Refresh from IGDB" of the Series page
	similarJob seriesJob      // ditto for the Similar page
	dealsJob   seriesJob      // ditto for the Deals page

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
	s.cache.mu.Lock()
	s.cache.version = func() (int64, error) {
		var v int64
		err := s.DB.QueryRow(`PRAGMA data_version`).Scan(&v)
		return v, err
	}
	s.cache.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.PingContext(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "db down", nil)
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
	mux.HandleFunc("POST /api/games/{id}/poster", s.setPoster)
	mux.HandleFunc("GET /api/games/{id}/screenshots", s.listShots)
	mux.HandleFunc("POST /api/games/{id}/screenshots", s.uploadShots)
	mux.HandleFunc("PUT /api/games/{id}/screenshots/order", s.orderShots)
	mux.HandleFunc("GET /api/screenshots", s.listAllShots)
	mux.HandleFunc("PATCH /api/screenshots/{id}", s.patchShot)
	mux.HandleFunc("DELETE /api/screenshots/{id}", s.deleteShot)
	mux.HandleFunc("GET /screenshots/{gid}/{name}", s.serveShot)
	mux.HandleFunc("GET /api/lookup", s.lookup)
	mux.HandleFunc("GET /api/igdb/{id}", s.igdbGame)
	mux.HandleFunc("GET /api/series/missing", s.seriesMissing)
	mux.HandleFunc("POST /api/series/refresh", s.seriesRefresh)
	mux.HandleFunc("POST /api/series/hide", s.seriesHide)
	mux.HandleFunc("POST /api/series/unhide", s.seriesUnhide)
	mux.HandleFunc("GET /api/similar", s.similarList)
	mux.HandleFunc("POST /api/similar/refresh", s.similarRefresh)
	mux.HandleFunc("POST /api/similar/hide", s.similarHide)
	mux.HandleFunc("POST /api/similar/unhide", s.similarUnhide)
	mux.HandleFunc("GET /api/deals", s.dealsList)
	mux.HandleFunc("POST /api/deals/refresh", s.dealsRefresh)
	mux.HandleFunc("GET /api/deals/search", s.dealsSearch)
	mux.HandleFunc("PUT /api/deals/country", s.dealsSetCountry)
	mux.HandleFunc("PUT /api/games/{id}/deal-match", s.dealsSetMatch)
	mux.HandleFunc("GET /api/covers/search", s.searchCovers)
	mux.HandleFunc("GET /api/hltb/search", s.hltbSearch)
	mux.HandleFunc("GET /api/facets", s.facets)
	mux.HandleFunc("GET /api/link/queue", s.linkQueue)
	mux.HandleFunc("POST /api/link/accept", s.linkAccept)
	mux.HandleFunc("POST /api/link/none", s.linkNone)
	mux.HandleFunc("GET /api/views", s.listViews)
	mux.HandleFunc("POST /api/views", s.createView)
	mux.HandleFunc("PUT /api/views/order", s.reorderViews)
	mux.HandleFunc("PATCH /api/views/{id}", s.patchView)
	mux.HandleFunc("DELETE /api/views/{id}", s.deleteView)
	mux.HandleFunc("GET /api/rankings", s.listRankings)
	mux.HandleFunc("POST /api/rankings", s.createRanking)
	mux.HandleFunc("PUT /api/rankings/order", s.reorderRankings)
	mux.HandleFunc("GET /api/rankings/{id}", s.getRanking)
	mux.HandleFunc("PATCH /api/rankings/{id}", s.patchRanking)
	mux.HandleFunc("DELETE /api/rankings/{id}", s.deleteRanking)
	mux.HandleFunc("POST /api/rankings/{id}/duplicate", s.duplicateRanking)
	mux.HandleFunc("PUT /api/rankings/{id}/board", s.putBoard)
	mux.HandleFunc("POST /api/rankings/{id}/tiers", s.addTier)
	mux.HandleFunc("PUT /api/rankings/{id}/tiers/order", s.reorderTiers)
	mux.HandleFunc("PATCH /api/rankings/{id}/tiers/{tid}", s.patchTier)
	mux.HandleFunc("DELETE /api/rankings/{id}/tiers/{tid}", s.deleteTier)
	mux.HandleFunc("POST /api/rankings/{id}/entries", s.addRankingEntries)
	mux.HandleFunc("POST /api/rankings/{id}/move", s.moveRankingEntries)
	mux.HandleFunc("DELETE /api/rankings/{id}/entries/{gid}", s.removeRankingEntry)
	mux.Handle("GET /covers/", http.StripPrefix("/covers/", cacheForever(filesOnly(http.FileServer(http.Dir(filepath.Clean(s.CoverDir)))))))
	return s.invalidating(mux)
}

// filesOnly answers 404 for a folder: the cover folder is never listed.
func filesOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
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
	writeErr(w, http.StatusInternalServerError, "internal error", nil)
}
