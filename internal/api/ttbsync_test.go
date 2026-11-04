package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/hltb"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

// bulkHLTB answers per title and records every search; block (when set) holds each search until it is closed or the
// context ends, so a test can cancel a run in the middle.
type bulkHLTB struct {
	mu      sync.Mutex
	answers map[string][]hltb.Candidate
	seen    []string
	block   chan struct{}
	err     error
}

func (f *bulkHLTB) Search(ctx context.Context, title string, year int) ([]hltb.Candidate, error) {
	f.mu.Lock()
	f.seen = append(f.seen, title)
	block, err := f.block, f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.answers[title], err
}

const bulkCSV = `id,Title,Platform,Status,Style,VR,Genre,Boxart,Completed,Years_played,Series,Notas,Ownership,Played_with,Favorito,nc_order
1,Portal,PC,Backlog,,f,,,f,,,,Owned,,f,1
2,Doom,PC,Backlog,,f,,,f,,,,Owned,,f,2
3,Celeste,PC,Backlog,,f,,,f,,,,Owned,,f,3
4,Hollow Knight,PC,Backlog,,f,,,f,,,,Owned,,f,4
5,Tetris,PC,Finished,,f,,,f,,,,Owned,,f,5
6,Zelda,PC,Up next,,f,,,f,,,,Owned,,f,6
`

func bulkServer(t *testing.T, f *bulkHLTB) (*Server, *httptest.Server, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(bulkCSV), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec("UPDATE game SET release_year = 2017 WHERE title = 'Zelda'")
	d.Exec("UPDATE game SET time_to_beat_normally = 777, time_to_beat_src = 'manual' WHERE title = 'Celeste'")
	s := &Server{DB: d, CoverDir: t.TempDir(), HLTB: f}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv, d
}

func waitIdle(t *testing.T, s *Server) ttbStatus {
	t.Helper()
	for i := 0; i < 200; i++ {
		if st := s.ttbJob.ttbStatus(); !st.Running {
			return st
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
	return ttbStatus{}
}

func normalTime(t *testing.T, d *sql.DB, title string) (n sql.NullInt64, src sql.NullString) {
	t.Helper()
	d.QueryRow("SELECT time_to_beat_normally, time_to_beat_src FROM game WHERE title = ?", title).Scan(&n, &src)
	return
}

func TestBulkHLTBSavesOnlyConfidentMatches(t *testing.T) {
	f := &bulkHLTB{answers: map[string][]hltb.Candidate{
		"Portal":        {{ID: 1, Name: "Portal", Year: 2007, Hastily: 180, Normally: 300, Completely: 600}},
		"Doom":          {{ID: 2, Name: "Doom", Year: 1993, Normally: 400}, {ID: 3, Name: "DOOM", Year: 2016, Normally: 700}}, // two namesakes, no year: a guess
		"Zelda":         {{ID: 4, Name: "Zelda", Year: 2011, Normally: 900}},                                                  // another year
		"Hollow Knight": {{ID: 5, Name: "Hollow Knight", Year: 2017, Normally: 1500}},
		"Celeste":       {{ID: 6, Name: "Celeste", Normally: 500}},
	}}
	s, srv, d := bulkServer(t, f)
	postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "no_ttb", "game_id": 4, "ignore": true})

	res, out := postJSON(t, srv.URL+"/api/ttb/sync", map[string]any{})
	if res.StatusCode != http.StatusAccepted || out["total"] != float64(4) {
		t.Fatalf("start: %d %v (the groups are Portal, Doom, Zelda and the finished Tetris)", res.StatusCode, out)
	}
	st := waitIdle(t, s)
	s.bg.Wait()

	if n, src := normalTime(t, d, "Portal"); n.Int64 != 300 || src.String != "hltb" {
		t.Errorf("Portal = %v %v, want 300 from hltb", n, src)
	}
	for _, title := range []string{"Doom", "Zelda", "Hollow Knight", "Tetris"} {
		if n, _ := normalTime(t, d, title); n.Valid {
			t.Errorf("%s got a time (%d)", title, n.Int64)
		}
	}
	if n, src := normalTime(t, d, "Celeste"); n.Int64 != 777 || src.String != "manual" {
		t.Errorf("a time set by hand was replaced: %v %v", n, src)
	}
	if len(st.Manual) != 3 || st.Found != 1 {
		t.Errorf("status = %+v", st)
	}
	if st.Report != "Found times for 1 game; 3 need a manual pick." || st.Failed {
		t.Errorf("report = %q failed=%v", st.Report, st.Failed)
	}
	for _, title := range f.seen {
		if title == "Hollow Knight" || title == "Celeste" {
			t.Errorf("%s must not be searched", title)
		}
	}
}

func TestBulkHLTBKeepsATimeSetMeanwhile(t *testing.T) {
	s, _, d := bulkServer(t, &bulkHLTB{})
	d.Exec("UPDATE game SET time_to_beat_hastily = 60, time_to_beat_src = 'manual' WHERE title = 'Portal'")
	saved, err := s.saveHLTBTimes(1, hltb.Candidate{Normally: 300})
	if err != nil || saved {
		t.Errorf("saved over an existing time: %v %v", saved, err)
	}
}

func TestBulkHLTBCancel(t *testing.T) {
	f := &bulkHLTB{block: make(chan struct{}), answers: map[string][]hltb.Candidate{}}
	s, srv, _ := bulkServer(t, f)
	postJSON(t, srv.URL+"/api/ttb/sync", map[string]any{})
	if res, _ := postJSON(t, srv.URL+"/api/ttb/sync", map[string]any{}); res.StatusCode != http.StatusAccepted {
		t.Errorf("a second start while running: %d", res.StatusCode)
	}
	postJSON(t, srv.URL+"/api/ttb/sync/cancel", map[string]any{})
	st := waitIdle(t, s)
	s.bg.Wait()
	if st.Failed || !strings.HasPrefix(st.Report, "Stopped.") || st.Done >= st.Total {
		t.Errorf("cancelled run = %+v", st)
	}
}

