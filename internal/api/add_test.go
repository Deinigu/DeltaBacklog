package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

type fakeMeta struct {
	igdb, sgdb bool
	results    []meta.Candidate
	covers     []meta.CoverOption
	images     []meta.CoverOption
	imgID      int64
	imgTitle   string
	resolved   string
	resolveErr error
	igdbErr    error
	lastQuery  string
}

func (f *fakeMeta) Configured() (bool, bool) { return f.igdb, f.sgdb }
func (f *fakeMeta) SearchIGDB(_ context.Context, t string) ([]meta.Candidate, error) {
	f.lastQuery = t
	return f.results, f.igdbErr
}
func (f *fakeMeta) SearchCovers(_ context.Context, t string) ([]meta.CoverOption, error) {
	return f.covers, nil
}
func (f *fakeMeta) IGDBImages(_ context.Context, id int64, title string) ([]meta.CoverOption, error) {
	f.imgID, f.imgTitle = id, title
	return f.images, nil
}
func (f *fakeMeta) ResolveLink(_ context.Context, l string) (string, error) {
	return f.resolved, f.resolveErr
}

// addEnv is a server with a fake metadata service and a fake image host.
type addEnv struct {
	db       *sql.DB
	srv      *httptest.Server
	meta     *fakeMeta
	coverDir string
	images   *httptest.Server
}

