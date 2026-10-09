package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

// The API serves what `sync-igdb --enrich` stored, and empty arrays (never null) for games without it.
func TestGamesCarryEnrichment(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	mode, _ := db.GetOrCreate(d, "game_mode", "Single player")
	persp, _ := db.GetOrCreate(d, "perspective", "First person")
	d.Exec("UPDATE game SET time_to_beat_normally = 750, time_to_beat_hastily = 600 WHERE title = 'Hades II'")
	d.Exec("INSERT INTO game_game_mode SELECT id, ? FROM game WHERE title = 'Hades II'", mode)
	d.Exec("INSERT INTO game_perspective SELECT id, ? FROM game WHERE title = 'Hades II'", persp)
	d.Exec("INSERT INTO game_link SELECT id, 'steam', 'https://store.steampowered.com/app/1' FROM game WHERE title = 'Hades II'")

	srv := newServerFrom(t, d)
	var games []map[string]any
	res, err := http.Get(srv + "/api/games")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&games); err != nil {
		t.Fatal(err)
	}
	var hades, old map[string]any
	for _, g := range games {
		if g["title"] == "Hades II" {
			hades = g
		} else {
			old = g
		}
	}
	if hades["ttb_normally"] != float64(750) || hades["ttb_hastily"] != float64(600) || hades["ttb_completely"] != nil {
		t.Errorf("time to beat: %v", hades)
	}
	if m := hades["modes"].([]any); len(m) != 1 || m[0] != "Single player" {
		t.Errorf("modes: %v", hades["modes"])
	}
	if l := hades["links"].([]any); len(l) != 1 || l[0].(map[string]any)["kind"] != "steam" {
		t.Errorf("links: %v", hades["links"])
	}
	for _, k := range []string{"modes", "perspectives", "links"} {
		if v, ok := old[k].([]any); !ok || len(v) != 0 {
			t.Errorf("old game %s = %#v, want []", k, old[k])
		}
	}
	for k := range hades { // the owner turned scores down: nothing like them may reach the client
		if strings.Contains(k, "rating") || strings.Contains(k, "score") {
			t.Errorf("unexpected field %s", k)
		}
	}

	var facets map[string][]Facet
	r2, err := http.Get(srv + "/api/facets")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	json.NewDecoder(r2.Body).Decode(&facets)
	if f := facets["mode"]; len(f) != 1 || f[0].Label != "Single player" || f[0].Count != 1 {
		t.Errorf("mode facets = %v", f)
	}
	if f := facets["perspective"]; len(f) != 1 || f[0].Count != 1 {
		t.Errorf("perspective facets = %v", f)
	}
}

func newServerFrom(t *testing.T, d *sql.DB) string {
	t.Helper()
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir()}).Handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

// Another IGDB game (or none) means the old game's enrichment is gone: modes, perspectives, store links, times.
func TestChangingTheIGDBLinkClearsTheEnrichment(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	var id int64
	d.QueryRow("SELECT id FROM game WHERE title = 'Hades II'").Scan(&id)
	enrich := func() {
		mode, _ := db.GetOrCreate(d, "game_mode", "Single player")
		persp, _ := db.GetOrCreate(d, "perspective", "First person")
		d.Exec("UPDATE game SET igdb_id = 5, igdb_enriched_at = '2026-01-01T00:00:00Z', igdb_series = 'Hades', time_to_beat_normally = 750, time_to_beat_hastily = 600, time_to_beat_completely = 900 WHERE id = ?", id)
		d.Exec("INSERT OR IGNORE INTO game_game_mode VALUES (?, ?)", id, mode)
		d.Exec("INSERT OR IGNORE INTO game_perspective VALUES (?, ?)", id, persp)
		d.Exec("INSERT OR REPLACE INTO game_link VALUES (?, 'steam', 'https://store.steampowered.com/app/1')", id)
	}
	state := func() string {
		var s string
		d.QueryRow(`SELECT coalesce(igdb_enriched_at, '-') || '|' || coalesce(igdb_series, '-') || '|' || coalesce(time_to_beat_normally, '-') ||
			'|' || (SELECT count(*) FROM game_game_mode WHERE game_id = g.id) || (SELECT count(*) FROM game_perspective WHERE game_id = g.id) ||
			(SELECT count(*) FROM game_link WHERE game_id = g.id) FROM game g WHERE id = ?`, id).Scan(&s)
		return s
	}
	srv := newServerFrom(t, d)
	patch := func(body string) {
		t.Helper()
		req, _ := http.NewRequest("PATCH", srv+"/api/games/"+strconv.FormatInt(id, 10), strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("PATCH %s: %v %v", body, res, err)
		}
		res.Body.Close()
	}
	enrich()
	patch(`{"igdb_id":5,"notes":"same link, nothing is cleared"}`)
	if s := state(); s != "2026-01-01T00:00:00Z|Hades|750|111" {
		t.Errorf("same id cleared something: %s", s)
	}
	patch(`{"igdb_id":6}`)
	if s := state(); s != "-|-|-|000" {
		t.Errorf("after relinking: %s", s)
	}
	enrich()
	patch(`{"igdb_id":0}`)
	if s := state(); s != "-|-|-|000" {
		t.Errorf("after unlinking: %s", s)
	}
	var link sql.NullInt64
	d.QueryRow("SELECT igdb_id FROM game WHERE id = ?", id).Scan(&link)
	if link.Valid {
		t.Error("still linked")
	}
}
