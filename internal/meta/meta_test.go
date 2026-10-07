package meta

import (
	"context"
	"encoding/json"
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
	h := got[0]
	if h.Name != "Hades" || h.Year != 2020 || h.Developer != "Supergiant Games" || h.Series != "Hades" ||
		strings.Join(h.Genres, ",") != "Role-playing (RPG),Indie" || strings.Join(h.Platforms, ",") != "PC,Nintendo Switch" ||
		h.CoverURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1abc.jpg" {
		t.Errorf("first candidate = %+v", h)
	}
	if got[1].Series != "Franchise X" || got[1].Year != 0 || got[1].Genres == nil {
		t.Errorf("second candidate = %+v", got[1])
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
	title, err := c.ResolveLink(ctx, "https://store.steampowered.com/app/1145350/Hades_II/")
	t.Logf("steam link -> %q %v", title, err)
}
