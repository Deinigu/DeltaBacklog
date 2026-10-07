package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func getWith(t *testing.T, url string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// the default transport would add gzip itself and hide what the server sent
	res, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestRespCacheBuildsOnceAndRebuildsAfterInvalidate(t *testing.T) {
	var c respCache
	n := 0
	build := func() (any, error) { n++; return map[string]int{"n": n}, nil }

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ { // a burst of first requests shares one build
		wg.Add(1)
		go func() { defer wg.Done(); c.get("games", build) }()
	}
	wg.Wait()
	if n != 1 {
		t.Fatalf("20 concurrent requests built %d times, want 1", n)
	}
	a, _ := c.get("games", build)
	c.invalidate()
	b, _ := c.get("games", build)
	if n != 2 || a.etag == b.etag {
		t.Errorf("after invalidate: builds=%d, etags %s vs %s", n, a.etag, b.etag)
	}
	if _, err := c.get("broken", func() (any, error) { return nil, errors.New("boom") }); err == nil {
		t.Error("a failing build must report its error")
	}
	if _, err := c.get("broken", build); err != nil || n != 3 {
		t.Errorf("a failed build must not be cached: %v builds=%d", err, n)
	}
}

func TestGamesResponseIsCachedCompressedAndValidated(t *testing.T) {
	e := newAddEnv(t)
	url := e.srv.URL + "/api/games"

	res, plain := getWith(t, url, nil)
	etag := res.Header.Get("ETag")
	if res.StatusCode != 200 || etag == "" || res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("plain: %d %v", res.StatusCode, res.Header)
	}
	if res.Header.Get("Content-Length") != strconv.Itoa(len(plain)) {
		t.Errorf("content-length %s for %d bytes", res.Header.Get("Content-Length"), len(plain))
	}

	// gzip when asked, identical content, same validator, and smaller
	res, zipped := getWith(t, url, map[string]string{"Accept-Encoding": "gzip, deflate"})
	if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("ETag") != etag || res.Header.Get("Vary") == "" {
		t.Fatalf("gzip response headers: %v", res.Header)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		t.Fatal(err)
	}
	un, _ := io.ReadAll(zr)
	if !bytes.Equal(un, plain) || len(zipped) >= len(plain) {
		t.Errorf("gzip body differs or is not smaller: %d vs %d", len(zipped), len(plain))
	}
	// gzip refused explicitly (q=0) means plain
	if r, _ := getWith(t, url, map[string]string{"Accept-Encoding": "gzip;q=0"}); r.Header.Get("Content-Encoding") != "" {
		t.Error("gzip;q=0 must not be answered with gzip")
	}

	// the browser's conditional request gets 304 and no body
	res, body := getWith(t, url, map[string]string{"If-None-Match": etag})
	if res.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Errorf("conditional request: %d with %d bytes", res.StatusCode, len(body))
	}
	if res, _ = getWith(t, url, map[string]string{"If-None-Match": `W/"something-else"`}); res.StatusCode != 200 {
		t.Errorf("a different validator must get the full response: %d", res.StatusCode)
	}
}

