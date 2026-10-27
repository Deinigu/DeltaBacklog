package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// collFake is an offline IGDB for the Series page: collection 7 has Zed (100, in the library), Zed II and Zed III.
type collFake struct{ reads int }

func (f *collFake) GameLinks(_ context.Context, ids []int64) (map[int64]meta.GameLinks, error) {
	out := map[int64]meta.GameLinks{}
	for _, id := range ids {
		if id == 100 {
			out[id] = meta.GameLinks{Collections: []meta.CollectionRef{{ID: 7, Name: "Zed Series"}}, ParentID: 90}
		}
	}
	return out, nil
}

func (f *collFake) CollectionGames(context.Context, int64) (string, []meta.CollectionGame, error) {
	f.reads++
	return "Zed Series", []meta.CollectionGame{
		{IGDBID: 100, Name: "Zed", Year: 1997, ParentID: 90, CoverID: "a"},
		{IGDBID: 101, Name: "Zed II", Year: 2005, CoverID: "b"},
		{IGDBID: 102, Name: "Zed III", Year: 2011, CoverID: "c"},
	}, nil
}

type candMeta struct{ *fakeMeta }

func (candMeta) GameCandidate(_ context.Context, id int64) (meta.Candidate, error) {
	if id == 101 {
		return meta.Candidate{IGDBID: 101, Name: "Zed II", Year: 2005, Genres: []string{"RPG"}, Platforms: []string{"PC"}, Themes: []string{}}, nil
	}
	return meta.Candidate{}, meta.ErrNoGame
}

type seriesEnv struct {
	db   *sql.DB
	url  string
	fake *collFake
	dir  string
	srv  *Server // set by newSeriesEnvWith: the tests wait for its background work
}

func newSeriesEnv(t *testing.T, configured bool) *seriesEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	sid, _ := db.GetOrCreate(d, "series", "Zeds")
	if _, err := d.Exec(`INSERT INTO game(title, title_sort, status_id, series_id, igdb_id) VALUES ('Zed','zed',7,?,100)`, sid); err != nil {
		t.Fatal(err)
	}
	fake := &collFake{}
	dir := t.TempDir()
	s := &Server{DB: d, CoverDir: dir, Meta: candMeta{&fakeMeta{igdb: configured}}, Collections: fake,
		MissingPosterFetch: func(url, dir string, igdbID int64) (string, error) {
			name := "i" + strconv.FormatInt(igdbID, 10) + ".jpg"
			return name, os.WriteFile(filepath.Join(dir, name), []byte("jpeg"), 0o644)
		}}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &seriesEnv{db: d, url: srv.URL, fake: fake, dir: dir}
}

func (e *seriesEnv) req(t *testing.T, method, path, body string, want int) []byte {
	t.Helper()
	req, _ := http.NewRequest(method, e.url+path, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, res.StatusCode, want, sb.String())
	}
	return []byte(sb.String())
}

type missingResp struct {
	OwnedRoots []int64              `json:"owned_roots"`
	Configured bool                 `json:"configured"`
	Series     []collections.Series `json:"series"`
	Refresh    seriesStatus         `json:"refresh"`
}

