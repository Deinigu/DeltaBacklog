package igdblink

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// fakeEnricher answers from canned details; it logs every request and can answer 429 or fail.
type fakeEnricher struct {
	details  map[int64]meta.Details
	ttb      map[int64]meta.TimeToBeat
	batches  [][]int64 // ids of each GameDetails call
	calls    int       // GameDetails + TimeToBeat requests
	throttle int       // answer the next N requests with 429
	fail     bool      // answer every request with 500
}

func (f *fakeEnricher) answer() error {
	f.calls++
	if f.throttle > 0 {
		f.throttle--
		return &meta.HTTPError{Method: "POST", Host: "igdb", Status: 429, Text: "429"}
	}
	if f.fail {
		return &meta.HTTPError{Method: "POST", Host: "igdb", Status: 500, Text: "500"}
	}
	return nil
}

func (f *fakeEnricher) GameDetails(_ context.Context, ids []int64) ([]meta.Details, error) {
	if err := f.answer(); err != nil {
		return nil, err
	}
	f.batches = append(f.batches, ids)
	var out []meta.Details
	for _, id := range ids {
		if d, ok := f.details[id]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeEnricher) TimeToBeat(_ context.Context, ids []int64) (map[int64]meta.TimeToBeat, error) {
	if err := f.answer(); err != nil {
		return nil, err
	}
	out := map[int64]meta.TimeToBeat{}
	for _, id := range ids {
		if t, ok := f.ttb[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func link(t *testing.T, d *sql.DB, id, igdbID int64) {
	t.Helper()
	if _, err := d.Exec("UPDATE game SET igdb_id = ? WHERE id = ?", igdbID, id); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, d *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := d.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

func hades() meta.Details {
	return meta.Details{
		IGDBID: 1, Year: 2020, Platforms: []string{"PC"}, Genres: []string{"Roguelike", "Indie"}, Themes: []string{"Action", "4X (explore, expand, exploit, and exterminate)"},
		Developer: "Supergiant Games", Series: "Hades", Modes: []string{"Single player"}, Perspectives: []string{"Isometric"},
		Links: []meta.StoreLink{{Kind: "steam", URL: "https://store.steampowered.com/app/1145360"}, {Kind: "official", URL: "https://www.supergiantgames.com/"}},
	}
}

func TestEnrichFillsEmptyFieldsAndStoresNewData(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades") // no platform
	link(t, d, id, 1)
	db.GetOrCreate(d, "platform", "PC") // the library has PC; enrichment never creates a platform
	// a second game of the same IGDB collection: two library games sharing it make it a series
	id2 := addGame(t, d, "Hades II")
	link(t, d, id2, 2)
	second := hades()
	second.IGDBID, second.Year = 2, 2024
	f := &fakeEnricher{
		details: map[int64]meta.Details{1: hades(), 2: second},
		ttb:     map[int64]meta.TimeToBeat{1: {Hastily: 36000, Normally: 72000, Completely: 150001}},
	}
	rep, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 2 || rep.Enriched != 2 || rep.TimeToBeat != 1 || rep.FilledYear != 2 || rep.FilledGenres != 2 || rep.FilledThemes != 2 ||
		rep.FilledDeveloper != 2 || rep.FilledSeries != 2 || rep.FilledPlatforms != 2 || rep.Modes != 2 || rep.Perspectives != 2 || rep.Links != 2 {
		t.Errorf("report = %+v", rep)
	}
	var year, h, n, c int
	var dev, series string
	d.QueryRow(`SELECT release_year, time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely,
		(SELECT name FROM company WHERE id = developer_id), (SELECT name FROM series WHERE id = series_id) FROM game WHERE id = ?`, id).Scan(&year, &h, &n, &c, &dev, &series)
	if year != 2020 || h != 600 || n != 1200 || c != 2500 || dev != "Supergiant Games" || series != "Hades" {
		t.Errorf("game = %d %d %d %d %q %q", year, h, n, c, dev, series)
	}
	if g := names(t, d, "SELECT p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id WHERE x.game_id = ? ORDER BY 1", id); g != "Indie,Roguelike" {
		t.Errorf("genres = %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_theme x JOIN theme p ON p.id = x.theme_id WHERE x.game_id = ? ORDER BY 1", id); g != "4X,Action" {
		t.Errorf("themes (verbose IGDB names are folded) = %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = ?", id); g != "PC" {
		t.Errorf("platforms = %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_game_mode x JOIN game_mode p ON p.id = x.game_mode_id WHERE x.game_id = ?", id); g != "Single player" {
		t.Errorf("modes = %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_perspective x JOIN perspective p ON p.id = x.perspective_id WHERE x.game_id = ?", id); g != "Isometric" {
		t.Errorf("perspectives = %s", g)
	}
	if g := names(t, d, "SELECT kind || '=' || url FROM game_link WHERE game_id = ? ORDER BY kind", id); !strings.Contains(g, "official=https://www.supergiantgames.com/,steam=https://store.steampowered.com/app/1145360") {
		t.Errorf("links = %s", g)
	}
	var enriched sql.NullString
	d.QueryRow("SELECT igdb_enriched_at FROM game WHERE id = ?", id).Scan(&enriched)
	if !enriched.Valid {
		t.Error("igdb_enriched_at not set")
	}
	if g := names(t, d, "SELECT rowid FROM game_fts WHERE game_fts MATCH 'series:Hades' ORDER BY rowid"); g != "1,2" {
		t.Error("search index lost the game")
	}
}

func TestEnrichNeverOverwritesWhatTheOwnerEntered(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades", "Switch")
	link(t, d, id, 1)
	sid, _ := db.GetOrCreate(d, "series", "My Own Series")
	cid, _ := db.GetOrCreate(d, "company", "My Studio")
	gid, _ := db.GetOrCreate(d, "genre", "Mine")
	tid, _ := db.GetOrCreate(d, "theme", "Cozy")
	d.Exec("UPDATE game SET release_year = 1999, series_id = ?, developer_id = ? WHERE id = ?", sid, cid, id)
	d.Exec("INSERT INTO game_genre (game_id, genre_id) VALUES (?, ?)", id, gid)
	d.Exec("INSERT INTO game_theme (game_id, theme_id) VALUES (?, ?)", id, tid)
	var updated string
	d.QueryRow("SELECT updated_at FROM game WHERE id = ?", id).Scan(&updated)
	d.Exec("UPDATE game SET updated_at = '2020-01-01T00:00:00Z' WHERE id = ?", id)

	counts := func() string {
		return names(t, d, "SELECT (SELECT count(*) FROM series) || '/' || (SELECT count(*) FROM company) || '/' || (SELECT count(*) FROM genre) || '/' || (SELECT count(*) FROM theme) || '/' || (SELECT count(*) FROM platform)")
	}
	rowsBefore := counts()
	rep, err := Enrich(context.Background(), d, &fakeEnricher{details: map[int64]meta.Details{1: hades()}}, EnrichOptions{Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if after := counts(); after != rowsBefore {
		t.Errorf("lookup rows (series/company/genre/theme/platform) %s became %s: IGDB's spellings must not be created when nothing is filled", rowsBefore, after)
	}
	if rep.FilledYear+rep.FilledGenres+rep.FilledThemes+rep.FilledDeveloper+rep.FilledSeries+rep.FilledPlatforms != 0 {
		t.Errorf("filled something that was not empty: %+v", rep)
	}
	var year int
	var dev, series, upd string
	d.QueryRow(`SELECT release_year, (SELECT name FROM company WHERE id = developer_id), (SELECT name FROM series WHERE id = series_id), updated_at FROM game WHERE id = ?`, id).Scan(&year, &dev, &series, &upd)
	if year != 1999 || dev != "My Studio" || series != "My Own Series" {
		t.Errorf("owner values changed: %d %s %s", year, dev, series)
	}
	if upd != "2020-01-01T00:00:00Z" {
		t.Errorf("updated_at was touched: %s (the Stats page reads it)", upd)
	}
	if g := names(t, d, "SELECT name FROM genre WHERE id IN (SELECT genre_id FROM game_genre WHERE game_id = ?)", id); g != "Mine" {
		t.Errorf("genres = %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = ?", id); g != "Switch" {
		t.Errorf("platforms = %s", g)
	}
	// ...but the data that is new in the app is still stored
	if rep.Modes != 1 || rep.Links != 1 {
		t.Errorf("report = %+v", rep)
	}
}

func TestEnrichDoesNotGuessAPlatform(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.Platforms = []string{"PC", "Switch", "PS5"}
	rep, _ := Enrich(context.Background(), d, &fakeEnricher{details: map[int64]meta.Details{1: det}}, EnrichOptions{Sleep: noSleep})
	if rep.FilledPlatforms != 0 || rep.PlatformsSkip != 1 {
		t.Errorf("report = %+v", rep)
	}
	if g := names(t, d, "SELECT count(*) FROM game_platform"); g != "0" {
		t.Errorf("platforms were guessed: %s", g)
	}
}

func TestEnrichSkipsUnlinkedAndDoneGamesAndBatches(t *testing.T) {
	d := openDB(t)
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	addGame(t, d, "Unlinked")
	for i := int64(1); i <= 45; i++ {
		link(t, d, addGame(t, d, "G"+string(rune('A'+i%26))+string(rune('a'+i/26))), i)
		f.details[i] = meta.Details{IGDBID: i, Year: 2000}
	}
	rep, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 45 || len(f.batches) != 3 || len(f.batches[0]) != 20 || len(f.batches[2]) != 5 || f.calls != 6 {
		t.Errorf("total %d, batches %v, calls %d", rep.Total, len(f.batches), f.calls)
	}
	// a second run has nothing to do (resumable) ...
	f.calls = 0
	rep, _ = Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if rep.Total != 0 || f.calls != 0 {
		t.Errorf("second run: %+v, %d calls", rep, f.calls)
	}
	// ... unless refresh is asked for
	rep, _ = Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true})
	if rep.Total != 45 {
		t.Errorf("refresh total = %d", rep.Total)
	}
}

func TestEnrichResumesAfterBeingThrottled(t *testing.T) {
	d := openDB(t)
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	for i := int64(1); i <= 30; i++ {
		link(t, d, addGame(t, d, "Game "+string(rune('A'+i))), i)
		f.details[i] = meta.Details{IGDBID: i, Year: 2001}
	}
	// the first batch works (2 requests); then IGDB refuses for good
	var calls int
	o := EnrichOptions{Sleep: noSleep, MaxRetries: 2}
	flaky := &throttleEnrich{inner: f, okCalls: 2, calls: &calls}
	rep, err := Enrich(context.Background(), d, flaky, o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Stopped == "" || rep.Enriched != 20 {
		t.Fatalf("report = %+v", rep)
	}
	var left int
	d.QueryRow("SELECT count(*) FROM game WHERE igdb_enriched_at IS NULL").Scan(&left)
	if left != 10 {
		t.Errorf("%d games left, want 10", left)
	}
	// next run finishes the rest, without reading the first batch again
	f.batches = nil
	rep, _ = Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if rep.Total != 10 || rep.Enriched != 10 || rep.Stopped != "" || len(f.batches) != 1 {
		t.Errorf("resume: %+v batches %d", rep, len(f.batches))
	}
}

type throttleEnrich struct {
	inner   *fakeEnricher
	okCalls int
	calls   *int
}

func (t *throttleEnrich) gate() error {
	*t.calls++
	if *t.calls > t.okCalls {
		return &meta.HTTPError{Method: "POST", Host: "igdb", Status: 429, Text: "429"}
	}
	return nil
}
func (t *throttleEnrich) GameDetails(ctx context.Context, ids []int64) ([]meta.Details, error) {
	if err := t.gate(); err != nil {
		return nil, err
	}
	return t.inner.GameDetails(ctx, ids)
}
func (t *throttleEnrich) TimeToBeat(ctx context.Context, ids []int64) (map[int64]meta.TimeToBeat, error) {
	if err := t.gate(); err != nil {
		return nil, err
	}
	return t.inner.TimeToBeat(ctx, ids)
}

func TestEnrichBacksOffOn429(t *testing.T) {
	d := openDB(t)
	link(t, d, addGame(t, d, "Hades"), 1)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, throttle: 3}
	var slept []time.Duration
	o := EnrichOptions{Sleep: func(_ context.Context, w time.Duration) error { slept = append(slept, w); return nil }}
	rep, err := Enrich(context.Background(), d, f, o)
	if err != nil || rep.Stopped != "" || rep.Enriched != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	// three throttled answers: waits of 2s, 4s, 8s (doubling); the 250ms pause comes between the two requests of a batch
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 250 * time.Millisecond}
	if len(slept) != len(want) {
		t.Fatalf("slept %v, want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Errorf("slept %v, want %v", slept, want)
			break
		}
	}
}

func TestEnrichFailedRequestsAreRetriedNextRun(t *testing.T) {
	d := openDB(t)
	link(t, d, addGame(t, d, "Hades"), 1)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, fail: true}
	rep, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if err != nil || rep.Errors != 1 || rep.Enriched != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	f.fail = false
	rep, _ = Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if rep.Enriched != 1 {
		t.Errorf("not retried: %+v", rep)
	}
}

func TestEnrichGameGoneFromIGDBIsMarkedDone(t *testing.T) {
	d := openDB(t)
	link(t, d, addGame(t, d, "Gone"), 99)
	rep, _ := Enrich(context.Background(), d, &fakeEnricher{}, EnrichOptions{Sleep: noSleep})
	if rep.Missing != 1 || rep.Enriched != 0 {
		t.Errorf("%+v", rep)
	}
	if rep, _ = Enrich(context.Background(), d, &fakeEnricher{}, EnrichOptions{Sleep: noSleep}); rep.Total != 0 {
		t.Error("asked again for a game IGDB no longer has")
	}
}

func TestEnrichRefreshKeepsOldTimeToBeatWhenIGDBHasNone(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 3600}}}
	Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	f.ttb = map[int64]meta.TimeToBeat{}
	Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true})
	var n sql.NullInt64
	d.QueryRow("SELECT time_to_beat_normally FROM game WHERE id = ?", id).Scan(&n)
	if n.Int64 != 60 {
		t.Errorf("time to beat = %v", n)
	}
}

func TestEnrichDryRunWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	link(t, d, addGame(t, d, "Hades"), 1)
	d.Exec("PRAGMA journal_mode = DELETE")
	d.Close()
	before, _ := os.ReadFile(path)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 7200}}}
	var out bytes.Buffer
	if code := run([]string{"--enrich", "--dry-run", "--db", path}, "", &enrichSearch{f}, true, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("the dry run changed the database file")
	}
	for _, want := range []string{"DRY RUN", "Linked games to read: 1", "time to beat:        1 games", "release year 1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	if m, _ := filepath.Glob(path + "*"); len(m) != 1 {
		t.Errorf("stray files: %v", m)
	}
}

