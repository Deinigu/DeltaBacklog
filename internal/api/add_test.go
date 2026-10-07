package api

import (
	"bytes"
	"context"
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
func (f *fakeMeta) ResolveLink(_ context.Context, l string) (string, error) {
	return f.resolved, f.resolveErr
}

// addEnv is a server with a fake metadata service and a fake image host.
type addEnv struct {
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
	return &addEnv{srv: srv, meta: fm, coverDir: coverDir, images: images}
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
