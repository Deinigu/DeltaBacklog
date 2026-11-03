package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/hltb"
)

// ttbSyncGroup is the Needs attention group the bulk lookup works for (its ignore list is honoured).
const ttbSyncGroup = "no_ttb"

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

// ttbTodo lists the games the lookup is for: in Backlog, Up Next or Playing, with no time at all, not ignored in the group.
func (s *Server) ttbTodo() ([]ttbGame, error) {
	skip := map[int64]bool{}
	for _, id := range s.loadIgnored()[ttbSyncGroup] {
		skip[id] = true
	}
	rows, err := s.DB.Query(`SELECT g.id, g.title, coalesce(g.release_year, 0) FROM game g JOIN status s ON s.id = g.status_id
		WHERE s.code IN ('backlog', 'up_next', 'playing') AND g.time_to_beat_hastily IS NULL AND g.time_to_beat_normally IS NULL
		AND g.time_to_beat_completely IS NULL ORDER BY g.title_sort`)
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
	stopped := ""
loop:
	for i, g := range todo {
		if ctx.Err() != nil {
			stopped = "Stopped."
			break
		}
		// the owner may have ignored the game since the run started
		ignored := false
		for _, id := range s.loadIgnored()[ttbSyncGroup] {
			ignored = ignored || id == g.id
		}
		if !ignored {
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
					stopped = err.Error() + ". Stopped."
					break loop
				}
			default:
				consecutive = 0
				if c, ok := confidentMatch(g, res); ok {
					saved, err := s.saveHLTBTimes(g.id, c)
					if err != nil {
						log.Printf("time to beat of game %d: %v", g.id, err)
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
	j.mu.Lock()
	j.running = false
	j.cancel = nil
	j.finished = time.Now().UTC().Format(time.RFC3339)
	j.report = fmt.Sprintf("Found times for %s; %d need a manual pick.", gamesText(j.found), len(j.manual))
	if failedSearches > 0 {
		j.report += fmt.Sprintf(" %s could not be searched.", gamesText(failedSearches))
	}
	if stopped != "" {
		j.failed = true
		j.report = stopped + " " + j.report
	}
	j.mu.Unlock()
	s.cache.invalidate()
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
		AND status_id IN (SELECT id FROM status WHERE code IN ('backlog', 'up_next', 'playing'))`,
		nz(c.Hastily), nz(c.Normally), nz(c.Completely), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
