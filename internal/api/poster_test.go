package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// newPosterGame adds a game with a wide cover and returns its URL under /api/games.
func newPosterGame(t *testing.T, e *addEnv) (string, int64) {
	t.Helper()
	res, body := do(t, "POST", e.srv.URL+"/api/games", `{"title":"Poster Test","cover_url":"`+e.images.URL+`/ok.png"}`)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var out struct {
		Game Game `json:"game"`
	}
	json.Unmarshal(body, &out)
	return e.srv.URL + "/api/games/" + strconv.FormatInt(out.Game.ID, 10), out.Game.ID
}

func posterRow(t *testing.T, e *addEnv, id int64) (file, src string, user int, v string) {
	t.Helper()
	var f, s *string
	if err := e.db.QueryRow(`SELECT poster_file, poster_src, poster_user, poster_updated_at FROM game WHERE id = ?`, id).Scan(&f, &s, &user, &v); err != nil {
		t.Fatal(err)
	}
	if f != nil {
		file = *f
	}
	if s != nil {
		src = *s
	}
	return
}

func TestSetPoster(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)

	for _, body := range []string{
		`{"url":"https://evil.example.com/x.png"}`, // host not allowed
		`{"url":"not a url"}`,
		`{}`, // nothing asked
		`{"url":"` + e.images.URL + `/portrait.png","auto":true}`, // two things asked
		`{broken`,
	} {
		if res, b := do(t, "POST", url+"/poster", body); res.StatusCode != 400 {
			t.Errorf("%s = %d %s, want 400", body, res.StatusCode, b)
		}
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a refused request made a poster: %q", file)
	}
	if res, _ := do(t, "POST", e.srv.URL+"/api/games/9999/poster", `{"auto":true}`); res.StatusCode != 404 {
		t.Errorf("missing game = %d", res.StatusCode)
	}
	for _, bad := range []string{"/boom.png", "/missing.png", "/redirect.png", "/huge.png"} {
		if res, _ := do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+bad+`"}`); res.StatusCode != 502 {
			t.Errorf("%s = %d, want 502", bad, res.StatusCode)
		}
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a failed download made a poster: %q", file)
	}

	res, body := do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	var g Game
	json.Unmarshal(body, &g)
	if res.StatusCode != 200 || g.Poster == nil || g.PosterV == "" {
		t.Fatalf("set poster: %d %s", res.StatusCode, body)
	}
	file, src, user, v1 := posterRow(t, e, id)
	if file != *g.Poster || user != 1 || src != "sgdb" {
		t.Errorf("row = %q %q user=%d", file, src, user)
	}
	if _, err := os.Stat(filepath.Join(e.coverDir, file)); err != nil {
		t.Error(err)
	}
	// the game list the browser reads carries the new image at once
	_, list := do(t, "GET", e.srv.URL+"/api/games", "")
	if !bytes.Contains(list, []byte(`"poster_v":"`+v1+`"`)) {
		t.Error("the cached game list does not show the new poster key")
	}
	// a second choice within the same second still gets a new cache key
	_, body = do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	json.Unmarshal(body, &g)
	if g.PosterV == v1 {
		t.Errorf("poster_v %q did not change", v1)
	}
}