// enrichSearch adapts the fake to the Searcher the command also expects.
type enrichSearch struct{ *fakeEnricher }

func (e *enrichSearch) SearchIGDB(context.Context, string) ([]meta.Candidate, error) {
	return nil, nil
}

func TestEnrichCommandBacksUpFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	d, _ := db.Open(path)
	link(t, d, addGame(t, d, "Hades"), 1)
	d.Close()
	f := &enrichSearch{&fakeEnricher{details: map[int64]meta.Details{1: hades()}}}
	var out bytes.Buffer
	if code := run([]string{"--enrich", "--db", path}, "", f, true, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	m, _ := filepath.Glob(filepath.Join(dir, "app-before-igdb-enrich-*.db"))
	if len(m) != 1 {
		t.Fatalf("backups: %v\n%s", m, out.String())
	}
	if code := run([]string{"--refresh", "--db", path}, "", f, true, &out); code != 2 {
		t.Errorf("--refresh alone exit = %d", code)
	}
}

func TestMigration007OnAPopulatedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id := addGame(t, d, "Hades", "PC")
	d.Exec("UPDATE game SET release_year = 2020, igdb_id = 5, igdb_synced_at = '2026-01-01T00:00:00Z' WHERE id = ?", id)
	for _, q := range []string{ // put the file back to what version 6 looked like
		"DROP TABLE deal_price", "DROP TABLE deal_match", "DROP TABLE setting", "DROP TABLE similar_hidden", "DROP TABLE similar_link", "DROP TABLE similar_fetch", "DROP TABLE similar_game", "DROP INDEX idx_icg_release_date", "DROP INDEX idx_game_release_date", "ALTER TABLE igdb_collection_game DROP COLUMN first_release_date", "ALTER TABLE igdb_collection_game DROP COLUMN release_date_format", "ALTER TABLE igdb_collection_game DROP COLUMN platforms", "ALTER TABLE game DROP COLUMN first_release_date", "ALTER TABLE game DROP COLUMN release_date_format", "DELETE FROM status WHERE code = 'wishlist'", "DROP TABLE series_hidden", "DROP TABLE igdb_collection_game", "DROP TABLE igdb_collection", "DROP TABLE game_collection", "ALTER TABLE game DROP COLUMN igdb_parent_id", "DROP TABLE ranking_entry", "DROP TABLE ranking_tier", "DROP TABLE ranking", "ALTER TABLE game DROP COLUMN poster_user", "ALTER TABLE game DROP COLUMN poster_file", "ALTER TABLE game DROP COLUMN poster_src", "ALTER TABLE game DROP COLUMN poster_updated_at", "DROP TABLE screenshot", "ALTER TABLE game DROP COLUMN igdb_series", "DROP TABLE game_link", "DROP TABLE game_game_mode", "DROP TABLE game_perspective", "DROP TABLE game_mode", "DROP TABLE perspective",
		"ALTER TABLE game DROP COLUMN time_to_beat_src", "ALTER TABLE game DROP COLUMN time_to_beat_hastily", "ALTER TABLE game DROP COLUMN time_to_beat_normally",
		"ALTER TABLE game DROP COLUMN time_to_beat_completely", "ALTER TABLE game DROP COLUMN igdb_enriched_at", "PRAGMA user_version = 6",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	d.Close()
	d, err = db.Open(path) // migrates 6 -> latest
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v, year int
	var enriched sql.NullString
	var ttb sql.NullInt64
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if err := d.QueryRow("SELECT release_year, igdb_enriched_at, time_to_beat_normally FROM game WHERE id = ?", id).Scan(&year, &enriched, &ttb); err != nil {
		t.Fatal(err)
	}
	if v != 16 || year != 2020 || enriched.Valid || ttb.Valid {
		t.Errorf("version %d, year %d, enriched %v, ttb %v", v, year, enriched, ttb)
	}
	for _, bad := range []string{
		"UPDATE game SET time_to_beat_normally = 0",
		"UPDATE game SET time_to_beat_normally = -5",
		"INSERT INTO game_link (game_id, kind, url) VALUES (1, 'myspace', 'https://x.com')",
		"INSERT INTO game_link (game_id, kind, url) VALUES (1, 'steam', 'javascript:alert(1)')",
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	d.Exec("INSERT INTO game_link (game_id, kind, url) VALUES (1, 'steam', 'https://store.steampowered.com/app/1')")
	d.Exec("DELETE FROM game WHERE id = 1")
	var n int
	d.QueryRow("SELECT count(*) FROM game_link").Scan(&n)
	if n != 0 {
		t.Error("links survive their game")
	}
}

func TestEnrichStandaloneGameKeepsItsSeriesEmpty(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Undertale")
	link(t, d, id, 1)
	det := hades()
	det.Series = "Undertale" // IGDB names a collection after the game itself
	rep, _ := Enrich(context.Background(), d, &fakeEnricher{details: map[int64]meta.Details{1: det}}, EnrichOptions{Sleep: noSleep})
	var series sql.NullInt64
	d.QueryRow("SELECT series_id FROM game WHERE id = ?", id).Scan(&series)
	if series.Valid || rep.FilledSeries != 0 || rep.SeriesProposed != 1 || len(rep.SeriesExamples) != 1 {
		t.Errorf("series %v, report %+v", series, rep)
	}
	if n := names(t, d, "SELECT count(*) FROM series"); n != "0" {
		t.Errorf("%s series rows created", n)
	}
	var out bytes.Buffer
	PrintEnrichReport(&out, rep)
	if !strings.Contains(out.String(), "Undertale -> Undertale") {
		t.Errorf("proposal not listed: %s", out.String())
	}
}

func TestEnrichSeriesMatchingAnExistingOneUsesTheOwnersSpelling(t *testing.T) {
	d := openDB(t)
	sid, _ := db.GetOrCreate(d, "series", "Mario and Luigi")
	id := addGame(t, d, "Superstar Saga")
	link(t, d, id, 1)
	det := hades()
	det.Series = "Mario & Luigi"
	rep, _ := Enrich(context.Background(), d, &fakeEnricher{details: map[int64]meta.Details{1: det}}, EnrichOptions{Sleep: noSleep})
	var got int64
	d.QueryRow("SELECT series_id FROM game WHERE id = ?", id).Scan(&got)
	if got != sid || rep.FilledSeries != 1 || names(t, d, "SELECT count(*) FROM series") != "1" {
		t.Errorf("series %d (want %d), report %+v", got, sid, rep)
	}
}

func TestEnrichSharedCollectionBecomesASeriesAcrossRuns(t *testing.T) {
	d := openDB(t)
	a, b := addGame(t, d, "Part One"), addGame(t, d, "Part Two")
	link(t, d, a, 1)
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	for i := int64(1); i <= 2; i++ {
		det := hades()
		det.IGDBID, det.Series = i, "Big Saga"
		f.details[i] = det
	}
	// first run sees only one game of the collection: no series yet
	Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if n := names(t, d, "SELECT count(*) FROM series"); n != "0" {
		t.Fatalf("series after one game: %s", n)
	}
	// the second game is linked later; the next run completes the pair, including the first game
	link(t, d, b, 2)
	rep, _ := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if g := names(t, d, "SELECT s.name || ':' || count(*) FROM game g JOIN series s ON s.id = g.series_id GROUP BY s.id"); g != "Big Saga:2" || rep.FilledSeries != 1 || rep.SeriesCompleted != 1 {
		t.Errorf("series %q, report %+v", g, rep)
	}
}

func TestEnrichPlatformUsesTheLibrarysNamesAndNeverCreatesOne(t *testing.T) {
	d := openDB(t)
	addGame(t, d, "Anchor", "Switch") // the library calls it Switch
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	for i, plat := range []string{"Nintendo Switch", "Meta Quest 2"} {
		link(t, d, addGame(t, d, fmt.Sprintf("G%d", i)), int64(i+1))
		det := hades()
		det.IGDBID, det.Platforms, det.Series = int64(i+1), []string{plat}, ""
		f.details[det.IGDBID] = det
	}
	rep, _ := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	if rep.FilledPlatforms != 1 || rep.PlatformsUnmapped != 1 {
		t.Errorf("report %+v", rep)
	}
	if got := names(t, d, "SELECT name FROM platform ORDER BY name"); got != "Switch" {
		t.Errorf("platform rows = %s (none may be created)", got)
	}
	if got := names(t, d, "SELECT p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id JOIN game g ON g.id = x.game_id WHERE g.title = 'G0'"); got != "Switch" {
		t.Errorf("G0 platform = %q", got)
	}
}

// The Go and JavaScript platform alias tables describe the same mapping.
func TestPlatformAliasesMatchTheJS(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "js", "platforms.js"))
	if err != nil {
		t.Fatal(err)
	}
	block := string(src)
	block = block[strings.Index(block, "const ALIASES = {"):]
	block = block[:strings.Index(block, "};")]
	pairs := regexp.MustCompile(`(\w+): '([^']+)'`).FindAllStringSubmatch(block, -1)
	if len(pairs) < 20 {
		t.Fatalf("parsed only %d aliases from platforms.js", len(pairs))
	}
	js := map[string]string{}
	for _, p := range pairs {
		js[p[1]] = p[2]
		if PlatformKey(p[1]) != PlatformKey(p[2]) {
			t.Errorf("JS maps %q to %q, Go folds %q to %q", p[1], p[2], p[1], PlatformKey(p[1]))
		}
	}
	// and the other way: every Go alias that renames something must be in the JS table too
	for k, v := range platformAliases {
		if k == v {
			continue
		}
		if name, ok := js[k]; !ok || PlatformKey(name) != v {
			t.Errorf("Go folds %q to %q, but platforms.js maps it to %q", k, v, name)
		}
	}
}

