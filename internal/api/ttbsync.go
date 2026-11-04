package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/hltb"
)

// The statuses that need a time to beat. Keep in sync with web/static/js/ttbscope.js (the one rule Stats and Needs
// attention share): Stats estimates finished games and the waiting ones, so those are the games a missing time matters for.
const ttbStatusList = "'backlog', 'up_next', 'playing', 'finished'"

// ttbSyncGroups are the Needs attention groups the bulk lookup works for (their ignore lists are honoured).
var ttbSyncGroups = []string{"no_ttb", "no_ttb_finished"}

// ttbIgnored is the set of games ignored in any of the groups the bulk lookup works for.
func (s *Server) ttbIgnored() map[int64]bool {
	skip := map[int64]bool{}
	all := s.loadIgnored()
	for _, g := range ttbSyncGroups {
		for _, id := range all[g] {
			skip[id] = true
		}
	}
	return skip
}

// ttbJob is the state of the one background "Find times for all" run (the Needs attention page polls it).
type ttbJob struct {
	seriesJob
	cancel context.CancelFunc
	found  int
	manual []int64 // games whose search had no confident match: the owner picks by hand
}

type ttbStatus struct {
	seriesStatus
	Found  int     `json:"found"`
	Manual []int64 `json:"manual"`
}

func (j *ttbJob) ttbStatus() ttbStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	m := append([]int64{}, j.manual...)
	return ttbStatus{seriesStatus{Running: j.running, Done: j.done, Total: j.total, Finished: j.finished, Report: j.report, Failed: j.failed}, j.found, m}
}

func (s *Server) ttbSyncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.ttbJob.ttbStatus())
}

func (s *Server) ttbSyncCancel(w http.ResponseWriter, r *http.Request) {
	s.ttbJob.mu.Lock()
	if s.ttbJob.cancel != nil {
		s.ttbJob.cancel()
	}
	s.ttbJob.mu.Unlock()
	writeJSON(w, s.ttbJob.ttbStatus())
}

type ttbGame struct {
	id    int64
	title string
	year  int
}

