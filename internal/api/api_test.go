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
	"time"

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

func TestSlotOrder(t *testing.T) {
	// ids 10, 20, 30 occupy slots 5, 9, 12 in the global order; moving 30 to the front reuses those slots.
	got := SlotOrder([]int64{30, 10, 20}, map[int64]int{10: 5, 20: 9, 30: 12})
	if got[30] != 5 || got[10] != 9 || got[20] != 12 {
		t.Errorf("slots = %v", got)
	}
}

func TestPatchAndReorderGames(t *testing.T) {
	srv := newServer(t)
	get := func() []Game {
		_, body := do(t, "GET", srv.URL+"/api/games", "")
		var g []Game
		if err := json.Unmarshal(body, &g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	games := get()
	hades, old := games[0], games[1] // Playing, Backlog
	hURL := srv.URL + "/api/games/" + strconv.FormatInt(hades.ID, 10)

	res, body := do(t, "PATCH", hURL, `{"status":"up_next","favorite":false,"vr":true}`)
	var g Game
	json.Unmarshal(body, &g)
	if res.StatusCode != 200 || g.Status != "up_next" || g.Favorite || !g.VR {
		t.Fatalf("patch: %d %s", res.StatusCode, body)
	}
	if g.QueuePos == nil || *g.QueuePos <= *old.QueuePos {
		t.Errorf("a game that changes shelf should go to the end of the order: %v vs %v", g.QueuePos, old.QueuePos)
	}
	// becoming Playing or Finished records the current year; other shelves do not
	oURL := srv.URL + "/api/games/" + strconv.FormatInt(old.ID, 10)
	year := time.Now().Year()
	has := func(g Game) bool {
		for _, y := range g.Years {
			if y == year {
				return true
			}
		}
		return false
	}
	_, body = do(t, "PATCH", oURL, `{"status":"hold"}`)
	var og Game
	json.Unmarshal(body, &og)
	if has(og) {
		t.Errorf("Hold must not add a year: %v", og.Years)
	}
	_, body = do(t, "PATCH", oURL, `{"status":"finished"}`)
	json.Unmarshal(body, &og)
	if !has(og) || len(og.Periods) != 1 {
		t.Errorf("Finished should add %d and keep childhood: years=%v periods=%v", year, og.Years, og.Periods)
	}
	n := len(og.Years)
	do(t, "PATCH", oURL, `{"status":"dropped"}`)
	_, body = do(t, "PATCH", oURL, `{"status":"playing"}`)
	json.Unmarshal(body, &og)
	if len(og.Years) != n {
		t.Errorf("the same year must not be added twice: %v", og.Years)
	}

	// same status again must not move it
	pos := *g.QueuePos
	_, body = do(t, "PATCH", hURL, `{"status":"up_next"}`)
	json.Unmarshal(body, &g)
	if *g.QueuePos != pos {
		t.Errorf("unchanged status moved the game: %d -> %d", pos, *g.QueuePos)
	}
	for _, bad := range []string{`{"status":"nope"}`, `not json`} {
		if res, _ := do(t, "PATCH", hURL, bad); res.StatusCode != 400 {
			t.Errorf("PATCH %s = %d", bad, res.StatusCode)
		}
	}
	if res, _ := do(t, "PATCH", srv.URL+"/api/games/9999", `{"favorite":true}`); res.StatusCode != 404 {
		t.Errorf("missing game = %d", res.StatusCode)
	}

	// reorder the two games: positions swap, nothing else changes
	before := map[int64]int{}
	for _, x := range get() {
		before[x.ID] = *x.QueuePos
	}
	b, _ := json.Marshal([]int64{hades.ID, old.ID})
	if before[hades.ID] < before[old.ID] {
		b, _ = json.Marshal([]int64{old.ID, hades.ID})
	}
	if res, body := do(t, "POST", srv.URL+"/api/games/reorder", string(b)); res.StatusCode != 200 {
		t.Fatalf("reorder: %d %s", res.StatusCode, body)
	}
	after := map[int64]int{}
	for _, x := range get() {
		after[x.ID] = *x.QueuePos
	}
	if after[hades.ID] != before[old.ID] && after[hades.ID] != before[hades.ID] {
		t.Errorf("slots not reused: %v -> %v", before, after)
	}
	if after[hades.ID] == before[hades.ID] {
		t.Error("order did not change")
	}
	for _, bad := range []string{`[]`, `[1,1]`, `[9999]`, `nope`} {
		if res, _ := do(t, "POST", srv.URL+"/api/games/reorder", bad); res.StatusCode != 400 {
			t.Errorf("reorder %s = %d", bad, res.StatusCode)
		}
	}
}

func TestEditFields(t *testing.T) {
	srv := newServer(t)
	_, body := do(t, "GET", srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	hades := games[0]
	url := srv.URL + "/api/games/" + strconv.FormatInt(hades.ID, 10)
	patch := func(b string) (int, Game) {
		res, body := do(t, "PATCH", url, b)
		var g Game
		json.Unmarshal(body, &g)
		return res.StatusCode, g
	}

	code, g := patch(`{"title":"  The Hades 2 ","notes":"great","ownership":"Gifted","series":"Hades Saga","developer":"Supergiant","release_year":2025,
		"platforms":["PC","Switch"],"genres":["Roguelike"],"themes":[],"players":["Solo","Lucas"],"years":[2026,2024,2024],"periods":["always"]}`)
	if code != 200 {
		t.Fatalf("patch: %d", code)
	}
	if g.Title != "The Hades 2" || g.Notes != "great" || *g.Ownership != "Gifted" || *g.Series != "Hades Saga" || *g.Developer != "Supergiant" || *g.ReleaseYear != 2025 {
		t.Errorf("scalar fields: %+v", g)
	}
	if strings.Join(g.Platforms, ",") != "PC,Switch" || strings.Join(g.Genres, ",") != "Roguelike" || len(g.Themes) != 0 || strings.Join(g.Players, ",") != "Lucas,Solo" {
		t.Errorf("lists: %v %v %v %v", g.Platforms, g.Genres, g.Themes, g.Players)
	}
	if len(g.Years) != 2 || g.Years[0] != 2024 || g.Years[1] != 2026 || len(g.Periods) != 1 || g.Periods[0] != "always" {
		t.Errorf("years=%v periods=%v", g.Years, g.Periods)
	}

	// a patch touching one field leaves the others alone; empty string / 0 clear
	code, g = patch(`{"series":"","release_year":0,"years":[]}`)
	if code != 200 || g.Series != nil || g.ReleaseYear != nil || len(g.Years) != 0 || g.Title != "The Hades 2" || len(g.Periods) != 1 || len(g.Platforms) != 2 {
		t.Errorf("clear: %d %+v", code, g)
	}

	// the same lookup value reused by another game is one row, not a duplicate
	_, body = do(t, "PATCH", srv.URL+"/api/games/"+strconv.FormatInt(games[1].ID, 10), `{"platforms":["pc"]}`)
	var o Game
	json.Unmarshal(body, &o)
	_, body = do(t, "GET", srv.URL+"/api/facets", "")
	var facets map[string][]Facet
	json.Unmarshal(body, &facets)
	pcs := 0
	for _, f := range facets["platform"] {
		if strings.EqualFold(f.Key, "pc") {
			pcs++
		}
	}
	if pcs != 1 {
		t.Errorf("platform PC duplicated: %v", facets["platform"])
	}

	for _, bad := range []string{`{"title":"  "}`, `{"release_year":1800}`, `{"years":[1900]}`, `{"periods":["later"]}`, `{"genres":["` + strings.Repeat("x", 200) + `"]}`} {
		if code, _ := patch(bad); code != 400 {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	if code, g := patch(`{}`); code != 200 || g.Title != "The Hades 2" {
		t.Errorf("empty patch changed the game: %d %+v", code, g)
	}
}

func TestStatusYearSurvivesExplicitYears(t *testing.T) {
	srv := newServer(t)
	_, body := do(t, "GET", srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	old := games[1] // Backlog
	year := time.Now().Year()
	_, body = do(t, "PATCH", srv.URL+"/api/games/"+strconv.FormatInt(old.ID, 10), `{"status":"playing","years":[2020]}`)
	var g Game
	json.Unmarshal(body, &g)
	if len(g.Years) != 2 || g.Years[0] != 2020 || g.Years[1] != year {
		t.Errorf("years = %v, want [2020 %d]", g.Years, year)
	}
}