func TestTimesToBeatDropOutliersAndContradictions(t *testing.T) {
	val := func(v any) int {
		if v == nil {
			return 0
		}
		return v.(int)
	}
	h, n, c := timesToBeat(meta.TimeToBeat{Hastily: 36000, Normally: 72000, Completely: 338978 * 60}) // BG3's 5650 h
	if val(h) != 600 || val(n) != 1200 || c != nil {
		t.Errorf("outlier: %v %v %v", h, n, c)
	}
	h, n, c = timesToBeat(meta.TimeToBeat{Hastily: 72000, Normally: 36000, Completely: 90000}) // quick longer than usual
	if h != nil || n != nil || c != nil {
		t.Errorf("inverted: %v %v %v", h, n, c)
	}
	h, n, c = timesToBeat(meta.TimeToBeat{Normally: 3600})
	if h != nil || val(n) != 60 || c != nil {
		t.Errorf("single: %v %v %v", h, n, c)
	}
}

func TestEnrichHonoursRetryAfter(t *testing.T) {
	d := openDB(t)
	link(t, d, addGame(t, d, "Hades"), 1)
	f := &retryAfterEnricher{fakeEnricher: fakeEnricher{details: map[int64]meta.Details{1: hades()}}}
	var slept []time.Duration
	o := EnrichOptions{Sleep: func(_ context.Context, w time.Duration) error { slept = append(slept, w); return nil }}
	if rep, err := Enrich(context.Background(), d, f, o); err != nil || rep.Enriched != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if len(slept) == 0 || slept[0] != 7*time.Second {
		t.Errorf("slept %v, want the 7s IGDB asked for first", slept)
	}
}

