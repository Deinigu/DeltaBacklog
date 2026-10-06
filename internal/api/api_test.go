package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

func do(t *testing.T, method, url, body string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestViewsCRUD(t *testing.T) {
	srv := newServer(t)

	res, body := do(t, "GET", srv.URL+"/api/views", "")
	var views []View
	json.Unmarshal(body, &views)
	if res.StatusCode != 200 || len(views) != 5 || views[0].Name != "Playing now" {
		t.Fatalf("starters: %d %s", res.StatusCode, body)
	}

	res, body = do(t, "POST", srv.URL+"/api/views", `{"name":" Indie RPGs ","filter":{"in":{"genre":["RPG","Indie"]},"all":["genre"]},"sort":[{"field":"title","dir":"asc"}],"layout":"table"}`)
	var v View
	json.Unmarshal(body, &v)
	if res.StatusCode != 201 || v.Name != "Indie RPGs" || v.Layout != "table" || !v.Pinned || v.Pos != 6 || !strings.Contains(string(v.Filter), `"all"`) {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}

	url := srv.URL + "/api/views/" + strconv.FormatInt(v.ID, 10)
	res, body = do(t, "PATCH", url, `{"name":"Renamed","pinned":false}`)
	json.Unmarshal(body, &v)
	if res.StatusCode != 200 || v.Name != "Renamed" || v.Pinned || v.Layout != "table" || !strings.Contains(string(v.Filter), "Indie") {
		t.Fatalf("patch should change only given fields: %d %s", res.StatusCode, body)
	}

	for _, bad := range []string{`{"name":""}`, `{"layout":"grid"}`, `not json`} {
		if res, _ := do(t, "PATCH", url, bad); res.StatusCode != 400 {
			t.Errorf("PATCH %s = %d, want 400", bad, res.StatusCode)
		}
	}
	if res, _ := do(t, "POST", srv.URL+"/api/views", `{"layout":"cards"}`); res.StatusCode != 400 {
		t.Errorf("create without name = %d", res.StatusCode)
	}
	if res, _ := do(t, "PATCH", srv.URL+"/api/views/9999", `{"name":"x"}`); res.StatusCode != 404 {
		t.Errorf("missing view = %d", res.StatusCode)
	}

	// reorder: put the new view first
	ids := []int64{v.ID}
	for _, o := range views {
		ids = append(ids, o.ID)
	}
	b, _ := json.Marshal(ids)
	res, body = do(t, "PUT", srv.URL+"/api/views/order", string(b))
	json.Unmarshal(body, &views)
	if res.StatusCode != 200 || views[0].ID != v.ID {
		t.Fatalf("reorder: %d %s", res.StatusCode, body)
	}

	if res, _ := do(t, "DELETE", url, ""); res.StatusCode != 204 {
		t.Errorf("delete = %d", res.StatusCode)
	}
	if res, _ := do(t, "DELETE", url, ""); res.StatusCode != 404 {
		t.Errorf("second delete = %d", res.StatusCode)
	}
}