func newAddEnv(t *testing.T) *addEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			img := image.NewRGBA(image.Rect(0, 0, 920, 430))
			for x := 0; x < 920; x++ {
				img.Set(x, 10, color.RGBA{200, 30, 30, 255})
			}
			png.Encode(w, img)
		case "/redirect.png":
			http.Redirect(w, r, "/ok.png", http.StatusFound)
		case "/huge.png":
			png.Encode(w, image.NewGray(image.Rect(0, 0, 6000, 6000)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(images.Close)
	coverDir := t.TempDir()
	fm := &fakeMeta{igdb: true, sgdb: true}
	s := &Server{DB: d, CoverDir: coverDir, Meta: fm, CoverHosts: []string{strings.TrimPrefix(images.URL, "http://")}}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &addEnv{db: d, srv: srv, meta: fm, coverDir: coverDir, images: images}
}

func TestLookup(t *testing.T) {
	e := newAddEnv(t)
	e.meta.results = []meta.Candidate{{IGDBID: 1, Name: "Hades II"}, {IGDBID: 2, Name: "hades"}}
	_, body := do(t, "GET", e.srv.URL+"/api/lookup?q=hades", "")
	var out struct {
		Configured bool             `json:"configured"`
		Query      string           `json:"query"`
		Results    []meta.Candidate `json:"results"`
	}
	json.Unmarshal(body, &out)
	if !out.Configured || len(out.Results) != 2 || out.Results[0].Name != "hades" {
		t.Errorf("exact title should rank first: %+v", out)
	}

	// a pasted link is resolved to a title first
	e.meta.resolved = "Hades II"
	_, body = do(t, "GET", e.srv.URL+"/api/lookup?q=https%3A%2F%2Fstore.steampowered.com%2Fapp%2F1145350%2F", "")
	json.Unmarshal(body, &out)
	if out.Query != "Hades II" || e.meta.lastQuery != "Hades II" {
		t.Errorf("link: query=%q igdb searched %q", out.Query, e.meta.lastQuery)
	}
	e.meta.resolveErr = errors.New("unsupported link")
	if res, _ := do(t, "GET", e.srv.URL+"/api/lookup?q=https%3A%2F%2Fevil.example%2F", ""); res.StatusCode != 422 {
		t.Errorf("bad link = %d, want 422", res.StatusCode)
	}
	e.meta.resolveErr = nil
	e.meta.igdbErr = errors.New("down")
	if res, _ := do(t, "GET", e.srv.URL+"/api/lookup?q=hades", ""); res.StatusCode != 502 {
		t.Errorf("igdb down = %d, want 502", res.StatusCode)
	}

	e.meta.igdb = false
	_, body = do(t, "GET", e.srv.URL+"/api/lookup?q=hades", "")
	json.Unmarshal(body, &out)
	if out.Configured || len(out.Results) != 0 {
		t.Errorf("unconfigured: %+v", out)
	}
}

func createGameReq(t *testing.T, e *addEnv, body string) (int, map[string]any) {
	t.Helper()
	res, b := do(t, "POST", e.srv.URL+"/api/games", body)
	var out map[string]any
	json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func TestCreateGame(t *testing.T) {
	e := newAddEnv(t)

	code, out := createGameReq(t, e, `{"title":" Bare Game "}`)
	g, _ := out["game"].(map[string]any)
	if code != 201 || g["title"] != "Bare Game" || g["status"] != "backlog" {
		t.Fatalf("minimal create: %d %v", code, out)
	}

	code, out = createGameReq(t, e, `{"title":"Hades III","status":"playing","igdb_id":777,"platforms":["PC"],"genres":["RPG"],
		"themes":["4X (explore, expand, exploit, and exterminate)","Action"],"series":"Hades","developer":"Supergiant","release_year":2027,
		"ownership":"Owned","cover_url":"`+e.images.URL+`/ok.png"}`)
	g, _ = out["game"].(map[string]any)
	if code != 201 || out["cover_error"] != "" {
		t.Fatalf("full create: %d %v", code, out)
	}
	themes := g["themes"].([]any)
	if len(themes) != 2 || themes[0] != "4X" {
		t.Errorf("themes not folded: %v", themes)
	}
	years := g["years"].([]any)
	if len(years) != 1 || int(years[0].(float64)) != time.Now().Year() {
		t.Errorf("a game created as Playing should record this year: %v", years)
	}
	file, _ := g["cover"].(string)
	if file == "" {
		t.Fatal("cover not stored")
	}
	if st, err := os.Stat(filepath.Join(e.coverDir, file)); err != nil || st.Size() == 0 {
		t.Errorf("cover file missing: %v", err)
	}
	if f, _ := os.Open(filepath.Join(e.coverDir, file)); f != nil {
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width > 480 {
			t.Errorf("cover should be a resized image: %+v %v", cfg, err)
		}
	}

	// the same IGDB game cannot be added twice
	code, out = createGameReq(t, e, `{"title":"Hades III again","igdb_id":777}`)
	if code != 409 || out["id"] == nil {
		t.Errorf("duplicate igdb id: %d %v", code, out)
	}

	// a cover that fails to download does not lose the game
	code, out = createGameReq(t, e, `{"title":"No Cover","cover_url":"`+e.images.URL+`/missing.png"}`)
	if code != 201 || out["cover_error"] == "" {
		t.Errorf("failed cover: %d %v", code, out)
	}
	// redirects are refused and oversized images rejected, but the game is still created
	for _, u := range []string{e.images.URL + "/redirect.png", e.images.URL + "/huge.png"} {
		if _, out = createGameReq(t, e, `{"title":"X `+filepath.Base(u)+`","cover_url":"`+u+`"}`); out["cover_error"] == "" {
			t.Errorf("%s should not produce a cover", u)
		}
	}
	for _, bad := range []string{`{"title":""}`, `{}`, `{"title":"x","status":"nope"}`, `{"title":"x","cover_url":"https://evil.example.com/a.png"}`, `{"title":"x","cover_url":"file:///etc/passwd"}`, `{"title":"x","release_year":1800}`, `nope`} {
		if code, _ = createGameReq(t, e, bad); code != 400 {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	// new games go to the end of the manual order
	_, body := do(t, "GET", e.srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	max, newest := 0, 0
	for _, x := range games {
		if *x.QueuePos > max {
			max = *x.QueuePos
		}
		if x.Title == "No Cover" {
			newest = *x.QueuePos
		}
	}
	if newest == 0 || newest > max {
		t.Errorf("queue positions: newest=%d max=%d", newest, max)
	}
}

func TestSetCoverAndDelete(t *testing.T) {
	e := newAddEnv(t)
	_, out := createGameReq(t, e, `{"title":"Cover Test"}`)
	gm, _ := out["game"].(map[string]any)
	idJSON, _ := json.Marshal(gm["id"])
	url := e.srv.URL + "/api/games/" + string(bytes.TrimSpace(idJSON))

	if res, _ := do(t, "POST", url+"/cover", `{"url":"https://evil.example.com/x.png"}`); res.StatusCode != 400 {
		t.Errorf("foreign host = %d", res.StatusCode)
	}
	res, body := do(t, "POST", url+"/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	var g Game
	json.Unmarshal(body, &g)
	if res.StatusCode != 200 || g.Cover == nil {
		t.Fatalf("set cover: %d %s", res.StatusCode, body)
	}
	if res, _ := do(t, "POST", e.srv.URL+"/api/games/9999/cover", `{"url":"`+e.images.URL+`/ok.png"}`); res.StatusCode != 404 {
		t.Errorf("missing game = %d", res.StatusCode)
	}

	if res, _ := do(t, "DELETE", url, ""); res.StatusCode != 204 {
		t.Errorf("delete = %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.coverDir, *g.Cover)); !os.IsNotExist(err) {
		t.Error("cover file should be removed with the game")
	}
	if res, _ := do(t, "DELETE", url, ""); res.StatusCode != 404 {
		t.Errorf("second delete = %d", res.StatusCode)
	}
	_, body = do(t, "GET", e.srv.URL+"/api/games", "")
	if strings.Contains(string(body), "Cover Test") {
		t.Error("deleted game still listed")
	}
}

func TestLinkGameToIGDB(t *testing.T) {
	e := newAddEnv(t)
	_, body := do(t, "GET", e.srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	a, b := games[0], games[1]
	if a.IGDBID != nil {
		t.Fatalf("imported games have no IGDB id yet: %v", *a.IGDBID)
	}
	urlOf := func(g Game) string { return e.srv.URL + "/api/games/" + itoa64(g.ID) }

	res, body := do(t, "PATCH", urlOf(a), `{"igdb_id":228525}`)
	var got Game
	json.Unmarshal(body, &got)
	if res.StatusCode != 200 || got.IGDBID == nil || *got.IGDBID != 228525 {
		t.Fatalf("link: %d %s", res.StatusCode, body)
	}
	// setting the same id again on the same game is fine
	if res, _ := do(t, "PATCH", urlOf(a), `{"igdb_id":228525,"notes":"x"}`); res.StatusCode != 200 {
		t.Errorf("re-link same game = %d", res.StatusCode)
	}
	// another game cannot take it, and nothing else in that patch is applied
	res, body = do(t, "PATCH", urlOf(b), `{"igdb_id":228525,"notes":"should not be saved"}`)
	var conflict map[string]any
	json.Unmarshal(body, &conflict)
	if res.StatusCode != 409 || conflict["id"] == nil {
		t.Errorf("conflict: %d %s", res.StatusCode, body)
	}
	_, body = do(t, "GET", e.srv.URL+"/api/games", "")
	json.Unmarshal(body, &games)
	for _, g := range games {
		if g.ID == b.ID && g.Notes != "" {
			t.Errorf("a rejected patch must not change other fields: %q", g.Notes)
		}
	}
	// 0 clears the link; negative is invalid
	_, body = do(t, "PATCH", urlOf(a), `{"igdb_id":0}`)
	json.Unmarshal(body, &got)
	if got.IGDBID != nil {
		t.Errorf("0 should clear the link: %v", *got.IGDBID)
	}
	if res, _ := do(t, "PATCH", urlOf(a), `{"igdb_id":-5}`); res.StatusCode != 400 {
		t.Errorf("negative id = %d", res.StatusCode)
	}
	// the freed id can now go to the other game
	if res, _ := do(t, "PATCH", urlOf(b), `{"igdb_id":228525}`); res.StatusCode != 200 {
		t.Errorf("freed id = %d", res.StatusCode)
	}
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestCoverSources(t *testing.T) {
	e := newAddEnv(t)
	e.meta.covers = []meta.CoverOption{{URL: "https://cdn2.steamgriddb.com/a.png", Source: "steamgriddb"}}
	e.meta.images = []meta.CoverOption{{URL: "https://images.igdb.com/x.jpg", Source: "igdb", Kind: "Artwork"}, {URL: "https://images.igdb.com/y.jpg", Source: "igdb", Kind: "Cover"}}
	type resp struct {
		Configured bool               `json:"configured"`
		Source     string             `json:"source"`
		Covers     []meta.CoverOption `json:"covers"`
	}
	get := func(q string) (int, resp) {
		res, body := do(t, "GET", e.srv.URL+"/api/covers/search"+q, "")
		var r resp
		json.Unmarshal(body, &r)
		return res.StatusCode, r
	}

	// default source is SteamGridDB, as before
	if code, r := get("?q=hades"); code != 200 || r.Source != "steamgriddb" || len(r.Covers) != 1 {
		t.Errorf("default: %d %+v", code, r)
	}
	// IGDB: the linked id and the title are both passed through
	code, r := get("?source=igdb&q=Hades&igdb_id=228525")
	if code != 200 || r.Source != "igdb" || len(r.Covers) != 2 || r.Covers[0].Kind != "Artwork" || e.meta.imgID != 228525 || e.meta.imgTitle != "Hades" {
		t.Errorf("igdb: %d %+v id=%d title=%q", code, r, e.meta.imgID, e.meta.imgTitle)
	}
	// without an id the title alone is enough
	if code, r := get("?source=igdb&q=Hades"); code != 200 || len(r.Covers) != 2 || e.meta.imgID != 0 {
		t.Errorf("igdb by title: %d %+v", code, r)
	}
	// an id alone is enough too
	if code, r := get("?source=igdb&igdb_id=5"); code != 200 || len(r.Covers) != 2 {
		t.Errorf("igdb by id: %d %+v", code, r)
	}
	for _, bad := range []string{"?source=bing&q=x", "?source=igdb&igdb_id=abc", "?source=igdb&igdb_id=-1&q=x"} {
		if code, _ := get(bad); code != 400 {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	// each source reports its own configuration
	e.meta.igdb = false
	if _, r := get("?source=igdb&q=x"); r.Configured || len(r.Covers) != 0 {
		t.Errorf("igdb off: %+v", r)
	}
	if _, r := get("?source=steamgriddb&q=x"); !r.Configured || len(r.Covers) != 1 {
		t.Errorf("sgdb still on: %+v", r)
	}
	// nothing to search for
	e.meta.igdb = true
	if _, r := get("?source=igdb"); len(r.Covers) != 0 {
		t.Errorf("empty query: %+v", r)
	}
}

func TestPatchFoldsThemesLikeCreate(t *testing.T) {
	e := newAddEnv(t)
	_, body := do(t, "GET", e.srv.URL+"/api/games", "")
	var games []Game
	json.Unmarshal(body, &games)
	url := e.srv.URL + "/api/games/" + itoa64(games[0].ID)
	_, body = do(t, "PATCH", url, `{"themes":["4X (explore, expand, exploit, and exterminate)","Action"]}`)
	var g Game
	json.Unmarshal(body, &g)
	if len(g.Themes) != 2 || g.Themes[0] != "4X" {
		t.Errorf("themes saved by an edit must be folded: %v", g.Themes)
	}
	_, body = do(t, "GET", e.srv.URL+"/api/facets", "")
	var facets map[string][]Facet
	json.Unmarshal(body, &facets)
	for _, f := range facets["theme"] {
		if len(f.Key) > 20 {
			t.Errorf("an unfolded theme name reached the facets: %q", f.Key)
		}
	}
}

func TestCoverVersionOnlyChangesWithTheImage(t *testing.T) {
	e := newAddEnv(t)
	_, out := createGameReq(t, e, `{"title":"Versioned","cover_url":"`+e.images.URL+`/ok.png"}`)
	g0, _ := out["game"].(map[string]any)
	v0, _ := g0["cover_v"].(string)
	if v0 == "" {
		t.Fatalf("a game with a cover needs a cover version: %v", g0)
	}
	url := e.srv.URL + "/api/games/" + itoa64(int64(g0["id"].(float64)))
	time.Sleep(1100 * time.Millisecond) // timestamps have one-second resolution

	_, body := do(t, "PATCH", url, `{"status":"up_next","favorite":true,"notes":"edited"}`)
	var g Game
	json.Unmarshal(body, &g)
	if g.CoverV != v0 {
		t.Errorf("editing a game must not change its cover version: %q -> %q", v0, g.CoverV)
	}
	if g.UpdatedAt == g0["updated_at"] {
		t.Error("updated_at should still move on an edit")
	}

	_, body = do(t, "POST", url+"/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	json.Unmarshal(body, &g)
	if g.CoverV == v0 || g.CoverV == "" {
		t.Errorf("a new cover must get a new version: %q -> %q", v0, g.CoverV)
	}

	// Two covers in the same second must still differ, or the browser keeps showing the first one.
	v1 := g.CoverV
	_, body = do(t, "POST", url+"/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	json.Unmarshal(body, &g)
	if g.CoverV == v1 {
		t.Errorf("a cover changed within the same second kept its version %q", v1)
	}
}

func TestCreateReturnsJSONContentType(t *testing.T) {
	e := newAddEnv(t)
	res, _ := do(t, "POST", e.srv.URL+"/api/games", `{"title":"Typed"}`)
	if res.StatusCode != 201 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("created: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	res, _ = do(t, "POST", e.srv.URL+"/api/views", `{"name":"Typed view"}`)
	if res.StatusCode != 201 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("view created: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
