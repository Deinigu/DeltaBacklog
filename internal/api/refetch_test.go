package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// detailsMeta is the offline IGDB: fakeMeta plus the details the enrichment reads.
type detailsMeta struct {
	fakeMeta
	details map[int64]meta.Details
}

func (m *detailsMeta) GameDetails(_ context.Context, ids []int64) ([]meta.Details, error) {
	var out []meta.Details
	for _, id := range ids {
		if d, ok := m.details[id]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}
func (m *detailsMeta) TimeToBeat(context.Context, []int64) (map[int64]meta.TimeToBeat, error) {
	return map[int64]meta.TimeToBeat{}, nil
}

func postJSON(t *testing.T, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func TestIGDBDetailsPreviewApplyAndErrors(t *testing.T) {
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
	d.Exec("UPDATE game SET igdb_id = 7, release_year = 1999, notes = 'mine' WHERE id = ?", id)
	m := &detailsMeta{fakeMeta: fakeMeta{igdb: true}, details: map[int64]meta.Details{7: {IGDBID: 7, Year: 2024, Developer: "Supergiant Games", Genres: []string{"Roguelike"}}}}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Meta: m}).Handler())
	defer srv.Close()
	url := srv.URL + "/api/games/" + itoa(id) + "/igdb-details"

	res, out := postJSON(t, url, map[string]any{"mode": "replace"})
	if res.StatusCode != 200 || out["game"] != nil || len(out["changes"].([]any)) == 0 {
		t.Fatalf("preview: %d %v", res.StatusCode, out)
	}
	var year int
	d.QueryRow("SELECT release_year FROM game WHERE id = ?", id).Scan(&year)
	if year != 1999 {
		t.Fatalf("the preview wrote the year: %d", year)
	}
	res, out = postJSON(t, url, map[string]any{"mode": "empty", "apply": true})
	g := out["game"].(map[string]any)
	if res.StatusCode != 200 || g["release_year"] != float64(1999) || g["developer"] != "Supergiant Games" || g["notes"] != "mine" {
		t.Errorf("fill empty: %d %v", res.StatusCode, g)
	}
	res, out = postJSON(t, url, map[string]any{"mode": "replace", "apply": true})
	if g := out["game"].(map[string]any); res.StatusCode != 200 || g["release_year"] != float64(2024) {
		t.Errorf("replace: %d %v", res.StatusCode, g)
	}
	if res, _ := postJSON(t, url, map[string]any{"mode": "everything"}); res.StatusCode != 400 {
		t.Errorf("bad mode: %d", res.StatusCode)
	}
	if res, _ := postJSON(t, srv.URL+"/api/games/99999/igdb-details", map[string]any{"mode": "empty"}); res.StatusCode != 404 {
		t.Errorf("missing game: %d", res.StatusCode)
	}
	d.Exec("UPDATE game SET igdb_id = NULL WHERE id = ?", id)
	if res, _ := postJSON(t, url, map[string]any{"mode": "empty"}); res.StatusCode != 409 {
		t.Errorf("unlinked: %d", res.StatusCode)
	}
	// without IGDB credentials the endpoint says so
	srv2 := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir()}).Handler())
	defer srv2.Close()
	if res, _ := postJSON(t, srv2.URL+"/api/games/"+itoa(id)+"/igdb-details", map[string]any{"mode": "empty"}); res.StatusCode != 503 {
		t.Errorf("no IGDB: %d", res.StatusCode)
	}
}

func TestAttentionIgnoreRoundTrip(t *testing.T) {
	srv := newServer(t)
	get := func() map[string]any {
		res, err := http.Get(srv.URL + "/api/attention/ignored")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return out["ignored"]
	}
	if got := get(); len(got) != 0 {
		t.Fatalf("starts empty: %v", got)
	}
	postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "no_poster", "game_id": 3, "ignore": true})
	postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "no_poster", "game_id": 3, "ignore": true})
	postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "no_year", "game_id": 4, "ignore": true})
	got := get()
	if l := got["no_poster"].([]any); len(l) != 1 || l[0] != float64(3) || len(got["no_year"].([]any)) != 1 {
		t.Errorf("ignored = %v", got)
	}
	postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "no_poster", "game_id": 3, "ignore": false})
	if got := get(); got["no_poster"] != nil || got["no_year"] == nil {
		t.Errorf("after un-ignore = %v", got)
	}
	if res, _ := postJSON(t, srv.URL+"/api/attention/ignore", map[string]any{"group": "", "game_id": 3}); res.StatusCode != 400 {
		t.Errorf("empty group: %d", res.StatusCode)
	}
}
