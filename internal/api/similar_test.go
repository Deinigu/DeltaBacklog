package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// simFake is an offline IGDB for the Similar page: Zed (100, finished) is similar to Zed II (101) and Zed III (102).
type simFake struct{ reads int }

func (f *simFake) GameLinks(context.Context, []int64) (map[int64]meta.GameLinks, error) {
	return map[int64]meta.GameLinks{}, nil
}

func (f *simFake) SimilarGames(_ context.Context, ids []int64) (map[int64][]meta.CollectionGame, error) {
	f.reads++
	return map[int64][]meta.CollectionGame{100: {
		{IGDBID: 101, Name: "Zed II", Year: 2005, CoverID: "b"},
		{IGDBID: 102, Name: "Zed III", Year: 2011, CoverID: "c"},
	}}, nil
}

type similarResp struct {
	Configured  bool `json:"configured"`
	Suggestions []struct {
		IGDBID  int64   `json:"igdb_id"`
		Title   string  `json:"title"`
		Cover   *string `json:"cover"`
		Sources []int64 `json:"sources"`
		Hidden  bool    `json:"hidden"`
	} `json:"suggestions"`
	OwnedRoots []int64      `json:"owned_roots"`
	FetchedAt  string       `json:"fetched_at"`
	Refresh    seriesStatus `json:"refresh"`
}

func (e *seriesEnv) similar(t *testing.T) similarResp {
	t.Helper()
	var r similarResp
	if err := json.Unmarshal(e.req(t, "GET", "/api/similar", "", 200), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// newSimilarEnv serves a library with one finished game (Zed, IGDB 100) and the fake IGDB.
func newSimilarEnv(t *testing.T, configured bool) (*seriesEnv, *simFake) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO game(title, title_sort, status_id, igdb_id) VALUES ('Zed','zed',7,100)`); err != nil {
		t.Fatal(err)
	}
	fake := &simFake{}
	dir := t.TempDir()
	s := &Server{DB: d, CoverDir: dir, Meta: &fakeMeta{igdb: configured},
		MissingPosterFetch: func(url, dir string, igdbID int64) (string, error) {
			name := "i" + strconv.FormatInt(igdbID, 10) + ".jpg"
			return name, os.WriteFile(filepath.Join(dir, name), []byte("jpeg"), 0o644)
		}}
	if configured { // an injected client skips the "IGDB is set up" check, as in the Series tests
		s.Similar = fake
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &seriesEnv{db: d, url: srv.URL, dir: dir}, fake
}

// Reading the page never asks IGDB; Refresh does, in the background, once at a time; Hide survives and is reversible.
func TestSimilarEndpoints(t *testing.T) {
	env, fake := newSimilarEnv(t, true)
	d := env.db
	r := env.similar(t)
	if !r.Configured || len(r.Suggestions) != 0 || fake.reads != 0 {
		t.Fatalf("before a refresh: %+v reads %d", r, fake.reads)
	}
	env.req(t, "POST", "/api/similar/refresh", "", 202)
	var done similarResp
	for i := 0; i < 100; i++ {
		done = env.similar(t)
		if !done.Refresh.Running && done.Refresh.Finished != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if done.Refresh.Failed || len(done.Suggestions) != 2 || done.FetchedAt == "" || fake.reads != 1 {
		t.Fatalf("after: %+v reads %d", done, fake.reads)
	}
	if done.Suggestions[0].Cover == nil || len(done.Suggestions[0].Sources) != 1 || done.Suggestions[0].Sources[0] != 100 {
		t.Errorf("suggestion: %+v", done.Suggestions[0])
	}
	env.req(t, "POST", "/api/similar/hide", `{"igdb_id":101}`, 200)
	env.req(t, "POST", "/api/similar/hide", `{"igdb_id":555}`, 404)
	env.req(t, "POST", "/api/similar/hide", `{"igdb_id":0}`, 400)
	h := env.similar(t)
	if !h.Suggestions[0].Hidden || h.Suggestions[1].Hidden {
		t.Errorf("hidden flags: %+v", h.Suggestions)
	}
	// hiding it here does not hide it on the Series page: series_hidden is untouched
	var n int
	d.QueryRow(`SELECT count(*) FROM series_hidden`).Scan(&n)
	if n != 0 {
		t.Errorf("series_hidden has %d rows", n)
	}
	env.req(t, "POST", "/api/similar/unhide", `{"igdb_id":101}`, 200)
	if env.similar(t).Suggestions[0].Hidden {
		t.Error("unhide did not work")
	}
}

// Without IGDB keys the refresh says so and reads nothing.
func TestSimilarRefreshNeedsIGDB(t *testing.T) {
	env, fake := newSimilarEnv(t, false)
	env.req(t, "POST", "/api/similar/refresh", "", 503)
	if fake.reads != 0 {
		t.Error("IGDB was read")
	}
}
