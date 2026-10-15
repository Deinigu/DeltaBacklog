package meta

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func fake(t *testing.T) (*Client, *int32) {
	t.Helper()
	var tokenCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if r.URL.Query().Get("client_id") != "cid" || r.URL.Query().Get("client_secret") != "sec" {
			http.Error(w, "bad creds", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	})
	mux.HandleFunc("/igdb/games", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Client-ID") != "cid" {
			http.Error(w, "unauthorized", 401)
			return
		}
		body := make([]byte, 2000)
		n, _ := r.Body.Read(body)
		if strings.Contains(string(body[:n]), "where id = 1;") || strings.Contains(string(body[:n]), "where id = 2;") { // the images query
			w.Write([]byte(`[{"id":1,"name":"Hades","cover":{"image_id":"cover1"},
			 "artworks":[{"image_id":"art1","width":1920,"height":620},{"image_id":"art2"}],
			 "screenshots":[{"image_id":"s1"},{"image_id":"s2"},{"image_id":"s3"},{"image_id":"s4"},{"image_id":"s5"},{"image_id":"s6"},{"image_id":"s7"},{"image_id":"s8"},{"image_id":"s9"},{"image_id":"s10"}]}]`))
			return
		}
		if strings.Contains(string(body[:n]), "where id = 404;") {
			w.Write([]byte("[]"))
			return
		}
		if strings.HasPrefix(string(body[:n]), "where name ~ ") { // the exact-name query of every search
			w.Write([]byte("[]"))
			return
		}
		if !strings.Contains(string(body[:n]), `search "Hades \"II\""`) {
			http.Error(w, "unexpected query: "+string(body[:n]), 400)
			return
		}
		w.Write([]byte(`[
		 {"id":1,"name":"Hades","first_release_date":1600300800,"genres":[{"name":"Role-playing (RPG)"},{"name":"Indie"}],"themes":[{"name":"Action"}],
		  "platforms":[{"abbreviation":"PC"},{"name":"Nintendo Switch"}],
		  "involved_companies":[{"developer":false,"company":{"name":"Pub"}},{"developer":true,"company":{"name":"Supergiant Games"}}],
		  "collections":[{"name":"Hades"}],"cover":{"image_id":"co1abc"}},
		 {"id":2,"name":"Hades II","franchises":[{"name":"Franchise X"}]}]`))
	})
	mux.HandleFunc("/sgdb/search/autocomplete/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk" {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Write([]byte(`{"data":[{"id":10,"name":"Hades Other"},{"id":11,"name":"Hades"}]}`))
	})
	mux.HandleFunc("/sgdb/grids/game/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/11") {
			http.Error(w, "wrong game id (exact match should win)", 400)
			return
		}
		if r.URL.Query().Get("dimensions") == "" { // fallback request: any shape
			w.Write([]byte(`{"data":[{"url":"https://cdn2.steamgriddb.com/any.png","thumb":"t","width":600,"height":900}]}`))
			return
		}
		if strings.Contains(r.URL.RawQuery, "force-empty") {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Write([]byte(`{"data":[{"url":"https://cdn2.steamgriddb.com/w.png","thumb":"https://cdn2.steamgriddb.com/w_t.png","width":460,"height":215}]}`))
	})
	mux.HandleFunc("/steam", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("appids") == "1145350" {
			w.Write([]byte(`{"1145350":{"success":true,"data":{"name":"Hades II"}}}`))
			return
		}
		w.Write([]byte(`{"999":{"success":false}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec", SteamGridDBKey: "sk"})
	c.TokenURL, c.IGDBURL, c.SGDBURL, c.SteamURL = srv.URL+"/token", srv.URL+"/igdb", srv.URL+"/sgdb", srv.URL+"/steam"
	return c, &tokenCalls
}

func TestSearchIGDB(t *testing.T) {
	c, tokens := fake(t)
	got, err := c.SearchIGDB(context.Background(), `Hades "II"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates = %+v", got)
	}
	h := got[1] // `Hades "II"` names Hades II exactly, so it ranks first
	if h.Name != "Hades" || h.Year != 2020 || h.Developer != "Supergiant Games" || h.Series != "Hades" ||
		strings.Join(h.Genres, ",") != "Role-playing (RPG),Indie" || strings.Join(h.Platforms, ",") != "PC,Nintendo Switch" ||
		h.CoverURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1abc.jpg" {
		t.Errorf("first candidate = %+v", h)
	}
	if got[0].Series != "Franchise X" || got[0].Year != 0 || got[0].Genres == nil {
		t.Errorf("second candidate = %+v", got[0])
	}
	c.SearchIGDB(context.Background(), `Hades "II"`)
	if n := atomic.LoadInt32(tokens); n != 1 {
		t.Errorf("token fetched %d times, want 1 (cached)", n)
	}
}

