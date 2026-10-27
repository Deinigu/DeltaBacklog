package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// seriesJob is the state of the one background "Refresh from IGDB" run (the Series page polls it).
type seriesJob struct {
	mu       sync.Mutex
	running  bool
	done     int
	total    int
	finished string // RFC 3339 time the last run ended; "" before the first
	report   string // its summary, or the reason it failed
	failed   bool
}

type seriesStatus struct {
	Running  bool   `json:"running"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Finished string `json:"finished_at,omitempty"`
	Report   string `json:"report,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
}

func (j *seriesJob) status() seriesStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return seriesStatus{Running: j.running, Done: j.done, Total: j.total, Finished: j.finished, Report: j.report, Failed: j.failed}
}

// collectionSource is the IGDB client used to read collections: the server's Collections field (tests) or Meta.
func (s *Server) collectionSource() collections.Source {
	if s.Collections != nil {
		return s.Collections
	}
	if src, ok := s.Meta.(collections.Source); ok {
		return src
	}
	return nil
}

func (s *Server) igdbConfigured() bool {
	if s.Meta == nil {
		return false
	}
	ok, _ := s.Meta.Configured()
	return ok
}

// seriesMissing answers the games of the series' IGDB collections that the library lacks, from the cache (never IGDB).
func (s *Server) seriesMissing(w http.ResponseWriter, r *http.Request) {
	list, err := collections.Load(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	if list == nil {
		list = []collections.Series{}
	}
	roots, err := collections.OwnedRoots(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"configured": s.igdbConfigured(), "series": list, "owned_roots": roots, "refresh": s.seriesJob.status()})
}

// seriesRefresh starts reading the collections from IGDB in the background; the answer is the job's status.
func (s *Server) seriesRefresh(w http.ResponseWriter, r *http.Request) {
	src := s.collectionSource()
	if src == nil || (s.Collections == nil && !s.igdbConfigured()) {
		writeErr(w, http.StatusServiceUnavailable, "IGDB is not set up (keys are missing from .env)", nil)
		return
	}
	j := &s.seriesJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSONStatus(w, http.StatusAccepted, j.status())
		return
	}
	j.running, j.done, j.total, j.report, j.failed = true, 0, 0, "", false
	j.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		fetch := s.MissingPosterFetch // tests replace the download
		rep, err := collections.Refresh(ctx, s.DB, src, collections.Options{
			CoverDir: s.CoverDir, Fetch: fetch, Allowed: s.coverHostAllowed,
			Progress: func(done, total int) { j.mu.Lock(); j.done, j.total = done, total; j.mu.Unlock() },
		})
		j.mu.Lock()
		j.running = false
		j.finished = time.Now().UTC().Format(time.RFC3339)
		switch {
		case err != nil:
			log.Printf("series refresh: %v", err)
			j.failed, j.report = true, "The refresh failed."
		case rep.Stopped != "":
			j.failed, j.report = true, rep.String()
		default:
			j.report = rep.String()
		}
		j.mu.Unlock()
		s.cache.invalidate()
	}()
	writeJSONStatus(w, http.StatusAccepted, j.status())
}

type hideBody struct {
	CollectionID int64 `json:"collection_id"`
	IGDBID       int64 `json:"igdb_id"`
}

func (s *Server) seriesHide(w http.ResponseWriter, r *http.Request)   { s.setHidden(w, r, true) }
func (s *Server) seriesUnhide(w http.ResponseWriter, r *http.Request) { s.setHidden(w, r, false) }

func (s *Server) setHidden(w http.ResponseWriter, r *http.Request, hide bool) {
	var in hideBody
	if !readJSON(w, r, &in, 4<<10) {
		return
	}
	if in.CollectionID <= 0 || in.IGDBID <= 0 {
		bad(w, "invalid body")
		return
	}
	var err error
	if hide {
		err = collections.Hide(s.DB, in.CollectionID, in.IGDBID)
	} else {
		err = collections.Unhide(s.DB, in.CollectionID, in.IGDBID)
	}
	switch {
	case errors.Is(err, collections.ErrUnknown):
		writeErr(w, http.StatusNotFound, "that game is not in the cached collection", nil)
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// candidateReader is what the Add button needs beyond Meta: one IGDB game as a quick-add candidate.
type candidateReader interface {
	GameCandidate(ctx context.Context, igdbID int64) (meta.Candidate, error)
}

// igdbGame answers one IGDB game as a quick-add candidate, so a "not registered" game opens the add form pre-filled.
func (s *Server) igdbGame(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		bad(w, "invalid id")
		return
	}
	cr, ok := s.Meta.(candidateReader)
	if !ok || !s.igdbConfigured() {
		writeErr(w, http.StatusServiceUnavailable, "IGDB is not set up (keys are missing from .env)", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := cr.GameCandidate(ctx, id)
	switch {
	case errors.Is(err, meta.ErrNoGame):
		writeErr(w, http.StatusNotFound, "IGDB has no such game", nil)
	case err != nil:
		writeErr(w, http.StatusBadGateway, "IGDB lookup failed", nil)
	default:
		// The root of the game's version chain, so the add form can hand it back and the game's other entries (the
		// original, an expanded edition) stop being listed as missing at once. A failed walk just leaves it unknown.
		if src := s.collectionSource(); src != nil {
			if root, err := collections.RootOf(ctx, src, id, c.ParentID); err == nil {
				c.ParentID = root
			} else {
				c.ParentID = 0
			}
		} else {
			c.ParentID = 0
		}
		writeJSON(w, c)
	}
}

// fillReleaseLater runs fillRelease after the response, so a slow or unreachable IGDB never holds up an add or a link.
func (s *Server) fillReleaseLater(gameID, igdbID int64) {
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		s.fillRelease(gameID, igdbID)
	}()
}

// fillRelease stores the IGDB release date of a game just added or linked, for the Upcoming page. It is best effort:
// without IGDB (or when the lookup fails) the date stays empty until the next Refresh from IGDB or `sync-igdb --enrich`.
func (s *Server) fillRelease(gameID, igdbID int64) {
	rs, ok := s.Meta.(collections.ReleaseSource)
	if !ok || !s.igdbConfigured() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got, err := rs.ReleaseDates(ctx, []int64{igdbID})
	if err != nil {
		log.Printf("release date of IGDB game %d: %v", igdbID, err)
		return
	}
	if r, ok := got[igdbID]; ok {
		if err := collections.SetReleaseIfLinked(s.DB, gameID, igdbID, r.Release); err != nil {
			log.Printf("release date of game %d: %v", gameID, err)
		}
	}
}
