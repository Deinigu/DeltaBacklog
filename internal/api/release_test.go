package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// dateMeta answers the release date of IGDB game 100 (a year-precision release) and nothing else.
type dateMeta struct {
	candMeta
	calls *int
}

func (m dateMeta) ReleaseDates(_ context.Context, ids []int64) (map[int64]meta.ReleaseOf, error) {
	*m.calls++
	out := map[int64]meta.ReleaseOf{}
	for _, id := range ids {
		if id == 100 {
			out[id] = meta.ReleaseOf{Release: meta.Release{Date: 1830211199, Format: meta.FormatYear}}
		}
	}
	return out, nil
}

func releaseOf(t *testing.T, e *seriesEnv, title string) (date, format *int64) {
	t.Helper()
	if e.srv != nil {
		e.srv.bg.Wait() // the date is stored after the response
	}
	body := e.req(t, "GET", "/api/games", "", 200)
	var games []struct {
		Title         string `json:"title"`
		ReleaseDate   *int64 `json:"release_date"`
		ReleaseFormat *int64 `json:"release_format"`
	}
	if err := json.Unmarshal(body, &games); err != nil {
		t.Fatal(err, string(body))
	}
	for _, g := range games {
		if g.Title == title {
			return g.ReleaseDate, g.ReleaseFormat
		}
	}
	t.Fatalf("%s not listed", title)
	return
}

// Adding or linking a game stores its IGDB release date; without one the game is added all the same.
func TestAddAndRelinkStoreTheReleaseDate(t *testing.T) {
	var calls int
	e := newSeriesEnv(t, true)
	// swap in a Meta that knows dates: rebuild the server on the same database
	e2 := newSeriesEnvWith(t, e.db, dateMeta{candMeta{&fakeMeta{igdb: true}}, &calls})

	e.db.Exec(`UPDATE game SET igdb_id = NULL WHERE title = 'Zed'`) // the fixture game holds IGDB id 100
	e2.req(t, "POST", "/api/games", `{"title":"Zed Origins","status":"wishlist","igdb_id":100}`, 201)
	if d, f := releaseOf(t, e2, "Zed Origins"); d == nil || *d != 1830211199 || *f != meta.FormatYear {
		t.Errorf("after add: %v %v", d, f)
	}
	// a game linked later gets the date through PATCH
	e2.req(t, "POST", "/api/games", `{"title":"Other","status":"wishlist"}`, 201)
	if d, _ := releaseOf(t, e2, "Other"); d != nil {
		t.Errorf("unlinked game has a date: %v", *d)
	}
	var id int64
	e2.db.QueryRow(`SELECT id FROM game WHERE title = 'Other'`).Scan(&id)
	e2.db.Exec(`UPDATE game SET igdb_id = NULL WHERE title = 'Zed Origins'`) // free the id for the relink
	e2.req(t, "PATCH", "/api/games/"+itoa(id), `{"igdb_id":100}`, 200)
	if d, _ := releaseOf(t, e2, "Other"); d == nil || *d != 1830211199 {
		t.Errorf("after relink: %v", d)
	}
	// saving the same link again asks IGDB for nothing
	before := calls
	e2.req(t, "PATCH", "/api/games/"+itoa(id), `{"igdb_id":100,"notes":"x"}`, 200)
	e2.srv.bg.Wait()
	if calls != before {
		t.Errorf("IGDB asked %d more times for an unchanged link", calls-before)
	}
	// relinking to another game drops the old game's date
	e2.req(t, "PATCH", "/api/games/"+itoa(id), `{"igdb_id":555}`, 200)
	if d, _ := releaseOf(t, e2, "Other"); d != nil {
		t.Errorf("date of the old link kept: %v", *d)
	}
}

func TestSeriesMissingCarriesDatesAndPlatforms(t *testing.T) {
	e := newSeriesEnv(t, true)
	e.db.Exec(`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (7, 'Zed Series', '2026-01-01T00:00:00Z')`)
	e.db.Exec(`UPDATE game SET igdb_parent_id = 0`)
	e.db.Exec(`INSERT INTO game_collection(game_id, collection_id, name) SELECT id, 7, 'Zed Series' FROM game`)
	e.db.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type, first_release_date, release_date_format, platforms) VALUES (7, 101, 'Zed II', 0, 1830211199, 2, 'PC, PS5')`)
	e.db.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type) VALUES (7, 102, 'Zed III', 0)`)
	body := string(e.req(t, "GET", "/api/series/missing", "", 200))
	for _, want := range []string{`"date":1830211199`, `"format":2`, `"platforms":"PC, PS5"`, `"date":null`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
}

// newSeriesEnvWith serves the database of an existing environment through another Meta.
func newSeriesEnvWith(t *testing.T, d *sql.DB, m Meta) *seriesEnv {
	t.Helper()
	s := &Server{DB: d, CoverDir: t.TempDir(), Meta: m}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &seriesEnv{db: d, url: srv.URL, srv: s}
}