func TestNotConfigured(t *testing.T) {
	c := New(Config{})
	if _, err := c.SearchIGDB(context.Background(), "x"); err != ErrNotConfigured {
		t.Errorf("igdb err = %v", err)
	}
	if _, err := c.SearchCovers(context.Background(), "x"); err != ErrNotConfigured {
		t.Errorf("sgdb err = %v", err)
	}
	if i, s := c.Configured(); i || s {
		t.Error("should report nothing configured")
	}
}

func TestSearchCovers(t *testing.T) {
	c, _ := fake(t)
	got, err := c.SearchCovers(context.Background(), "hades")
	if err != nil || len(got) != 1 || got[0].Width != 460 || got[0].Source != "steamgriddb" {
		t.Errorf("covers = %+v err=%v", got, err)
	}
}

func TestResolveLink(t *testing.T) {
	c, _ := fake(t)
	ctx := context.Background()
	if got, err := c.ResolveLink(ctx, "https://store.steampowered.com/app/1145350/Hades_II/"); err != nil || got != "Hades II" {
		t.Errorf("steam = %q %v", got, err)
	}
	if _, err := c.ResolveLink(ctx, "https://store.steampowered.com/app/999/X/"); err == nil {
		t.Error("unknown steam app should fail")
	}
	if got, err := c.ResolveLink(ctx, "https://www.igdb.com/games/hades-ii"); err != nil || got != "hades ii" {
		t.Errorf("igdb = %q %v", got, err)
	}
	for _, bad := range []string{"https://evil.example.com/app/1", "http://169.254.169.254/latest", "ftp://store.steampowered.com/app/1", "https://notsteampowered.com/app/1", "https://store.steampowered.com.evil.com/app/1"} {
		if _, err := c.ResolveLink(ctx, bad); err == nil {
			t.Errorf("%s must be rejected", bad)
		}
	}
}

