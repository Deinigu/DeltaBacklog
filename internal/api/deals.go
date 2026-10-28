package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

// dealsConfigured: there is an ITAD client with a key.
func (s *Server) dealsConfigured() bool { return s.Deals != nil && s.Deals.Configured() }

func (s *Server) dealsError(w http.ResponseWriter, err error) {
	var rl *deals.RateLimitError
	switch {
	case errors.Is(err, deals.ErrNotConfigured):
		writeErr(w, http.StatusServiceUnavailable, err.Error(), nil)
	case errors.As(err, &rl):
		writeErr(w, http.StatusTooManyRequests, err.Error(), nil)
	case errors.Is(err, deals.ErrUnknownGame):
		notFound(w)
	default:
		log.Printf("deals: %v", err)
		writeErr(w, http.StatusBadGateway, "IsThereAnyDeal could not answer", nil)
	}
}

// dealsList answers the cached matches and prices (never ITAD).
func (s *Server) dealsList(w http.ResponseWriter, r *http.Request) {
	l, err := deals.Load(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"configured": s.dealsConfigured(), "country": l.Country, "matches": l.Matches, "prices": l.Prices,
		"refresh": s.dealsJob.status()})
}

// dealsRefresh starts reading the prices in the background; the answer is the job's status. One run at a time.
func (s *Server) dealsRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.dealsConfigured() {
		writeErr(w, http.StatusServiceUnavailable, deals.ErrNotConfigured.Error(), nil)
		return
	}
	j := &s.dealsJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSONStatus(w, http.StatusAccepted, j.status())
		return
	}
	j.running, j.done, j.total, j.report, j.failed = true, 0, 0, "", false
	j.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		rep, err := deals.Refresh(ctx, s.DB, s.Deals, deals.Options{
			Progress: func(done, total int) { j.mu.Lock(); j.done, j.total = done, total; j.mu.Unlock() },
		})
		j.mu.Lock()
		j.running = false
		j.finished = time.Now().UTC().Format(time.RFC3339)
		switch {
		case err != nil:
			log.Printf("deals refresh: %v", err)
			j.failed, j.report = true, "The refresh failed."
		case rep.Stopped != "":
			j.failed, j.report = true, rep.String()
		default:
			j.report = rep.String()
		}
		j.mu.Unlock()
	}()
	writeJSONStatus(w, http.StatusAccepted, j.status())
}

// dealsSearch lists ITAD games for a title, for the owner to pick the right one.
func (s *Server) dealsSearch(w http.ResponseWriter, r *http.Request) {
	if !s.dealsConfigured() {
		writeErr(w, http.StatusServiceUnavailable, deals.ErrNotConfigured.Error(), nil)
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if title == "" || len(title) > 200 {
		bad(w, "title is required")
		return
	}
	list, err := s.Deals.Search(r.Context(), title)
	if err != nil {
		s.dealsError(w, err)
		return
	}
	if list == nil {
		list = []deals.Game{}
	}
	writeJSON(w, map[string]any{"games": list})
}

type dealMatchBody struct {
	ITADID string `json:"itad_id"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	None   bool   `json:"none"`
	Reset  bool   `json:"reset"`
}

// dealsSetMatch records the owner's choice for a game: an ITAD game, "no match", or back to automatic.
func (s *Server) dealsSetMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		notFound(w)
		return
	}
	var in dealMatchBody
	if !readJSON(w, r, &in, 4<<10) {
		return
	}
	in.ITADID, in.Title, in.Slug = strings.TrimSpace(in.ITADID), strings.TrimSpace(in.Title), strings.TrimSpace(in.Slug)
	picked := in.ITADID != ""
	if n := b2n(picked) + b2n(in.None) + b2n(in.Reset); n != 1 || len(in.ITADID) > 64 || len(in.Title) > 300 || len(in.Slug) > 300 {
		bad(w, "invalid body")
		return
	}
	var err error
	switch {
	case in.Reset:
		err = deals.ClearMatch(s.DB, id)
	case in.None:
		err = deals.SetMatch(s.DB, id, deals.Game{})
	default:
		err = deals.SetMatch(s.DB, id, deals.Game{ID: in.ITADID, Title: in.Title, Slug: in.Slug})
	}
	if err != nil {
		s.dealsError(w, err)
		return
	}
	// A picked game gets its prices at once (one request); if ITAD cannot answer now the next refresh does it.
	if picked && s.dealsConfigured() {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := deals.RefreshOne(ctx, s.DB, s.Deals, id); err != nil {
			log.Printf("deals: prices of game %d: %v", id, err)
		}
	}
	s.dealsList(w, r)
}

func b2n(b bool) int {
	if b {
		return 1
	}
	return 0
}

type dealCountryBody struct {
	Country string `json:"country"`
}

// dealsSetCountry changes the country the prices are read for; the cached prices are dropped.
func (s *Server) dealsSetCountry(w http.ResponseWriter, r *http.Request) {
	var in dealCountryBody
	if !readJSON(w, r, &in, 1<<10) {
		return
	}
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	if !deals.ValidCountry(in.Country) {
		bad(w, "country must be a two-letter code such as ES")
		return
	}
	if err := deals.SetCountry(s.DB, in.Country); err != nil {
		fail(w, err)
		return
	}
	s.dealsList(w, r)
}
