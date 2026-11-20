package expansions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"database/sql"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

type fakeSource struct {
	mu       sync.Mutex
	lists    map[int64][]meta.Expansion
	ttb      map[int64]meta.TimeToBeat
	err      error
	calls    [][]int64
	ttbCalls []int64 // every id asked for its time to beat
}

func (f *fakeSource) Expansions(ctx context.Context, ids []int64) (map[int64][]meta.Expansion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]int64{}, ids...))
	if f.err != nil {
		return nil, f.err
	}
	out := map[int64][]meta.Expansion{}
	for _, id := range ids {
		out[id] = f.lists[id]
	}
	return out, nil
}

func (f *fakeSource) TimeToBeat(ctx context.Context, ids []int64) (map[int64]meta.TimeToBeat, error) {
	out := map[int64]meta.TimeToBeat{}
	f.mu.Lock()
	f.ttbCalls = append(f.ttbCalls, ids...)
	f.mu.Unlock()
	for _, id := range ids {
		if t, ok := f.ttb[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func open(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1,'Witcher 3','witcher 3',(SELECT id FROM status WHERE code='finished'),1942)`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (2,'Skyrim','skyrim',(SELECT id FROM status WHERE code='backlog'),2)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (3,'No link','no link',(SELECT id FROM status WHERE code='backlog'))`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func count(t *testing.T, d *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(q, err)
	}
	return n
}

var witcher = []meta.Expansion{
	{IGDBID: 100, Name: "Hearts of Stone", Release: 1432598400, Year: 2015, CoverID: "hos", SteamAppID: 378648},
	{IGDBID: 101, Name: "Blood and Wine", Release: 1464134400, Year: 2016, SteamAppID: 378649},
}

func TestFetchStoresAndNeverTouchesTheOwnersData(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}, ttb: map[int64]meta.TimeToBeat{100: {Hastily: 600, Normally: 900}}}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	todo, err := Due(d, now, false)
	if err != nil || len(todo) != 2 || todo[0].IGDBID != 1942 {
		t.Fatalf("only linked games are asked: %v %v", todo, err)
	}
	rep, err := Fetch(context.Background(), d, src, todo, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Games != 2 || rep.Found != 2 || rep.WithExpansion != 1 || rep.Text() != "Found 2 expansions for 1 game." {
		t.Errorf("%+v %q", rep, rep.Text())
	}
	if len(src.calls) != 1 || len(src.calls[0]) != 2 {
		t.Errorf("one request for the batch: %v", src.calls)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE game_id = 1 AND status_id IS NULL AND ownership_id IS NULL`) != 2 {
		t.Error("a new expansion has no status and no ownership")
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND steam_app_id = 378648 AND release_date = '2015-05-26' AND release_year = 2015 AND time_to_beat_normally = 900 AND time_to_beat_src = 'igdb'`) != 1 {
		t.Error("the first expansion lost a value")
	}
	if count(t, d, `SELECT count(*) FROM expansion_fetch`) != 2 {
		t.Error("both games are marked as read, the one without expansions too")
	}
	// within 30 days nobody is due; after that both are
	if due, _ := Due(d, now.Add(29*24*time.Hour), false); len(due) != 0 {
		t.Errorf("fresh: %v", due)
	}
	if due, _ := Due(d, now.Add(31*24*time.Hour), false); len(due) != 2 {
		t.Errorf("stale: %v", due)
	}
	if due, _ := Due(d, now.Add(31*24*time.Hour), true); len(due) != 0 {
		t.Errorf("never-read only: %v", due)
	}
	if !IsFresh(d, 1, now.Add(time.Hour)) || IsFresh(d, 1, now.Add(31*24*time.Hour)) || IsFresh(d, 3, now) {
		t.Error("IsFresh")
	}

	// the owner tracks one expansion
	d.Exec(`INSERT INTO ownership(name) VALUES ('Owned')`)
	if _, err := d.Exec(`UPDATE expansion SET status_id = (SELECT id FROM status WHERE code='finished'), notes = 'great', hidden = 1,
		ownership_id = (SELECT id FROM ownership WHERE name = 'Owned') WHERE igdb_id = 100`); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO expansion_play(expansion_id, year) SELECT id, 2016 FROM expansion WHERE igdb_id = 100`)
	// IGDB renames the first, drops the second, and adds one
	src.lists[1942] = []meta.Expansion{{IGDBID: 100, Name: "Hearts of Stone (renamed)", Release: 1432598400, Year: 2015, SteamAppID: 378648}, {IGDBID: 102, Name: "New one", SteamAppID: 5}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{Now: func() time.Time { return now.Add(40 * 24 * time.Hour) }}); err != nil {
		t.Fatal(err)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND title = 'Hearts of Stone (renamed)' AND notes = 'great' AND hidden = 1 AND status_id IS NOT NULL AND ownership_id IS NOT NULL`) != 1 {
		t.Error("IGDB data refreshes, the owner's data stays")
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 101`) != 0 {
		t.Error("an untouched expansion IGDB dropped goes")
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 102 AND release_date IS NULL AND release_year IS NULL`) != 1 {
		t.Error("a new TBA expansion")
	}
	// a tracked expansion that IGDB drops stays
	src.lists[1942] = nil
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100`) != 1 || count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 102`) != 0 {
		t.Error("tracked stays, untouched goes")
	}
}