func TestPageTitleCleanup(t *testing.T) {
	for in, want := range map[string]string{
		"Hades II on Steam":          "Hades II",
		"Cyberpunk 2077 - GOG.COM":   "Cyberpunk 2077",
		"Celeste | Epic Games Store": "Celeste",
		"Plain Title":                "Plain Title",
	} {
		if got := strings.TrimSpace(suffixes.ReplaceAllString(in, "")); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if !LooksLikeLink(" https://x.y ") || LooksLikeLink("Hades II") {
		t.Error("LooksLikeLink")
	}
}

func TestRankByTitle(t *testing.T) {
	got := RankByTitle([]Candidate{{Name: "Hades II"}, {Name: "hades"}, {Name: "Hades Demo"}}, "Hades")
	if got[0].Name != "hades" || got[1].Name != "Hades II" {
		t.Errorf("rank = %+v", got)
	}
}

// TestLive runs against the real services when DELTABACKLOG_LIVE=1 and the keys are in the environment.
func TestLive(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 with IGDB_CLIENT_ID, IGDB_CLIENT_SECRET, STEAMGRIDDB_API_KEY to run")
	}
	c := New(Config{os.Getenv("IGDB_CLIENT_ID"), os.Getenv("IGDB_CLIENT_SECRET"), os.Getenv("STEAMGRIDDB_API_KEY")})
	ctx := context.Background()
	cands, err := c.SearchIGDB(ctx, "Hades II")
	if err != nil || len(cands) == 0 {
		t.Fatalf("igdb: %v %v", cands, err)
	}
	for _, x := range cands[:min(3, len(cands))] {
		t.Logf("IGDB: %+v", x)
	}
	covers, err := c.SearchCovers(ctx, "Hades II")
	if err != nil || len(covers) == 0 {
		t.Fatalf("sgdb: %v %v", covers, err)
	}
	t.Logf("SGDB: %d covers, first %+v", len(covers), covers[0])
	imgs, err := c.IGDBImages(ctx, 0, "Hades II")
	if err != nil || len(imgs) == 0 {
		t.Fatalf("igdb images: %v %v", imgs, err)
	}
	kinds := map[string]int{}
	for _, o := range imgs {
		kinds[o.Kind]++
	}
	t.Logf("IGDB images: %d %v, first %s", len(imgs), kinds, imgs[0].URL)
	title, err := c.ResolveLink(ctx, "https://store.steampowered.com/app/1145350/Hades_II/")
	t.Logf("steam link -> %q %v", title, err)
}

func TestIGDBImages(t *testing.T) {
	c, _ := fake(t)
	got, err := c.IGDBImages(context.Background(), 1, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	// 2 artworks, then screenshots capped at 8, then the portrait cover last
	if len(got) != 2+8+1 {
		t.Fatalf("got %d options: %+v", len(got), got)
	}
	if got[0].Kind != "Artwork" || got[0].URL != "https://images.igdb.com/igdb/image/upload/t_1080p/art1.jpg" ||
		got[0].Thumb != "https://images.igdb.com/igdb/image/upload/t_screenshot_med/art1.jpg" || got[0].Width != 1920 || got[0].Source != "igdb" {
		t.Errorf("first = %+v", got[0])
	}
	if got[0].Width != 1920 || got[0].Height != 620 {
		t.Errorf("an artwork keeps the shape IGDB reports: %dx%d", got[0].Width, got[0].Height)
	}
	if got[1].Width != 1920 || got[1].Height != 1080 {
		t.Errorf("a missing size falls back to widescreen: %dx%d", got[1].Width, got[1].Height)
	}
	if got[2].Kind != "Screenshot" || got[9].Kind != "Screenshot" {
		t.Errorf("screenshots: %+v %+v", got[2], got[9])
	}
	last := got[len(got)-1]
	if last.Kind != "Cover" || last.URL != "https://images.igdb.com/igdb/image/upload/t_cover_big/cover1.jpg" || last.Height != 374 {
		t.Errorf("cover = %+v", last)
	}

	// no id: the best title match is used
	byTitle, err := c.IGDBImages(context.Background(), 0, `Hades "II"`)
	if err != nil || len(byTitle) != 11 {
		t.Errorf("by title: %d %v", len(byTitle), err)
	}
	// an unknown id gives an empty list, not an error
	none, err := c.IGDBImages(context.Background(), 404, "")
	if err != nil || len(none) != 0 || none == nil {
		t.Errorf("unknown id: %v %v", none, err)
	}
	if _, err := New(Config{}).IGDBImages(context.Background(), 1, ""); err != ErrNotConfigured {
		t.Errorf("unconfigured: %v", err)
	}
}

func TestLinkClientChecksEveryRedirect(t *testing.T) {
	c := New(Config{})
	cl := c.linkClient()
	req := func(u string) *http.Request { r, _ := http.NewRequest("GET", u, nil); return r }
	if err := cl.CheckRedirect(req("https://www.gog.com/game/x"), nil); err != nil {
		t.Errorf("an allowed host should be followed: %v", err)
	}
	for _, bad := range []string{"http://192.168.1.10:8080/", "http://169.254.169.254/latest/meta-data", "https://evil.example.com/", "https://gog.com.evil.example.com/"} {
		if err := cl.CheckRedirect(req(bad), nil); err == nil {
			t.Errorf("redirect to %s must be refused", bad)
		}
	}
	via := make([]*http.Request, 4)
	if err := cl.CheckRedirect(req("https://www.gog.com/x"), via); err == nil {
		t.Error("a redirect loop must stop")
	}
}

func TestRateLimitIsATypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	_, err := c.SearchIGDB(context.Background(), "Hades")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 429 {
		t.Fatalf("err = %v, want *HTTPError with status 429", err)
	}
}