func TestEveryWriteRefreshesTheCachedList(t *testing.T) {
	e := newAddEnv(t)
	list := func() ([]Game, string) {
		res, b := getWith(t, e.srv.URL+"/api/games", nil)
		var g []Game
		json.Unmarshal(b, &g)
		return g, res.Header.Get("ETag")
	}
	games, tag0 := list()
	first := games[0]
	url := e.srv.URL + "/api/games/" + strconv.FormatInt(first.ID, 10)

	check := func(what string, wantNew bool) {
		t.Helper()
		_, tag := list()
		if wantNew && tag == tag0 {
			t.Errorf("%s: the cached list was not refreshed", what)
		}
		tag0 = tag
	}

	do(t, "PATCH", url, `{"notes":"changed"}`)
	check("PATCH", true)
	g, _ := list()
	for _, x := range g {
		if x.ID == first.ID && x.Notes != "changed" {
			t.Errorf("edit not visible in the list: %q", x.Notes)
		}
	}
	do(t, "POST", e.srv.URL+"/api/games/reorder", `[`+strconv.FormatInt(g[1].ID, 10)+`,`+strconv.FormatInt(g[0].ID, 10)+`]`)
	check("reorder", true)
	_, out := createGameReq(t, e, `{"title":"Brand New"}`)
	check("create", true)
	id := int64(out["game"].(map[string]any)["id"].(float64))
	do(t, "POST", e.srv.URL+"/api/games/"+strconv.FormatInt(id, 10)+"/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	check("set cover", true)
	do(t, "DELETE", e.srv.URL+"/api/games/"+strconv.FormatInt(id, 10), "")
	check("delete", true)
	// reads and view writes do not throw the games away
	_, a := list()
	_, b := list()
	do(t, "POST", e.srv.URL+"/api/views", `{"name":"v"}`)
	_, c := list()
	if a != b || b != c {
		t.Error("reads and view changes must keep the cached list")
	}
	// the facet counts follow too
	res, fb := getWith(t, e.srv.URL+"/api/facets", nil)
	if res.Header.Get("ETag") == "" {
		t.Error("facets need a validator")
	}
	var f1 map[string][]Facet
	json.Unmarshal(fb, &f1)
	do(t, "PATCH", url, `{"genres":["Zzz Unique Genre"]}`)
	_, fb2 := getWith(t, e.srv.URL+"/api/facets", nil)
	var f2 map[string][]Facet
	json.Unmarshal(fb2, &f2)
	found := false
	for _, x := range f2["genre"] {
		found = found || x.Key == "Zzz Unique Genre"
	}
	if !found {
		t.Error("the facets were not refreshed after an edit")
	}
}

func TestLoadGameMatchesTheFullList(t *testing.T) {
	e := newAddEnv(t)
	_, b := getWith(t, e.srv.URL+"/api/games", nil)
	var all []Game
	json.Unmarshal(b, &all)
	for _, want := range all {
		do(t, "PATCH", e.srv.URL+"/api/games/"+strconv.FormatInt(want.ID, 10), `{"years":[2020,2021],"periods":["always"],"genres":["RPG","Indie"],"players":["Solo"]}`)
	}
	_, b = getWith(t, e.srv.URL+"/api/games", nil)
	json.Unmarshal(b, &all)
	for _, want := range all {
		_, one := getWith(t, e.srv.URL+"/api/games", nil) // list, for the comparison below
		_ = one
		res, body := do(t, "PATCH", e.srv.URL+"/api/games/"+strconv.FormatInt(want.ID, 10), `{}`) // answers LoadGame(id)
		var got Game
		json.Unmarshal(body, &got)
		a, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if res.StatusCode != 200 || !bytes.Equal(a, w) {
			t.Errorf("single-game load differs from the list for %q:\n got  %s\n want %s", want.Title, a, w)
		}
	}
}

func TestCacheWarmsItselfAfterAWriteBurst(t *testing.T) {
	e := newAddEnv(t)
	// newAddEnv built its own server; make another around the same database to control the warm delay
	s := &Server{DB: e.db, CoverDir: e.coverDir, Meta: e.meta}
	s.cache.warmDelay = 20 * time.Millisecond
	s.Warm()
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for i := 0; i < 200; i++ {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", what)
	}
	built := func() int { s.cache.mu.Lock(); defer s.cache.mu.Unlock(); return s.cache.builds }
	waitFor("the initial warm-up", func() bool { return built() == 2 }) // games + facets
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// a burst of writes causes one rebuild, in the background, without any read in between
	for i := 0; i < 5; i++ {
		do(t, "PATCH", srv.URL+"/api/games/1", `{"notes":"burst `+strconv.Itoa(i)+`"}`)
	}
	waitFor("the rebuild after the burst", func() bool { return built() == 4 })
	time.Sleep(100 * time.Millisecond)
	if built() != 4 {
		t.Errorf("a burst of 5 writes caused %d builds, want exactly one rebuild (2 entries)", built()-2)
	}
	// and the data it warmed is the fresh data
	_, body := getWith(t, srv.URL+"/api/games", nil)
	if !bytes.Contains(body, []byte("burst 4")) {
		t.Error("the warmed list does not contain the last write")
	}
}