func TestFetchTimesNeverOverwriteTheOwners(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}, ttb: map[int64]meta.TimeToBeat{100: {Normally: 900}, 101: {Normally: 1200}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE expansion SET time_to_beat_normally = 5, time_to_beat_src = 'manual' WHERE igdb_id = 100`)
	d.Exec(`UPDATE expansion SET time_to_beat_normally = NULL, time_to_beat_src = NULL WHERE igdb_id = 101`)
	src.ttb[100] = meta.TimeToBeat{Normally: 7777}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if count(t, d, `SELECT time_to_beat_normally FROM expansion WHERE igdb_id = 100`) != 5 {
		t.Error("a time the owner typed was overwritten")
	}
	if count(t, d, `SELECT time_to_beat_normally FROM expansion WHERE igdb_id = 101`) != 1200 {
		t.Error("a missing time is filled")
	}
}

func TestFetchBatchesOf100AndStopsOnError(t *testing.T) {
	d := open(t)
	var targets []Target
	for i := int64(1); i <= 250; i++ {
		targets = append(targets, Target{GameID: 1, IGDBID: i})
	}
	src := &fakeSource{}
	var progress []int
	if _, err := Fetch(context.Background(), d, src, targets, Options{Progress: func(done, total int) { progress = append(progress, done) }}); err != nil {
		t.Fatal(err)
	}
	if len(src.calls) != 3 || len(src.calls[0]) != 100 || len(src.calls[2]) != 50 || progress[len(progress)-1] != 250 {
		t.Errorf("calls %d, progress %v", len(src.calls), progress)
	}
	src.err = errors.New("IGDB down")
	if _, err := Fetch(context.Background(), d, src, targets[:1], Options{}); err == nil {
		t.Error("an IGDB failure is an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, d, &fakeSource{}, targets, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestSameExpansionUnderTwoGamesStaysWithTheFirst(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher[:1], 2: witcher[:1]}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}, {2, 2}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND game_id = 1`) != 1 || count(t, d, `SELECT count(*) FROM expansion`) != 1 {
		t.Error("one row, with the first game")
	}
}