func TestSearchFallsBackWhenTheTitleSearchFindsNothing(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		body := make([]byte, 2000)
		n, _ := r.Body.Read(body)
		q := string(body[:n])
		queries = append(queries, q[:strings.Index(q, ";")])
		switch {
		case strings.HasPrefix(q, `where alternative_names.name ~ *"Profesor Layton y el Futuro Perdido"*`):
			w.Write([]byte(`[{"id":9,"name":"Professor Layton and the Lost Future","alternative_names":[{"name":"Profesor Layton y el Futuro Perdido"}]}]`))
		case strings.HasPrefix(q, `search "Little Big`): // only the spaceless spelling is known
			w.Write([]byte(`[]`))
		case strings.HasPrefix(q, `search "LittleBigPlanet2"`):
			w.Write([]byte(`[{"id":5,"name":"LittleBigPlanet 2"}]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"

	got, err := c.SearchIGDB(context.Background(), "Profesor Layton y el Futuro Perdido")
	if err != nil || len(got) != 1 || got[0].IGDBID != 9 || got[0].AltNames[0] != "Profesor Layton y el Futuro Perdido" {
		t.Fatalf("alternative-name fallback: %+v %v", got, err)
	}
	if len(queries) != 3 || !strings.HasPrefix(queries[0], "search ") || !strings.HasPrefix(queries[1], "where name ~ ") {
		t.Errorf("queries = %q", queries)
	}
	got, err = c.SearchIGDB(context.Background(), "Little Big Planet 2")
	if err != nil || len(got) != 1 || got[0].IGDBID != 5 {
		t.Fatalf("spaceless fallback: %+v %v", got, err)
	}
	if got, _ := c.SearchIGDB(context.Background(), "Nothing Like This: At All"); got == nil || len(got) != 0 {
		t.Errorf("nothing found must be an empty list, got %#v", got)
	}
}

func TestPosterSources(t *testing.T) {
	c, _ := fake(t)
	ctx := context.Background()
	u, err := c.IGDBCoverURL(ctx, 1)
	if err != nil || u != "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/cover1.jpg" {
		t.Errorf("IGDBCoverURL = %q, %v", u, err)
	}
	if u, err := c.IGDBCoverURL(ctx, 404); err != nil || u != "" {
		t.Errorf("a game IGDB does not know: %q, %v", u, err)
	}
	u, err = c.SteamGridPortrait(ctx, "Hades") // the exact title match (game 11) wins over the first hit
	if err != nil || u != "https://cdn2.steamgriddb.com/w.png" {
		t.Errorf("SteamGridPortrait = %q, %v", u, err)
	}
	if u, err := c.SteamGridPortrait(ctx, "  "); err != nil || u != "" {
		t.Errorf("blank title: %q, %v", u, err)
	}
	off := New(Config{})
	if _, err := off.IGDBCoverURL(ctx, 1); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured IGDB: %v", err)
	}
	if _, err := off.SteamGridPortrait(ctx, "Hades"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured SteamGridDB: %v", err)
	}
}
