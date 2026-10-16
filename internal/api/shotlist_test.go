package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type shotPage struct {
	Screenshots []GalleryShot `json:"screenshots"`
	Total       int           `json:"total"`
	Years       []int         `json:"years"`
	Available   bool          `json:"available"`
	Error       string        `json:"error"`
}

func (e shotEnv) listAll(t *testing.T, query string) (int, shotPage) {
	t.Helper()
	res, err := http.Get(e.srv.URL + "/api/screenshots" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var p shotPage
	json.NewDecoder(res.Body).Decode(&p)
	return res.StatusCode, p
}

func TestListAllScreenshots(t *testing.T) {
	e := newShotEnv(t, nil)
	if c, p := e.listAll(t, ""); c != 200 || p.Total != 0 || p.Screenshots == nil || len(p.Screenshots) != 0 || p.Years == nil {
		t.Fatalf("empty library: %d %+v", c, p)
	}
	img := pngBytes(t, 40, 30)
	e.upload(t, "1", upFile{"a.png", img, "1577880000000", nil}, upFile{"b.png", img, "1704110400000", nil}) // 2020-01-01, 2024-01-01
	e.upload(t, "2", upFile{"c.png", img, "1672574400000", nil})                                             // 2023-01-01
	if c, _ := e.upload(t, "1", upFile{"d.png", img, "", nil}); c != 200 {                                   // no date: added now
		t.Fatal(c)
	}

	c, p := e.listAll(t, "")
	if c != 200 || p.Total != 4 || len(p.Screenshots) != 4 || !p.Available {
		t.Fatalf("%d %+v", c, p)
	}
	if first := p.Screenshots[0]; first.Taken != "" || first.GameTitle == "" || first.Date != first.Added {
		t.Errorf("an undated picture is filed under its upload time and comes first: %+v", first)
	}
	if got := p.Screenshots[1].Taken[:4] + p.Screenshots[2].Taken[:4] + p.Screenshots[3].Taken[:4]; got != "202420232020" {
		t.Errorf("newest first: %s", got)
	}
	if s := p.Screenshots[1]; s.Thumb == "" || s.Src == "" || s.GameID != 1 {
		t.Errorf("urls and game: %+v", s)
	}
	if len(p.Years) != 4 || p.Years[0] < 2024 || p.Years[3] != 2020 {
		t.Errorf("years: %v", p.Years)
	}

	// by game: titles ascending, each game's pictures in its own order
	_, p = e.listAll(t, "?sort=game")
	if p.Screenshots[0].GameTitle > p.Screenshots[3].GameTitle || p.Screenshots[0].GameID != p.Screenshots[1].GameID {
		t.Errorf("grouped by game: %+v", p.Screenshots)
	}

	// pages: total stays the number of matches
	_, p = e.listAll(t, "?limit=2&offset=3")
	if p.Total != 4 || len(p.Screenshots) != 1 || p.Years != nil {
		t.Errorf("page: %d of %d", len(p.Screenshots), p.Total)
	}
	_, p = e.listAll(t, "?limit=1")
	if p.Total != 4 || len(p.Screenshots) != 1 {
		t.Errorf("limit: %d of %d", len(p.Screenshots), p.Total)
	}

	// filters
	_, p = e.listAll(t, "?year=2023")
	if p.Total != 1 || p.Screenshots[0].GameID != 2 {
		t.Errorf("year: %+v", p)
	}
	_, p = e.listAll(t, "?game_id=1")
	if p.Total != 3 {
		t.Errorf("game_id: %d", p.Total)
	}
	first := p.Screenshots[0].ID
	_, p = e.listAll(t, "?q=hades")
	if p.Total != 3 {
		t.Errorf("q by title: %d", p.Total)
	}
	_, p = e.listAll(t, "?q=%25")
	if p.Total != 0 {
		t.Errorf("a percent sign is a literal: %d", p.Total)
	}
	req, _ := http.NewRequest("PATCH", e.srv.URL+"/api/screenshots/"+itoa(first), strings.NewReader(`{"caption":"Boss fight C:\\save"}`))
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 200 {
		t.Fatal("caption", err)
	}
	if _, p = e.listAll(t, "?q=boss"); p.Total != 1 || p.Screenshots[0].Caption != `Boss fight C:\save` {
		t.Errorf("q by caption: %+v", p)
	}
	for q, want := range map[string]int{"%5C": 1, "%5Csave": 1, "C:%5Cs": 1, "a%5Cb": 0, "%5C%25": 0} { // a backslash is a literal too
		if _, p = e.listAll(t, "?q="+q); p.Total != want {
			t.Errorf("q=%s: %d, want %d", q, p.Total, want)
		}
	}
}

func TestListAllScreenshotsBadQuery(t *testing.T) {
	e := newShotEnv(t, nil)
	for _, q := range []string{"?limit=0", "?limit=x", "?offset=-1", "?sort=oldest", "?year=20x", "?year=99", "?game_id=0"} {
		if c, p := e.listAll(t, q); c != 400 || p.Error == "" {
			t.Errorf("%s: %d %q", q, c, p.Error)
		}
	}
}

func TestListAllScreenshotsWithoutStorage(t *testing.T) {
	e := newShotEnv(t, func(s *Server) { s.ShotDir = "" })
	if c, p := e.listAll(t, ""); c != 200 || p.Available {
		t.Errorf("%d available=%v", c, p.Available)
	}
}