func TestDropUntrackedOnRelink(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE expansion SET notes = 'mine' WHERE igdb_id = 100`)
	tx, _ := d.Begin()
	if err := DropUntracked(tx, 1); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100`) != 1 || count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 101`) != 0 {
		t.Error("only the untouched expansion is replaced")
	}
	if count(t, d, `SELECT count(*) FROM expansion_fetch WHERE game_id = 1`) != 0 {
		t.Error("the game is asked again")
	}
	// every kind of owner data protects a row
	for _, set := range []string{`status_id = (SELECT id FROM status WHERE code = 'playing')`, `time_to_beat_normally = 9, time_to_beat_src = 'hltb'`} {
		d.Exec(`DELETE FROM expansion`)
		d.Exec(`INSERT INTO expansion(game_id, igdb_id, title) VALUES (1, 100, 'x')`)
		d.Exec(`UPDATE expansion SET ` + set)
		tx, _ := d.Begin()
		DropUntracked(tx, 1)
		tx.Commit()
		if count(t, d, `SELECT count(*) FROM expansion`) != 1 {
			t.Errorf("%s is the owner's data", set)
		}
	}
	d.Exec(`DELETE FROM expansion`)
	d.Exec(`INSERT INTO expansion(id, game_id, igdb_id, title) VALUES (5, 1, 100, 'x')`)
	d.Exec(`INSERT INTO expansion_play(expansion_id, period) VALUES (5, 'always')`)
	tx, _ = d.Begin()
	DropUntracked(tx, 1)
	tx.Commit()
	if count(t, d, `SELECT count(*) FROM expansion`) != 1 {
		t.Error("years played are the owner's data")
	}
}

func TestPostersAreDownloadedOnceAndSwept(t *testing.T) {
	d := open(t)
	dir := t.TempDir()
	var urls []string
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}}
	opt := Options{CoverDir: dir, Fetch: func(url, dir string, id int64) (string, error) {
		urls = append(urls, url)
		name := "x" + strings.TrimPrefix(url[strings.LastIndex(url, "/")+1:], "")
		_ = name
		f := filepath.Join(dir, "x100.jpg")
		return "x100.jpg", os.WriteFile(f, []byte("jpg"), 0o644)
	}}
	rep, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, opt)
	if err != nil || rep.Covers != 1 || len(urls) != 1 || !strings.Contains(urls[0], "hos") || !strings.HasPrefix(urls[0], "https://images.igdb.com/") {
		t.Fatalf("%+v %v %v", rep, urls, err)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND cover_file = 'x100.jpg'`) != 1 {
		t.Error("the file name is stored")
	}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, opt); err != nil || len(urls) != 1 {
		t.Errorf("a poster that exists is not downloaded again: %v", urls)
	}
	// a changed IGDB cover drops the file name so the next run downloads it
	src.lists[1942] = []meta.Expansion{{IGDBID: 100, Name: "Hearts of Stone", CoverID: "new"}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, opt); err != nil || len(urls) != 2 || !strings.Contains(urls[1], "new") {
		t.Errorf("%v %v", urls, err)
	}
	// the sweep keeps named and fresh files, removes old unnamed ones
	old := filepath.Join(dir, "x999.jpg")
	os.WriteFile(old, []byte("x"), 0o644)
	os.Chtimes(old, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	fresh := filepath.Join(dir, "x998.jpg")
	os.WriteFile(fresh, []byte("x"), 0o644)
	other := filepath.Join(dir, "i999.jpg") // the Series cache's poster is never ours to remove
	os.WriteFile(other, []byte("x"), 0o644)
	os.Chtimes(other, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	if err := SweepCovers(d, dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"x100.jpg": true, "x999.jpg": false, "x998.jpg": true, "i999.jpg": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s present=%v want %v", name, err == nil, want)
		}
	}
}

