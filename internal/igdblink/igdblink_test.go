package igdblink

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// fakeIGDB is the offline IGDB: canned answers per title, a call log, and optional 429s.
type fakeIGDB struct {
	answers   map[string][]meta.Candidate
	calls     []string
	throttle  int // answer the next N calls with 429
	failTitle string
}

func (f *fakeIGDB) SearchIGDB(_ context.Context, title string) ([]meta.Candidate, error) {
	f.calls = append(f.calls, title)
	if f.throttle > 0 {
		f.throttle--
		return nil, &meta.HTTPError{Method: "POST", Host: "igdb", Status: 429, Text: "429 Too Many Requests"}
	}
	if title == f.failTitle {
		return nil, &meta.HTTPError{Method: "POST", Host: "igdb", Status: 500, Text: "500"}
	}
	return f.answers[title], nil
}

func cand(id int64, name string, year int, plats ...string) meta.Candidate {
	return meta.Candidate{IGDBID: id, Name: name, Year: year, Platforms: plats}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func addGame(t *testing.T, d *sql.DB, title string, platforms ...string) int64 {
	t.Helper()
	res, err := d.Exec("INSERT INTO game (title, title_sort, status_id) VALUES (?, ?, 4)", title, strings.ToLower(title))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for _, p := range platforms {
		pid, err := db.GetOrCreate(d, "platform", p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec("INSERT INTO game_platform (game_id, platform_id) VALUES (?, ?)", id, pid); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func noSleep(context.Context, time.Duration) error { return nil }

// library: five games, one per outcome (plus an ambiguous one and a platform mismatch).
func seed(t *testing.T) (*sql.DB, *fakeIGDB, map[string]int64) {
	d := openDB(t)
	ids := map[string]int64{
		"Hades":           addGame(t, d, "Hades", "PC", "Switch"),
		"Hades II":        addGame(t, d, "Hades II", "PC"),
		"Prey":            addGame(t, d, "Prey", "PC"),
		"Obscure Thing":   addGame(t, d, "Obscure Thing", "PC"),
		"Celeste":         addGame(t, d, "Celeste", "PS4"),
		"Already Linked":  addGame(t, d, "Already Linked", "PC"),
		"Resident Evil 2": addGame(t, d, "Resident Evil 2", "PC"),
	}
	if _, err := d.Exec("UPDATE game SET igdb_id = 777 WHERE title = 'Already Linked'"); err != nil {
		t.Fatal(err)
	}
	f := &fakeIGDB{answers: map[string][]meta.Candidate{
		"Hades":           {cand(1, "Hades", 2020, "PC", "Nintendo Switch"), cand(2, "Hades II", 2024, "PC")},
		"Hades II":        {cand(2, "Hades II", 2024, "PC")},
		"Prey":            {cand(10, "Prey", 2006, "PC"), cand(11, "Prey", 2017, "PC", "PS4")}, // two equally good: ambiguous
		"Celeste":         {cand(20, "Celeste", 2018, "PC", "Switch")},                         // right game, no shared platform
		"Resident Evil 2": {cand(30, "Resident Evil 3", 1999, "PC")},                           // different number: never a match
	}}
	return d, f, ids
}

func linkOf(t *testing.T, d *sql.DB, id int64) (igdb sql.NullInt64, checked sql.NullString, synced sql.NullString) {
	t.Helper()
	if err := d.QueryRow("SELECT igdb_id, igdb_checked_at, igdb_synced_at FROM game WHERE id = ?", id).Scan(&igdb, &checked, &synced); err != nil {
		t.Fatal(err)
	}
	return
}

func TestBuckets(t *testing.T) {
	d, f, ids := seed(t)
	rep, err := Run(context.Background(), d, f, Options{Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 6 || rep.Confident != 2 || rep.Uncertain != 2 || rep.NotFound != 2 || rep.Stopped != "" {
		t.Fatalf("report = %+v", rep)
	}
	// Hades and Hades II: confident, with igdb_id and synced_at.
	for title, want := range map[string]int64{"Hades": 1, "Hades II": 2} {
		igdb, checked, synced := linkOf(t, d, ids[title])
		if igdb.Int64 != want || !checked.Valid || !synced.Valid {
			t.Errorf("%s: igdb=%v checked=%v synced=%v", title, igdb, checked, synced)
		}
	}
	// Uncertain games keep no link but get their candidates (Prey: both; Celeste: the one without platform overlap).
	for title, n := range map[string]int{"Prey": 2, "Celeste": 1} {
		igdb, checked, synced := linkOf(t, d, ids[title])
		if igdb.Valid || synced.Valid || !checked.Valid {
			t.Errorf("%s: igdb=%v checked=%v synced=%v", title, igdb, checked, synced)
		}
		var got int
		d.QueryRow("SELECT count(*) FROM sync_candidate WHERE game_id = ? AND status = 'pending'", ids[title]).Scan(&got)
		if got != n {
			t.Errorf("%s: %d candidates, want %d", title, got, n)
		}
	}
	// Resident Evil 2 vs 3: score capped below 60, so not found; no candidates stored.
	igdb, checked, _ := linkOf(t, d, ids["Resident Evil 2"])
	var stored int
	d.QueryRow("SELECT count(*) FROM sync_candidate WHERE game_id = ?", ids["Resident Evil 2"]).Scan(&stored)
	if igdb.Valid || !checked.Valid || stored != 0 {
		t.Errorf("RE2: igdb=%v checked=%v stored=%d", igdb, checked, stored)
	}
	// A game that already has an igdb_id is never searched.
	for _, c := range f.calls {
		if c == "Already Linked" {
			t.Error("searched a linked game")
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	d, f, _ := seed(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "b.db")
	var before, after string
	snap := func() string {
		var s string
		d.QueryRow(`SELECT (SELECT group_concat(coalesce(igdb_id,'-') || coalesce(igdb_checked_at,'-') || igdb_not_found) FROM game) ||
			(SELECT count(*) FROM sync_candidate)`).Scan(&s)
		return s
	}
	before = snap()
	rep, err := Run(context.Background(), d, f, Options{DryRun: true, Sleep: noSleep, BackupTo: backup})
	if err != nil {
		t.Fatal(err)
	}
	after = snap()
	if before != after {
		t.Errorf("dry run changed the database:\n%s\n%s", before, after)
	}
	if rep.Confident != 2 || rep.Uncertain != 2 || rep.NotFound != 2 || rep.BackupFile != "" {
		t.Errorf("report = %+v", rep)
	}
	var out bytes.Buffer
	PrintReport(&out, rep)
	if !strings.Contains(out.String(), "Dry run (nothing was written)") || !strings.Contains(out.String(), "Hades") {
		t.Errorf("report text:\n%s", out.String())
	}
	if _, err := db.Check(backup); err == nil {
		t.Error("a dry run wrote a backup")
	}
}

func TestBackupBeforeApplying(t *testing.T) {
	d, f, _ := seed(t)
	backup := filepath.Join(t.TempDir(), "b.db")
	rep, err := Run(context.Background(), d, f, Options{Sleep: noSleep, BackupTo: backup})
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.Check(backup)
	if err != nil || r.Integrity != "ok" || r.Games != 7 || rep.BackupFile != backup {
		t.Fatalf("backup: %+v err=%v rep=%+v", r, err, rep)
	}
	// The backup is the state before the run: nothing linked by the sync yet.
	b, _ := sql.Open("sqlite", "file:"+backup+"?mode=ro")
	defer b.Close()
	var linked int
	b.QueryRow("SELECT count(*) FROM game WHERE igdb_synced_at IS NOT NULL").Scan(&linked)
	if linked != 0 {
		t.Errorf("backup already holds %d synced games", linked)
	}
}

func TestResume(t *testing.T) {
	d, f, _ := seed(t)
	rep, err := Run(context.Background(), d, f, Options{Sleep: noSleep, Limit: 3})
	if err != nil || rep.Searched != 3 {
		t.Fatalf("first run: %+v %v", rep, err)
	}
	first := len(f.calls)
	rep, err = Run(context.Background(), d, f, Options{Sleep: noSleep})
	if err != nil || rep.Total != 3 || len(f.calls)-first != 3 {
		t.Fatalf("second run: %+v %v calls=%d", rep, err, len(f.calls)-first)
	}
	// Everything searched: a third run has nothing to do and makes no request.
	rep, _ = Run(context.Background(), d, f, Options{Sleep: noSleep})
	if rep.Total != 0 || len(f.calls) != first+3 {
		t.Errorf("third run: %+v calls=%d", rep, len(f.calls))
	}
}

func TestFailedSearchIsRetriedNextRun(t *testing.T) {
	d, f, ids := seed(t)
	f.failTitle = "Celeste"
	rep, err := Run(context.Background(), d, f, Options{Sleep: noSleep})
	if err != nil || rep.Errors != 1 || rep.Stopped != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if _, checked, _ := linkOf(t, d, ids["Celeste"]); checked.Valid {
		t.Error("a failed search marked the game as checked")
	}
	f.failTitle = ""
	rep, _ = Run(context.Background(), d, f, Options{Sleep: noSleep})
	if rep.Total != 1 || rep.Searched != 1 {
		t.Errorf("retry: %+v", rep)
	}
}

func TestBackoffOn429(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades", "PC")
	f := &fakeIGDB{answers: map[string][]meta.Candidate{"Hades": {cand(1, "Hades", 2020, "PC")}}, throttle: 3}
	var waits []time.Duration
	sleep := func(_ context.Context, w time.Duration) error { waits = append(waits, w); return nil }
	rep, err := Run(context.Background(), d, f, Options{Sleep: sleep, Interval: time.Millisecond, Backoff: time.Second})
	if err != nil || rep.Confident != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if fmt.Sprint(waits) != "[1s 2s 4s]" {
		t.Errorf("waits = %v, want doubling 1s 2s 4s", waits)
	}
	if igdb, _, _ := linkOf(t, d, id); igdb.Int64 != 1 {
		t.Error("not linked after the retries")
	}
}

func TestStopsCleanlyWhenThrottledForGood(t *testing.T) {
	d, f, ids := seed(t)
	f.answers["Celeste"] = nil
	// Hades links first, then every later request is throttled.
	calls := 0
	g := &throttleAfter{inner: f, after: 1, calls: &calls}
	rep, err := Run(context.Background(), d, g, Options{Sleep: noSleep, MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Stopped == "" || rep.Confident != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if igdb, _, _ := linkOf(t, d, ids["Hades"]); igdb.Int64 != 1 {
		t.Error("work before the stop was lost")
	}
	// Next run (throttle over) continues with the rest only.
	rep, _ = Run(context.Background(), d, f, Options{Sleep: noSleep})
	if rep.Stopped != "" || rep.Total != 5 {
		t.Errorf("resume: %+v", rep)
	}
}

type throttleAfter struct {
	inner *fakeIGDB
	after int
	calls *int
}

func (t *throttleAfter) SearchIGDB(ctx context.Context, title string) ([]meta.Candidate, error) {
	*t.calls++
	if *t.calls > t.after {
		return nil, &meta.HTTPError{Method: "POST", Host: "igdb", Status: http.StatusTooManyRequests, Text: "429"}
	}
	return t.inner.SearchIGDB(ctx, title)
}

func TestConfidentMatchAlreadyTakenGoesToReview(t *testing.T) {
	d := openDB(t)
	addGame(t, d, "Hades", "PC")
	other := addGame(t, d, "Hades (copy)", "PC")
	d.Exec("UPDATE game SET igdb_id = 1 WHERE id = ?", other)
	f := &fakeIGDB{answers: map[string][]meta.Candidate{"Hades": {cand(1, "Hades", 2020, "PC")}}}
	rep, err := Run(context.Background(), d, f, Options{Sleep: noSleep})
	if err != nil || rep.Confident != 0 || rep.Uncertain != 1 || rep.Conflicts != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestReviewQueue(t *testing.T) {
	d, f, ids := seed(t)
	if _, err := Run(context.Background(), d, f, Options{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	q, err := Queue(d)
	if err != nil {
		t.Fatal(err)
	}
	// Prey, Celeste (have candidates, best score first) and Resident Evil 2 (IGDB knew nothing), Obscure Thing (nothing).
	if len(q) != 4 || len(q[0].Candidates) == 0 || len(q[1].Candidates) == 0 || len(q[2].Candidates) != 0 {
		t.Fatalf("queue = %+v", q)
	}
	// Accept Prey's second candidate: linked, the other proposal rejected, leaves the queue.
	if err := Accept(d, ids["Prey"], 11); err != nil {
		t.Fatal(err)
	}
	if igdb, _, synced := linkOf(t, d, ids["Prey"]); igdb.Int64 != 11 || !synced.Valid {
		t.Error("not linked")
	}
	var accepted, rejected int
	d.QueryRow("SELECT count(*) FROM sync_candidate WHERE game_id = ? AND status = 'accepted'", ids["Prey"]).Scan(&accepted)
	d.QueryRow("SELECT count(*) FROM sync_candidate WHERE game_id = ? AND status = 'rejected'", ids["Prey"]).Scan(&rejected)
	if accepted != 1 || rejected != 1 {
		t.Errorf("accepted=%d rejected=%d", accepted, rejected)
	}
	// The same IGDB game cannot go to a second library game.
	err = Accept(d, ids["Celeste"], 11)
	if te, ok := err.(*ErrTaken); !ok || te.GameID != ids["Prey"] {
		t.Errorf("err = %v", err)
	}
	// A pick that was never proposed (chosen with the lookup) works too.
	if err := Accept(d, ids["Obscure Thing"], 999); err != nil {
		t.Fatal(err)
	}
	// "Not on IGDB": rejected proposals, never searched again, gone from the queue.
	if err := NotOnIGDB(d, ids["Celeste"]); err != nil {
		t.Fatal(err)
	}
	q, _ = Queue(d)
	if len(q) != 1 || q[0].Title != "Resident Evil 2" {
		t.Errorf("queue after decisions = %+v", q)
	}
	d.Exec("UPDATE game SET igdb_checked_at = NULL WHERE id = ?", ids["Celeste"]) // even if the check were forgotten
	rep, _ := Run(context.Background(), d, f, Options{Sleep: noSleep})
	if rep.Total != 0 {
		t.Errorf("searched a game marked not on IGDB: %+v", rep)
	}
	if err := Accept(d, 99999, 5); err != ErrNoGame {
		t.Errorf("err = %v", err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		title     string
		platforms []string
		cands     []meta.Candidate
		want      Bucket
	}{
		{"exact with platform", "Hades", []string{"PC"}, []meta.Candidate{cand(1, "Hades", 2020, "PC")}, Confident},
		{"long platform names map", "Hades", []string{"Switch"}, []meta.Candidate{cand(1, "Hades", 2020, "Nintendo Switch")}, Confident},
		{"no platform on game", "Hades", nil, []meta.Candidate{cand(1, "Hades", 2020, "PC")}, Uncertain},
		{"sequel only", "Hades", []string{"PC"}, []meta.Candidate{cand(2, "Hades II", 2024, "PC")}, NotFound},
		{"subtitle", "Dark Souls III", []string{"PC"}, []meta.Candidate{cand(3, "Dark Souls III: The Ringed City", 2017, "PC")}, Uncertain},
		{"nothing", "Zzz", []string{"PC"}, nil, NotFound},
	}
	for _, c := range cases {
		if got := Classify(c.title, c.platforms, c.cands).Bucket; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMigration006(t *testing.T) {
	d := openDB(t)
	var v int
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if v < 6 {
		t.Fatalf("user_version = %d", v)
	}
	addGame(t, d, "Seed")
	for _, bad := range []string{
		`INSERT INTO sync_candidate (game_id, igdb_id, name, score, status) VALUES (1, 1, 'x', 50, 'maybe')`,
		`INSERT INTO sync_candidate (game_id, igdb_id, name, score) VALUES (1, 1, 'x', 150)`,
		`UPDATE game SET igdb_not_found = 2 WHERE id = 1`,
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	id := addGame(t, d, "G")
	d.Exec("INSERT INTO sync_candidate (game_id, igdb_id, name, score) VALUES (?, 5, 'G', 80)", id)
	d.Exec("DELETE FROM game WHERE id = ?", id)
	var n int
	d.QueryRow("SELECT count(*) FROM sync_candidate").Scan(&n)
	if n != 0 {
		t.Error("candidates survive their game")
	}
}