func (e *seriesEnv) missing(t *testing.T) missingResp {
	t.Helper()
	var r missingResp
	if err := json.Unmarshal(e.req(t, "GET", "/api/series/missing", "", 200), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *seriesEnv) refresh(t *testing.T) missingResp {
	t.Helper()
	e.req(t, "POST", "/api/series/refresh", "", 202)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := e.missing(t)
		if !r.Refresh.Running {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatal("the refresh did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Opening the page reads the cache only: IGDB is asked only by the refresh.
func TestSeriesMissingComesFromTheCache(t *testing.T) {
	e := newSeriesEnv(t, true)
	r := e.missing(t)
	if !r.Configured || len(r.Series) != 0 || e.fake.reads != 0 {
		t.Fatalf("before a refresh: %+v reads %d", r, e.fake.reads)
	}
	r = e.refresh(t)
	if r.Refresh.Failed || r.Refresh.Finished == "" || !strings.Contains(r.Refresh.Report, "1 collections read") {
		t.Errorf("status: %+v", r.Refresh)
	}
	if len(r.Series) != 1 || r.Series[0].Key != "zeds" || len(r.Series[0].Missing) != 2 {
		t.Fatalf("series: %+v", r.Series)
	}
	if len(r.OwnedRoots) != 1 || r.OwnedRoots[0] != 90 { // the browser applies the family rule to games added later
		t.Errorf("owned roots: %v", r.OwnedRoots)
	}
	m := r.Series[0].Missing[0]
	if m.IGDBID != 101 || m.Title != "Zed II" || m.Cover == nil || *m.Cover != "i101.jpg" || m.V != "b" || m.CollectionID != 7 {
		t.Errorf("missing[0] = %+v", m)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "i101.jpg")); err != nil {
		t.Error("the poster was not cached")
	}
	reads := e.fake.reads
	e.missing(t)
	e.missing(t)
	if e.fake.reads != reads {
		t.Error("a page load read IGDB")
	}
}

func TestSeriesHideAndUnhide(t *testing.T) {
	e := newSeriesEnv(t, true)
	e.refresh(t)
	e.req(t, "POST", "/api/series/hide", `{"collection_id":7,"igdb_id":101}`, 200)
	r := e.missing(t)
	if len(r.Series[0].Missing) != 1 || len(r.Series[0].Hidden) != 1 || r.Series[0].Hidden[0].IGDBID != 101 {
		t.Errorf("after hide: %+v", r.Series[0])
	}
	e.req(t, "POST", "/api/series/hide", `{"collection_id":7,"igdb_id":999}`, 404)
	e.req(t, "POST", "/api/series/hide", `{"collection_id":0,"igdb_id":101}`, 400)
	e.req(t, "POST", "/api/series/hide", `nope`, 400)
	e.req(t, "POST", "/api/series/unhide", `{"collection_id":7,"igdb_id":101}`, 200)
	r = e.missing(t)
	if len(r.Series[0].Missing) != 2 || len(r.Series[0].Hidden) != 0 {
		t.Errorf("after unhide: %+v", r.Series[0])
	}
}

// Adding the game (any way: it gets that igdb_id) makes it vanish from the list, with no refresh.
func TestSeriesAddedGameDisappears(t *testing.T) {
	e := newSeriesEnv(t, true)
	e.refresh(t)
	sid, _ := db.GetOrCreate(e.db, "series", "Zeds")
	e.db.Exec(`INSERT INTO game(title, title_sort, status_id, series_id, igdb_id) VALUES ('Zed II','zed ii',7,?,101)`, sid)
	r := e.missing(t)
	if len(r.Series[0].Missing) != 1 || r.Series[0].Missing[0].IGDBID != 102 {
		t.Errorf("missing: %+v", r.Series[0].Missing)
	}
}

func TestSeriesRefreshNeedsIGDB(t *testing.T) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	defer d.Close()
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Meta: &fakeMeta{igdb: false}}).Handler())
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/series/refresh", "application/json", nil)
	if err != nil || res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %v, %v", res, err)
	}
	res, _ = http.Get(srv.URL + "/api/series/missing")
	var r missingResp
	json.NewDecoder(res.Body).Decode(&r)
	if r.Configured || r.Series == nil {
		t.Errorf("unconfigured answer: %+v", r)
	}
}

func TestIGDBGameCandidate(t *testing.T) {
	e := newSeriesEnv(t, true)
	var c meta.Candidate
	if err := json.Unmarshal(e.req(t, "GET", "/api/igdb/101", "", 200), &c); err != nil || c.Name != "Zed II" || c.Year != 2005 {
		t.Errorf("candidate %+v, %v", c, err)
	}
	e.req(t, "GET", "/api/igdb/555", "", 404)
	e.req(t, "GET", "/api/igdb/abc", "", 400)
	off := newSeriesEnv(t, false)
	off.req(t, "GET", "/api/igdb/101", "", 503)
}

// Adding a game from the shelf stores the root of its version chain, so its original stops being listed at once.
func TestAddStoresTheVersionRoot(t *testing.T) {
	e := newSeriesEnv(t, true)
	e.req(t, "POST", "/api/games", `{"title":"Zed II","status":"backlog","igdb_id":101,"igdb_parent_id":90}`, 201)
	var root int64
	e.db.QueryRow(`SELECT igdb_parent_id FROM game WHERE igdb_id = 101`).Scan(&root)
	if root != 90 {
		t.Errorf("stored root = %d", root)
	}
	e.req(t, "POST", "/api/games", `{"title":"Other","status":"backlog","igdb_id":102,"igdb_parent_id":-1}`, 400)
	e.req(t, "POST", "/api/games", `{"title":"Plain","status":"backlog","igdb_id":103}`, 201)
	var unknown *int64
	e.db.QueryRow(`SELECT igdb_parent_id FROM game WHERE igdb_id = 103`).Scan(&unknown)
	if unknown != nil { // not sent: still unknown, the next refresh reads it
		t.Errorf("root = %v", *unknown)
	}
}