func TestParseOwned(t *testing.T) {
	ok := `{"rgWishlist":[1,2],"rgOwnedApps":[10, 20, 10, 378648],"rgOwnedPackages":[5],"strCountry":"ES"}`
	got, err := ParseOwned(strings.NewReader(ok))
	if err != nil || len(got) != 3 || got[0] != 10 || got[2] != 378648 {
		t.Errorf("%v %v", got, err)
	}
	for name, body := range map[string]string{
		"not json":      `<html>`,
		"no list":       `{"rgWishlist":[1]}`,
		"null list":     `{"rgOwnedApps":null}`,
		"object":        `{"rgOwnedApps":{"a":1}}`,
		"strings":       `{"rgOwnedApps":["10"]}`,
		"fractions":     `{"rgOwnedApps":[1.5]}`,
		"zero or below": `{"rgOwnedApps":[0]}`,
		"empty":         ``,
		"array":         `[1,2]`,
	} {
		if _, err := ParseOwned(strings.NewReader(body)); !errors.Is(err, ErrNotSteamFile) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// the page saved by a browser as "Webpage, HTML only": the JSON inside <pre>, maybe with entities
	for _, page := range []string{
		`<html><head><meta name="color-scheme" content="light dark"></head><body><pre style="word-wrap: break-word;">` + ok + `</pre></body></html>`,
		`<html><body><pre>{&quot;rgOwnedApps&quot;:[10,20,378648],&quot;strCountry&quot;:&quot;ES&quot;}</pre></body></html>`,
	} {
		if got, err := ParseOwned(strings.NewReader(page)); err != nil || len(got) != 3 || got[2] != 378648 {
			t.Errorf("saved web page: %v %v", got, err)
		}
	}
	// a saved page whose head has braces of its own (an inline style or script) before the JSON
	styled := `<html><head><style>pre{white-space:pre-wrap}</style><script>var a = {b: 1};</script></head><body><pre>` + ok + `</pre><div class="json-formatter-container"></div></body></html>`
	if got, err := ParseOwned(strings.NewReader(styled)); err != nil || len(got) != 3 || got[0] != 10 {
		t.Errorf("saved web page with a style: %v %v", got, err)
	}
	for name, page := range map[string]string{
		"two lists":   `<pre>{"rgOwnedApps":[1]}</pre><pre>{"rgOwnedApps":[2]}</pre>`,
		"not a list":  `<pre>{"rgOwnedApps":"1,2"}</pre>`,
		"no colon":    `<p>"rgOwnedApps" [1,2]</p>`,
		"broken list": `<pre>{"rgOwnedApps":[1,2</pre>`,
	} {
		if _, err := ParseOwned(strings.NewReader(page)); !errors.Is(err, ErrNotSteamFile) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseOwned(strings.NewReader(`<html><pre>{"rgWishlist":[1]}</pre></html>`)); !errors.Is(err, ErrNotSteamFile) {
		t.Errorf("a web page without the list: %v", err)
	}
	// what Steam answers to a browser that is not logged in: a Steam file, but nothing to mark
	for _, empty := range []string{`{"rgWishlist":[],"rgOwnedPackages":[],"rgOwnedApps":[]}`, `<pre>{&quot;rgOwnedApps&quot;:[]}</pre>`} {
		if got, err := ParseOwned(strings.NewReader(empty)); !errors.Is(err, ErrNoOwnedApps) {
			t.Errorf("an empty list (logged out): %v %v", got, err)
		}
	}
	big := `{"x":"` + strings.Repeat("a", MaxSteamFile) + `","rgOwnedApps":[1]}`
	if _, err := ParseOwned(strings.NewReader(big)); !errors.Is(err, ErrTooBig) {
		t.Errorf("over the cap: %v", err)
	}
}

func TestImportOwned(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher, 2: {{IGDBID: 200, Name: "Dawnguard", SteamAppID: 211720}, {IGDBID: 201, Name: "No steam page"}}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}, {2, 2}}, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO ownership(name) VALUES ('Borrowed')`)
	// the owner already set one ownership, and a status on another
	d.Exec(`UPDATE expansion SET ownership_id = (SELECT id FROM ownership WHERE name = 'Borrowed') WHERE igdb_id = 101`)
	d.Exec(`UPDATE expansion SET status_id = (SELECT id FROM status WHERE code = 'dropped') WHERE igdb_id = 100`)
	d.Exec(`INSERT INTO game_link(game_id, kind, url) VALUES (1, 'steam', 'https://store.steampowered.com/app/292030/The_Witcher_3/')`)
	res, err := ImportOwned(d, []int64{378648, 378649, 211720, 292030, 999, 998})
	if err != nil {
		t.Fatal(err)
	}
	// 100 and 200 are marked; 101 already had an ownership
	if res.Marked != 2 || res.Already != 1 || res.Matched != 3 || res.Unmatched != 2 {
		t.Errorf("%+v", res)
	}
	if res.Text() != "Marked 2 expansions as owned (1 already had an ownership); 2 other owned Steam items are not expansions of games in your library (or not on IGDB)." {
		t.Error(res.Text())
	}
	own := func(igdb int64) string {
		var n sql.NullString
		d.QueryRow(`SELECT o.name FROM expansion e LEFT JOIN ownership o ON o.id = e.ownership_id WHERE e.igdb_id = ?`, igdb).Scan(&n)
		return n.String
	}
	if own(100) != "Owned" || own(200) != "Owned" || own(101) != "Borrowed" || own(201) != "" {
		t.Errorf("ownership %q %q %q %q", own(100), own(200), own(101), own(201))
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND status_id = (SELECT id FROM status WHERE code = 'dropped')`) != 1 {
		t.Error("a status is never changed")
	}
	// again: nothing more to do
	res, err = ImportOwned(d, []int64{378648, 378649, 211720, 999})
	if err != nil || res.Marked != 0 || res.Already != 3 || res.Matched != 3 {
		t.Errorf("idempotent: %+v %v", res, err)
	}
	// a second run says it found them, and one other item is singular
	if res.Text() != "Marked 0 expansions as owned (3 already had an ownership); 1 other owned Steam item is not an expansion of a game in your library (or not on IGDB)." {
		t.Error(res.Text())
	}
	if res, err := ImportOwned(d, nil); err != nil || res.Marked != 0 {
		t.Errorf("empty: %+v %v", res, err)
	}
}