func TestUserPosterSurvives(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	e.meta.portrait = e.images.URL + "/portrait.png"

	do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	file, _, _, v := posterRow(t, e, id)

	// the background queue only makes missing posters, and never touches the user's
	q := (&Server{DB: e.db, CoverDir: e.coverDir}).posterQ()
	q.Add(id)
	if _, err := q.M.Make(nil, id); !errors.Is(err, posters.ErrUserPoster) {
		t.Errorf("Make on a user poster = %v", err)
	}
	// a new wide cover leaves the poster alone
	if res, _ := do(t, "POST", url+"/cover", `{"url":"`+e.images.URL+`/ok.png"}`); res.StatusCode != 200 {
		t.Fatalf("cover = %d", res.StatusCode)
	}
	if f, _, user, v2 := posterRow(t, e, id); f != file || user != 1 || v2 != v {
		t.Errorf("after a cover change: %q user=%d v=%q (was %q %q)", f, user, v2, file, v)
	}

	// "automatic again" replaces it and gives it back to the queue's rules
	e.meta.portrait = e.images.URL + "/portrait.png"
	res, body := do(t, "POST", url+"/poster", `{"auto":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("auto = %d %s", res.StatusCode, body)
	}
	if _, src, user, _ := posterRow(t, e, id); user != 0 || src != "sgdb" {
		t.Errorf("after auto: src=%q user=%d", src, user)
	}

	// auto that finds nothing temporary keeps the poster
	do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	e.meta.posterErr = errors.New("network down")
	if res, _ := do(t, "POST", url+"/poster", `{"auto":true}`); res.StatusCode != 502 {
		t.Errorf("auto while the services are down = %d", res.StatusCode)
	}
	if _, _, user, _ := posterRow(t, e, id); user != 1 {
		t.Error("a failed automatic run dropped the user's poster")
	}
}

func TestPosterCrop(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	res, body := do(t, "POST", url+"/poster", `{"crop":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("crop = %d %s", res.StatusCode, body)
	}
	if file, src, user, _ := posterRow(t, e, id); file == "" || src != "crop" || user != 1 {
		t.Errorf("row = %q %q %d", file, src, user)
	}
	// a game without a wide cover cannot be cropped
	_, b := do(t, "POST", e.srv.URL+"/api/games", `{"title":"No cover"}`)
	var out struct {
		Game Game `json:"game"`
	}
	json.Unmarshal(b, &out)
	if res, _ := do(t, "POST", e.srv.URL+"/api/games/"+strconv.FormatInt(out.Game.ID, 10)+"/poster", `{"crop":true}`); res.StatusCode != 422 {
		t.Errorf("crop without cover = %d", res.StatusCode)
	}
}

func TestPosterOptions(t *testing.T) {
	e := newAddEnv(t)
	e.meta.posters = []meta.CoverOption{{URL: "u1", Thumb: "t1", Width: 600, Height: 900, Source: "steamgriddb"}, {URL: "u2", Thumb: "t2", Source: "steamgriddb"}}
	e.meta.igdbPosters = []meta.CoverOption{{URL: "i1", Thumb: "it1", Width: 528, Height: 748, Source: "igdb", Kind: "Cover"}}
	get := func(q string) (int, map[string]any) {
		res, b := do(t, "GET", e.srv.URL+"/api/covers/search?"+q, "")
		var m map[string]any
		json.Unmarshal(b, &m)
		return res.StatusCode, m
	}
	if code, m := get("q=x&shape=poster"); code != 200 || len(m["covers"].([]any)) != 2 {
		t.Errorf("sgdb posters: %d %v", code, m)
	}
	if code, m := get("q=x&shape=poster&source=igdb&igdb_id=5"); code != 200 || len(m["covers"].([]any)) != 1 {
		t.Errorf("igdb posters: %d %v", code, m)
	}
	e.meta.sgdbErr = errors.New("dial tcp: i/o timeout")
	if code, m := get("q=x&shape=poster"); code != 502 || m["unreachable"] != true {
		t.Errorf("unreachable: %d %v", code, m)
	}
	e.meta.sgdbErr = &meta.HTTPError{Status: 401, Text: "401 Unauthorized"}
	if code, m := get("q=x&shape=poster"); code != 502 || m["unreachable"] != false {
		t.Errorf("bad key: %d %v", code, m)
	}
	// the IGDB list does not wait for or depend on SteamGridDB
	if code, _ := get("q=x&shape=poster&source=igdb"); code != 200 {
		t.Errorf("igdb while sgdb is down: %d", code)
	}
}