func TestBulkHLTBStopsWhenTheSiteIsDown(t *testing.T) {
	f := &bulkHLTB{err: hltb.ErrUnavailable}
	s, srv, _ := bulkServer(t, f)
	postJSON(t, srv.URL+"/api/ttb/sync", map[string]any{})
	st := waitIdle(t, s)
	s.bg.Wait()
	if !st.Failed || !strings.Contains(st.Report, "can't be reached") || len(f.seen) != 3 {
		t.Errorf("status = %+v, searches = %d", st, len(f.seen))
	}
}

func TestHLTBConfident(t *testing.T) {
	c := hltb.Candidate{Name: "The Witcher 3: Wild Hunt", Year: 2015}
	for _, tc := range []struct {
		title string
		year  int
		want  bool
	}{
		{"the witcher 3 wild hunt", 2015, true},
		{"The Witcher 3: Wild Hunt", 0, true},
		{"The Witcher 3: Wild Hunt", 2016, false},
		{"The Witcher 3", 2015, false},
		{"", 0, false},
	} {
		if got := hltb.Confident(tc.title, tc.year, c); got != tc.want {
			t.Errorf("Confident(%q, %d) = %v", tc.title, tc.year, got)
		}
	}
	if !hltb.Confident("Portal", 2007, hltb.Candidate{Name: "Portal"}) {
		t.Error("a candidate without a year matches on the title")
	}
}

// A finished game is searched like the others, but a game marked "no time on HowLongToBeat" never is.
func TestBulkHLTBSkipsMarkedGamesAndSearchesFinishedOnes(t *testing.T) {
	f := &bulkHLTB{answers: map[string][]hltb.Candidate{
		"Tetris": {{ID: 7, Name: "Tetris", Normally: 120}},
		"Portal": {{ID: 1, Name: "Portal", Normally: 300}},
	}}
	s, srv, d := bulkServer(t, f)
	res, out := postJSON(t, srv.URL+"/api/ttb/none", map[string]any{"game_id": 1, "none": true})
	if res.StatusCode != http.StatusOK || out["none"] != true {
		t.Fatalf("mark: %d %v", res.StatusCode, out)
	}
	res, _ = postJSON(t, srv.URL+"/api/ttb/sync", map[string]any{})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", res.StatusCode)
	}
	waitIdle(t, s)
	s.bg.Wait()
	if n, src := normalTime(t, d, "Tetris"); n.Int64 != 120 || src.String != "hltb" {
		t.Errorf("finished Tetris = %v %v, want 120 from hltb", n, src)
	}
	if n, _ := normalTime(t, d, "Portal"); n.Valid {
		t.Errorf("a marked game got a time (%d)", n.Int64)
	}
	for _, title := range f.seen {
		if title == "Portal" {
			t.Error("a marked game was searched")
		}
	}
}

func TestTTBNoneMarkUndoAndGamesJSON(t *testing.T) {
	_, srv, d := bulkServer(t, &bulkHLTB{})
	flag := func() bool {
		var marked bool
		d.QueryRow("SELECT ttb_not_found FROM game WHERE id = 1").Scan(&marked)
		return marked
	}
	var before string
	d.QueryRow("SELECT updated_at FROM game WHERE id = 1").Scan(&before)
	if res, _ := postJSON(t, srv.URL+"/api/ttb/none", map[string]any{"game_id": 1, "none": true}); res.StatusCode != http.StatusOK || !flag() {
		t.Fatalf("mark failed (%d)", res.StatusCode)
	}
	var after string
	d.QueryRow("SELECT updated_at FROM game WHERE id = 1").Scan(&after)
	if after != before {
		t.Errorf("the mark changed updated_at: %s -> %s", before, after)
	}
	_, body := do(t, "GET", srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	for _, g := range games {
		if g.Title == "Portal" && !g.TTBNotFound {
			t.Error("GET /api/games does not carry the mark")
		}
		if g.Title == "Doom" && g.TTBNotFound {
			t.Error("Doom should not be marked")
		}
	}
	if res, _ := postJSON(t, srv.URL+"/api/ttb/none", map[string]any{"game_id": 1, "none": false}); res.StatusCode != http.StatusOK || flag() {
		t.Fatalf("undo failed (%d)", res.StatusCode)
	}
	if res, _ := postJSON(t, srv.URL+"/api/ttb/none", map[string]any{"game_id": 999, "none": true}); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game: %d, want 404", res.StatusCode)
	}
	if res, _ := postJSON(t, srv.URL+"/api/ttb/none", map[string]any{"none": true}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("no game id: %d, want 400", res.StatusCode)
	}
}

// The server's list of statuses and the browser's rule (web/static/js/ttbscope.js) are two copies of one rule.
func TestTTBStatusListMatchesTheBrowserRule(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "js", "ttbscope.js"))
	if err != nil {
		t.Fatal(err)
	}
	var js []string
	for _, m := range regexp.MustCompile(`\{ status: '(\w+)'`).FindAllStringSubmatch(string(src), -1) {
		js = append(js, m[1])
	}
	var goList []string
	for _, p := range strings.Split(ttbStatusList, ",") {
		goList = append(goList, strings.Trim(strings.TrimSpace(p), "'"))
	}
	sort.Strings(js)
	sort.Strings(goList)
	if len(js) == 0 || strings.Join(js, ",") != strings.Join(goList, ",") {
		t.Errorf("ttbscope.js has %v, ttbsync.go has %v", js, goList)
	}
}
