package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

const csvData = `id,Title,Platform,Status,Style,VR,Genre,Boxart,Completed,Years_played,Series,Notas,Ownership,Played_with,Favorito,nc_order
1,Hades II,PC,Playing,Action,f,"RPG, Indie",,f,"2025,2026",Hades,,Owned,Solo,t,1
2,Old Game,"PC, PS3",Backlog,,f,Indie,,f,Childhood,,,Pirated,,f,2
`

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir()}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestGamesAndFacets(t *testing.T) {
	srv := newServer(t)

	res, err := http.Get(srv.URL + "/api/games")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var games []Game
	if err := json.NewDecoder(res.Body).Decode(&games); err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].Title != "Hades II" { // Playing sorts before Backlog
		t.Fatalf("games = %+v", games)
	}
	h := games[0]
	if !h.Favorite || len(h.Platforms) != 1 || len(h.Genres) != 2 || len(h.Years) != 2 || h.Years[1] != 2026 {
		t.Errorf("hades = %+v", h)
	}
	if o := games[1]; len(o.Periods) != 1 || o.Periods[0] != "childhood" || len(o.Players) != 0 || o.Years == nil {
		t.Errorf("old game = %+v", o)
	}

	res2, err := http.Get(srv.URL + "/api/facets")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var facets map[string][]Facet
	if err := json.NewDecoder(res2.Body).Decode(&facets); err != nil {
		t.Fatal(err)
	}
	if len(facets["status"]) != 9 {
		t.Errorf("status facets = %v", facets["status"])
	}
	if g := facets["genre"]; len(g) == 0 || g[0].Label != "Indie" || g[0].Count != 2 {
		t.Errorf("genre facets = %v", g)
	}
}

func TestHealthz(t *testing.T) {
	srv := newServer(t)
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", err, res)
	}
	res.Body.Close()
}