// ttbTodo lists the games the lookup is for: in a status that needs a time, with no time at all, not ignored in a group
// and not marked "no time on HowLongToBeat".
func (s *Server) ttbTodo() ([]ttbGame, error) {
	skip := s.ttbIgnored()
	rows, err := s.DB.Query(`SELECT g.id, g.title, coalesce(g.release_year, 0) FROM game g JOIN status s ON s.id = g.status_id
		WHERE s.code IN (` + ttbStatusList + `) AND g.time_to_beat_hastily IS NULL AND g.time_to_beat_normally IS NULL
		AND g.time_to_beat_completely IS NULL AND g.ttb_not_found = 0 ORDER BY g.title_sort`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ttbGame
	for rows.Next() {
		var g ttbGame
		if err := rows.Scan(&g.id, &g.title, &g.year); err != nil {
			return nil, err
		}
		if !skip[g.id] {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

// ttbSyncStart searches HowLongToBeat for every game of the "No time to beat" group, in the background, one at a time
// (the client spaces searches 1 s apart, each gives up after 8 s). Only a confident match is saved.
func (s *Server) ttbSyncStart(w http.ResponseWriter, r *http.Request) {
	j := &s.ttbJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSONStatus(w, http.StatusAccepted, j.ttbStatus())
		return
	}
	todo, err := s.ttbTodo()
	if err != nil {
		j.mu.Unlock()
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	j.running, j.done, j.total, j.report, j.failed = true, 0, len(todo), "", false
	j.found, j.manual, j.cancel = 0, nil, cancel
	j.mu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		defer cancel()
		s.runTTBSync(ctx, todo)
	}()
	writeJSONStatus(w, http.StatusAccepted, j.ttbStatus())
}

func (s *Server) runTTBSync(ctx context.Context, todo []ttbGame) {
	j := &s.ttbJob
	client := s.hltbClient()
	failedSearches, consecutive := 0, 0
	stopped := ""  // the owner pressed Stop
	siteDown := "" // why the run gave up on the site
loop:
	for i, g := range todo {
		if ctx.Err() != nil {
			stopped = "Stopped."
			break
		}
		// the owner may have ignored the game since the run started
		if !s.ttbSkipped(g.id) {
			sctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			res, err := client.Search(sctx, g.title, g.year)
			cancel()
			switch {
			case err != nil && ctx.Err() != nil:
				stopped = "Stopped."
				break loop
			case err != nil:
				failedSearches++
				consecutive++
				if consecutive >= 3 || errors.Is(err, hltb.ErrChanged) {
					siteDown = err.Error() + ". Stopped."
					break loop
				}
			default:
				consecutive = 0
				if c, ok := confidentMatch(g, res); ok {
					saved, err := s.saveHLTBTimes(g.id, c)
					if err != nil {
						log.Printf("time to beat of game %d: %v", g.id, err)
					}
					if saved {
						s.cache.invalidate() // other clients see the new time at once, not at the end of the run
					}
					j.mu.Lock()
					if saved {
						j.found++
					}
					j.mu.Unlock()
				} else {
					j.mu.Lock()
					j.manual = append(j.manual, g.id)
					j.mu.Unlock()
				}
			}
		}
		j.mu.Lock()
		j.done = i + 1
		j.mu.Unlock()
	}
	// Drop the cached games before the run reads as finished: the page reloads them the moment it sees that.
	s.cache.invalidate()
	j.mu.Lock()
	j.running = false
	j.cancel = nil
	j.finished = time.Now().UTC().Format(time.RFC3339)
	j.report = fmt.Sprintf("Found times for %s; %d need a manual pick.", gamesText(j.found), len(j.manual))
	if failedSearches > 0 {
		j.report += fmt.Sprintf(" %s could not be searched.", gamesText(failedSearches))
	}
	if siteDown != "" {
		j.failed = true // only a site failure is an error; Stop is the owner's choice
		j.report = siteDown + " " + j.report
	} else if stopped != "" {
		j.report = stopped + " " + j.report
	}
	j.mu.Unlock()
}

func gamesText(n int) string {
	if n == 1 {
		return "1 game"
	}
	return fmt.Sprintf("%d games", n)
}

// confidentMatch is the one candidate that is certainly the game: exactly one candidate passes hltb.Confident.
func confidentMatch(g ttbGame, res []hltb.Candidate) (hltb.Candidate, bool) {
	var found []hltb.Candidate
	for _, c := range res {
		if hltb.Confident(g.title, g.year, c) {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		return hltb.Candidate{}, false
	}
	return found[0], true
}

// saveHLTBTimes stores a candidate's times only while the game still has no time at all (never over a time set by hand
// or picked from HowLongToBeat) and is still in a status the group covers.
func (s *Server) saveHLTBTimes(id int64, c hltb.Candidate) (bool, error) {
	nz := func(v int) any {
		if v > 0 {
			return v
		}
		return nil
	}
	res, err := s.DB.Exec(`UPDATE game SET time_to_beat_hastily = ?, time_to_beat_normally = ?, time_to_beat_completely = ?, time_to_beat_src = 'hltb'
		WHERE id = ? AND time_to_beat_hastily IS NULL AND time_to_beat_normally IS NULL AND time_to_beat_completely IS NULL
		AND status_id IN (SELECT id FROM status WHERE code IN (`+ttbStatusList+`))`,
		nz(c.Hastily), nz(c.Normally), nz(c.Completely), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ttbSkipped: the owner ignored the game, or marked it as having no time on HowLongToBeat, since the run started.
func (s *Server) ttbSkipped(id int64) bool {
	if s.ttbIgnored()[id] {
		return true
	}
	var marked bool
	if err := s.DB.QueryRow("SELECT ttb_not_found FROM game WHERE id = ?", id).Scan(&marked); err != nil {
		log.Printf("time to beat mark of game %d: %v", id, err)
	}
	return marked
}

// ttbNone marks (or unmarks) a game as having no time on HowLongToBeat: {"game_id": 3, "none": true}. It does not
// touch updated_at (the mark is a note about the lookup, not an edit of the game).
func (s *Server) ttbNone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GameID int64 `json:"game_id"`
		None   bool  `json:"none"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.GameID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	res, err := s.DB.Exec("UPDATE game SET ttb_not_found = ? WHERE id = ?", in.None, in.GameID)
	if err != nil {
		fail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "game not found", nil)
		return
	}
	s.cache.invalidate()
	writeJSON(w, map[string]any{"game_id": in.GameID, "none": in.None})
}
