package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
