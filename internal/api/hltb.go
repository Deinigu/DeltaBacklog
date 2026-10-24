package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/hltb"
)

// HLTB is what the time-to-beat lookup needs from the HowLongToBeat client (satisfied by *hltb.Client).
type HLTB interface {
	Search(ctx context.Context, title string, year int) ([]hltb.Candidate, error)
}

func (s *Server) hltbClient() HLTB {
	s.hltbOnce.Do(func() {
		if s.HLTB == nil {
			s.HLTB = hltb.New()
		}
	})
	return s.HLTB
}

// hltbSearch lists HowLongToBeat candidates for ?q=title (and ?year= to put the right namesake first). The site is
// an unofficial dependency: when it is down or has changed, the answer is a 502 with a message the drawer shows as is.
func (s *Server) hltbSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 200 {
		writeErr(w, http.StatusBadRequest, "a title is required", nil)
		return
	}
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.hltbClient().Search(ctx, q, year)
	if err != nil {
		msg := hltb.ErrUnavailable.Error()
		if errors.Is(err, hltb.ErrChanged) {
			msg = hltb.ErrChanged.Error()
		}
		writeErr(w, http.StatusBadGateway, msg, nil)
		return
	}
	if res == nil {
		res = []hltb.Candidate{}
	}
	writeJSON(w, map[string]any{"query": q, "results": res})
}
