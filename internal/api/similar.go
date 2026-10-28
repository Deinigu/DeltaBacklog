package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
)

// similarSource is the IGDB client used to read similar games: the server's Similar field (tests) or Meta.
func (s *Server) similarSource() collections.SimilarSource {
	if s.Similar != nil {
		return s.Similar
	}
	if src, ok := s.Meta.(collections.SimilarSource); ok {
		return src
	}
	return nil
}

// similarList answers the cached suggestions (never IGDB): the similar games of the finished and favourite games that the
// library does not have, with the roots the browser needs to keep applying the owned rule.
func (s *Server) similarList(w http.ResponseWriter, r *http.Request) {
	list, err := collections.LoadSimilar(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"configured": s.igdbConfigured(), "suggestions": list.Suggestions, "owned_roots": list.OwnedRoots,
		"fetched_at": list.FetchedAt, "refresh": s.similarJob.status()})
}

// similarRefresh starts reading the similar games from IGDB in the background; the answer is the job's status. One run at
// a time: asking again while it runs just answers its status.
func (s *Server) similarRefresh(w http.ResponseWriter, r *http.Request) {
	src := s.similarSource()
	if src == nil || (s.Similar == nil && !s.igdbConfigured()) {
		writeErr(w, http.StatusServiceUnavailable, "IGDB is not set up (keys are missing from .env)", nil)
		return
	}
	j := &s.similarJob
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
		rep, err := collections.RefreshSimilar(ctx, s.DB, src, collections.Options{
			CoverDir: s.CoverDir, Fetch: s.MissingPosterFetch, Allowed: s.coverHostAllowed,
			Progress: func(done, total int) { j.mu.Lock(); j.done, j.total = done, total; j.mu.Unlock() },
		})
		j.mu.Lock()
		j.running = false
		j.finished = time.Now().UTC().Format(time.RFC3339)
		switch {
		case err != nil:
			log.Printf("similar refresh: %v", err)
			j.failed, j.report = true, "The refresh failed."
		case rep.Stopped != "":
			j.failed, j.report = true, rep.SimilarString()
		default:
			j.report = rep.SimilarString()
		}
		j.mu.Unlock()
	}()
	writeJSONStatus(w, http.StatusAccepted, j.status())
}

type similarHideBody struct {
	IGDBID int64 `json:"igdb_id"`
}

func (s *Server) similarHide(w http.ResponseWriter, r *http.Request) { s.setSimilarHidden(w, r, true) }
func (s *Server) similarUnhide(w http.ResponseWriter, r *http.Request) {
	s.setSimilarHidden(w, r, false)
}

func (s *Server) setSimilarHidden(w http.ResponseWriter, r *http.Request, hide bool) {
	var in similarHideBody
	if !readJSON(w, r, &in, 4<<10) {
		return
	}
	if in.IGDBID <= 0 {
		bad(w, "invalid body")
		return
	}
	var err error
	if hide {
		err = collections.HideSimilar(s.DB, in.IGDBID)
	} else {
		err = collections.UnhideSimilar(s.DB, in.IGDBID)
	}
	switch {
	case errors.Is(err, collections.ErrUnknownSimilar):
		writeErr(w, http.StatusNotFound, "that game is not a cached suggestion", nil)
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, map[string]bool{"ok": true})
	}
}