type retryAfterEnricher struct {
	fakeEnricher
	done bool
}

func (r *retryAfterEnricher) GameDetails(ctx context.Context, ids []int64) ([]meta.Details, error) {
	if !r.done {
		r.done = true
		return nil, &meta.HTTPError{Method: "POST", Host: "igdb", Status: 429, Text: "429", RetryAfter: 7 * time.Second}
	}
	return r.fakeEnricher.GameDetails(ctx, ids)
}

func TestEnrichDryRunReportsLikeARealRunAndWritesNothing(t *testing.T) {
	d := openDB(t)
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	for i := int64(1); i <= 2; i++ {
		link(t, d, addGame(t, d, fmt.Sprintf("Part %d", i)), i)
		det := hades()
		det.IGDBID, det.Series = i, "Big Saga"
		f.details[i] = det
	}
	rep, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, DryRun: true})
	if err != nil || rep.FilledSeries != 2 || rep.Enriched != 2 {
		t.Fatalf("%+v %v", rep, err)
	}
	if n := names(t, d, "SELECT (SELECT count(*) FROM series) || (SELECT count(*) FROM game WHERE igdb_enriched_at IS NOT NULL OR igdb_series IS NOT NULL)"); n != "00" {
		t.Errorf("a dry run wrote something: %s", n)
	}
}