// A placeholder date of IGDB's (year 1, 2999) is stored as no date: the CHECK on release_year must never fail a batch.
func TestFetchDropsImpossibleDates(t *testing.T) {
	d := open(t)
	y1 := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	y2999 := time.Date(2999, 12, 31, 0, 0, 0, 0, time.UTC).Unix()
	src := &fakeSource{lists: map[int64][]meta.Expansion{
		1942: {{IGDBID: 100, Name: "Old placeholder", Release: y1, Year: 1}, {IGDBID: 101, Name: "Far placeholder", Release: y2999, Year: 2999}},
		2:    witcher[1:],
	}}
	rep, err := Fetch(context.Background(), d, src, []Target{{1, 1942}, {2, 2}}, Options{})
	if err != nil || rep.Found != 2 { // 101 is listed under both games: counted once
		t.Fatalf("%+v %v", rep, err)
	}
	if n := count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id IN (100, 101) AND release_date IS NULL AND release_year IS NULL`); n != 2 {
		t.Errorf("placeholder dates stored as none: %d", n)
	}
	if n := count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 101 AND game_id = 2`); n != 0 {
		t.Error("the same IGDB id stays with the first game")
	}
}

// A one-game fetch (the drawer's refresh) downloads only that game's posters; the background job (AllCovers) catches up on all.
func TestCoversOfOneGameOnly(t *testing.T) {
	d := open(t)
	if _, err := d.Exec(`INSERT INTO expansion(game_id, igdb_id, title, cover_image_id) VALUES (2, 500, 'Dragonborn', 'db')`); err != nil {
		t.Fatal(err)
	}
	var got []int64
	opt := Options{CoverDir: t.TempDir(), Fetch: func(url, dir string, id int64) (string, error) { got = append(got, id); return "x.jpg", nil }}
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, opt); err != nil || len(got) != 1 || got[0] != 100 {
		t.Fatalf("one game's posters only: %v %v", got, err)
	}
	opt.AllCovers = true
	if _, err := Fetch(context.Background(), d, src, nil, opt); err != nil || len(got) != 2 || got[1] != 500 {
		t.Errorf("the job gets the rest: %v %v", got, err)
	}
}