func TestRefreshRewritesOnlyIGDBsOwnData(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades", "Switch")
	link(t, d, id, 1)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 3600}}}
	Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep})
	// the owner empties things afterwards; IGDB now has newer data
	d.Exec("UPDATE game SET release_year = NULL, developer_id = NULL WHERE id = ?", id)
	d.Exec("DELETE FROM game_genre")
	d.Exec("DELETE FROM game_theme")
	det := hades()
	det.Modes, det.Perspectives = []string{"Co-operative"}, []string{"Text"}
	det.Links = []meta.StoreLink{{Kind: "gog", URL: "https://www.gog.com/game/hades"}}
	f.details[1], f.ttb[1] = det, meta.TimeToBeat{Normally: 7200}
	rep, _ := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true})
	if rep.FilledYear+rep.FilledGenres+rep.FilledThemes+rep.FilledDeveloper+rep.FilledSeries+rep.FilledPlatforms != 0 {
		t.Errorf("refresh filled owner fields: %+v", rep)
	}
	if g := names(t, d, "SELECT (SELECT count(*) FROM game_genre) || (SELECT count(*) FROM game_theme) || coalesce(release_year, '-') || coalesce(developer_id, '-') FROM game"); g != "00--" {
		t.Errorf("owner-emptied fields were refilled: %s", g)
	}
	if g := names(t, d, "SELECT p.name FROM game_game_mode x JOIN game_mode p ON p.id = x.game_mode_id"); g != "Co-operative" {
		t.Errorf("modes = %s", g)
	}
	if g := names(t, d, "SELECT kind FROM game_link"); g != "gog" {
		t.Errorf("links = %s", g)
	}
	if g := names(t, d, "SELECT time_to_beat_normally FROM game"); g != "120" {
		t.Errorf("time = %s", g)
	}
}

func TestReportKeepsCompletedSeriesOnItsOwnLine(t *testing.T) {
	var out bytes.Buffer
	PrintEnrichReport(&out, &EnrichReport{SeriesCompleted: 2})
	if !strings.Contains(out.String(), "2 games read in earlier runs got a series") {
		t.Errorf("report:\n%s", out.String())
	}
}