// An expansion the library also has as its own game (a card, e.g. from NocoDB) is not kept inside its base game: it would be
// counted twice. An untracked row of it goes; a tracked one stays in the database (the API leaves it out). The Steam import
// counts it as matched but never marks it.
func TestExpansionThatIsALibraryGame(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: witcher}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	// the owner tracks Hearts of Stone, then adds both expansions as games of their own
	d.Exec(`UPDATE expansion SET notes = 'mine' WHERE igdb_id = 100`)
	for _, q := range []string{
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (10,'Hearts of Stone','hearts of stone',(SELECT id FROM status WHERE code='finished'),100)`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (11,'Blood and Wine','blood and wine',(SELECT id FROM status WHERE code='finished'),101)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Found != 0 || rep.WithExpansion != 0 {
		t.Errorf("library games are not found as expansions: %+v", rep)
	}
	if count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 101`) != 0 || count(t, d, `SELECT count(*) FROM expansion WHERE igdb_id = 100 AND notes = 'mine'`) != 1 {
		t.Error("the untracked row goes, the tracked one stays")
	}
	// a new game whose expansion is already a library game: never inserted
	d.Exec(`DELETE FROM expansion`)
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM expansion`); n != 0 {
		t.Errorf("%d rows", n)
	}
	// the Steam import: a tracked row of a library game is matched, not marked
	d.Exec(`INSERT INTO expansion(game_id, igdb_id, title, steam_app_id) VALUES (1, 100, 'Hearts of Stone', 378648)`)
	res, err := ImportOwned(d, []int64{378648, 999})
	if err != nil || res.Marked != 0 || res.Matched != 1 || res.Unmatched != 1 {
		t.Errorf("%+v %v", res, err)
	}
}

// IGDB's DLC are stored with kind 'dlc'. The Steam import never gives a DLC an ownership (Steam lists cosmetic packs as DLC too):
// it only marks it as owned on Steam, and says so; an owned expansion is still marked as owned.
func TestDLCKindAndSteamImport(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{72: {
		{IGDBID: 6069, Name: "Dragonborn", SteamAppID: 211722, Kind: meta.KindExpansion},
		{IGDBID: 2992, Name: "Dawnguard", SteamAppID: 211720, Kind: meta.KindDLC},
		{IGDBID: 6068, Name: "Hearthfire", SteamAppID: 211721, Kind: meta.KindDLC},
		{IGDBID: 7000, Name: "No kind given", Kind: ""},
	}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 72}}, Options{}); err != nil {
		t.Fatal(err)
	}
	kind := func(igdb int64) (k string, steam bool, owned bool) {
		d.QueryRow(`SELECT kind, steam_owned, ownership_id IS NOT NULL FROM expansion WHERE igdb_id = ?`, igdb).Scan(&k, &steam, &owned)
		return
	}
	if k, _, _ := kind(6069); k != "expansion" {
		t.Errorf("Dragonborn kind %q", k)
	}
	if k, _, _ := kind(2992); k != "dlc" {
		t.Errorf("Dawnguard kind %q", k)
	}
	if k, _, _ := kind(7000); k != "expansion" {
		t.Errorf("no kind is an expansion: %q", k)
	}
	res, err := ImportOwned(d, []int64{211720, 211722, 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Marked != 1 || res.DLC != 1 || res.Matched != 2 || res.Unmatched != 1 {
		t.Errorf("%+v", res)
	}
	if _, steam, owned := kind(2992); !steam || owned {
		t.Errorf("Dawnguard: steam_owned %v, ownership set %v (want true, false)", steam, owned)
	}
	if _, steam, owned := kind(6068); steam || owned {
		t.Error("Hearthfire is not owned on Steam")
	}
	if _, _, owned := kind(6069); !owned {
		t.Error("Dragonborn (an expansion) is marked as owned")
	}
	want := "Marked 1 expansion as owned; 1 DLC you own on Steam is offered first in its game's details; 1 other owned Steam item is not an expansion of a game in your library (or not on IGDB)."
	if res.Text() != want {
		t.Errorf("%q", res.Text())
	}
	// a refetch keeps the mark (only IGDB's own fields are refreshed)
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 72}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, steam, _ := kind(2992); !steam {
		t.Error("a refetch dropped the Steam mark")
	}
}

// A DLC the owner added is marked Owned by the Steam import like an expansion, one with an ownership is left as it is ("already"),
// and only untouched DLC are counted as offers. The Steam mark follows the uploaded list: a refunded DLC loses it.
func TestSteamImportAddedDLCAndRefund(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{72: {
		{IGDBID: 2992, Name: "Dawnguard", SteamAppID: 211720, Kind: meta.KindDLC},
		{IGDBID: 6068, Name: "Hearthfire", SteamAppID: 211721, Kind: meta.KindDLC},
		{IGDBID: 8000, Name: "Costume", SteamAppID: 211730, Kind: meta.KindDLC},
	}}}
	rep, err := Fetch(context.Background(), d, src, []Target{{1, 72}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.DLC != 3 || rep.Text() != "Found 3 DLC for 1 game." {
		t.Errorf("%+v %q", rep, rep.Text())
	}
	d.Exec(`UPDATE expansion SET status_id = (SELECT id FROM status WHERE code = 'backlog') WHERE igdb_id = 2992`)
	if _, err := d.Exec(`INSERT INTO ownership(name) SELECT 'Nope' WHERE NOT EXISTS (SELECT 1 FROM ownership WHERE name = 'Nope')`); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE expansion SET ownership_id = (SELECT id FROM ownership WHERE name = 'Nope') WHERE igdb_id = 6068`)
	res, err := ImportOwned(d, []int64{211720, 211721, 211730})
	if err != nil {
		t.Fatal(err)
	}
	if res.Marked != 1 || res.Already != 1 || res.DLC != 1 || res.Matched != 3 {
		t.Errorf("%+v", res)
	}
	var own sql.NullString
	var steam bool
	d.QueryRow(`SELECT o.name, e.steam_owned FROM expansion e LEFT JOIN ownership o ON o.id = e.ownership_id WHERE e.igdb_id = 2992`).Scan(&own, &steam)
	if own.String != OwnedValue || !steam {
		t.Errorf("Dawnguard (added): ownership %v, steam %v", own, steam)
	}
	d.QueryRow(`SELECT o.name FROM expansion e LEFT JOIN ownership o ON o.id = e.ownership_id WHERE e.igdb_id = 6068`).Scan(&own)
	if own.String != "Nope" {
		t.Errorf("Hearthfire: the owner's Nope was changed to %v", own)
	}
	// Costume refunded: a new upload without it clears its mark
	if _, err := ImportOwned(d, []int64{211720}); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT steam_owned FROM expansion WHERE igdb_id = 8000`).Scan(&steam)
	if steam {
		t.Error("a DLC no longer in the owned list keeps its Steam mark")
	}
}

// The background run (AllCovers) skips the posters of DLC nobody added (unless owned on Steam); a one-game read gets them all.
func TestBackgroundCoversSkipUntouchedDLC(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: {
		{IGDBID: 10, Name: "Pack", CoverID: "a", Kind: meta.KindDLC},
		{IGDBID: 11, Name: "Owned pack", CoverID: "b", Kind: meta.KindDLC},
		{IGDBID: 12, Name: "Added pack", CoverID: "c", Kind: meta.KindDLC},
		{IGDBID: 13, Name: "Expansion", CoverID: "d", Kind: meta.KindExpansion},
	}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE expansion SET steam_owned = 1 WHERE igdb_id = 11`)
	d.Exec(`UPDATE expansion SET notes = 'want' WHERE igdb_id = 12`)
	var got []int64
	opt := Options{CoverDir: t.TempDir(), AllCovers: true, Fetch: func(url, dir string, id int64) (string, error) { got = append(got, id); return "x.jpg", nil }}
	if _, err := Fetch(context.Background(), d, src, nil, opt); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 12 {
		t.Errorf("background posters %v (want the added one first, no untouched pack)", got)
	}
	got = nil
	opt.AllCovers = false
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, opt); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 10 {
		t.Errorf("one-game read posters %v (want the untouched pack)", got)
	}
}

func TestBackgroundTimesSkipUntouchedDLC(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{1942: {
		{IGDBID: 10, Name: "Pack", Kind: meta.KindDLC},
		{IGDBID: 11, Name: "Owned pack", Kind: meta.KindDLC},
		{IGDBID: 12, Name: "Added pack", Kind: meta.KindDLC},
		{IGDBID: 13, Name: "Expansion", Kind: meta.KindExpansion},
	}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(src.ttbCalls) != 4 {
		t.Errorf("a one-game read asks for all: %v", src.ttbCalls)
	}
	d.Exec(`UPDATE expansion SET steam_owned = 1 WHERE igdb_id = 11`)
	d.Exec(`UPDATE expansion SET notes = 'want' WHERE igdb_id = 12`)
	d.Exec(`DELETE FROM expansion_fetch`)
	src.ttbCalls = nil
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{AllCovers: true}); err != nil {
		t.Fatal(err)
	}
	asked := map[int64]bool{}
	for _, id := range src.ttbCalls {
		asked[id] = true
	}
	if asked[10] || !asked[11] || !asked[12] || !asked[13] {
		t.Errorf("background times asked for %v (want all but the untouched pack 10)", src.ttbCalls)
	}
	src.ttbCalls = nil
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 1942}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(src.ttbCalls) != 4 {
		t.Errorf("a one-game read asks for all: %v", src.ttbCalls)
	}
}

// An owned DLC the owner hid keeps its Steam mark but is not counted as an offer, and never gets an ownership.
func TestSteamImportHiddenDLCNotCounted(t *testing.T) {
	d := open(t)
	src := &fakeSource{lists: map[int64][]meta.Expansion{72: {
		{IGDBID: 2992, Name: "Dawnguard", SteamAppID: 211720, Kind: meta.KindDLC},
		{IGDBID: 8000, Name: "Costume", SteamAppID: 211730, Kind: meta.KindDLC},
	}}}
	if _, err := Fetch(context.Background(), d, src, []Target{{1, 72}}, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE expansion SET hidden = 1 WHERE igdb_id = 8000`)
	res, err := ImportOwned(d, []int64{211720, 211730})
	if err != nil {
		t.Fatal(err)
	}
	if res.DLC != 1 || res.Marked != 0 || res.Matched != 2 {
		t.Errorf("%+v", res)
	}
	var steam bool
	var own sql.NullInt64
	d.QueryRow(`SELECT steam_owned, ownership_id FROM expansion WHERE igdb_id = 8000`).Scan(&steam, &own)
	if !steam || own.Valid {
		t.Errorf("hidden DLC: steam_owned %v, ownership %v (want marked, no ownership)", steam, own)
	}
}
