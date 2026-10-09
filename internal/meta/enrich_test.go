package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func enrichFake(t *testing.T, queries *[]string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	})
	mux.HandleFunc("/igdb/games", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*queries = append(*queries, string(b))
		w.Write([]byte(`[{"id":1,"first_release_date":1600300800,"genres":[{"name":"Indie"}],"themes":[{"name":"Action"}],
		  "platforms":[{"abbreviation":"PC"}],
		  "involved_companies":[{"developer":true,"company":{"name":"Supergiant Games"}}],
		  "collections":[{"name":"Hades"}],
		  "game_modes":[{"name":"Single player"}],"player_perspectives":[{"name":"Bird view / Isometric"}],
		  "websites":[
		    {"url":"https://twitter.com/supergiant","type":5},
		    {"url":"https://store.steampowered.com/app/1145360","type":13},
		    {"url":"https://www.gog.com/game/hades","type":17},
		    {"url":"https://store.epicgames.com/p/hades","type":16},
		    {"url":"https://www.supergiantgames.com/games/hades/","type":1},
		    {"url":"https://store.steampowered.com/app/9","type":13},
		    {"url":"javascript:alert(1)","type":1}]}]`))
	})
	mux.HandleFunc("/igdb/game_time_to_beats", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*queries = append(*queries, string(b))
		w.Write([]byte(`[{"game_id":1,"hastily":36000,"normally":72000,"completely":0}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	return c
}

func TestGameDetailsAndTimeToBeat(t *testing.T) {
	var queries []string
	c := enrichFake(t, &queries)
	got, err := c.GameDetails(context.Background(), []int64{1, 2})
	if err != nil || len(got) != 1 {
		t.Fatalf("details = %+v, %v", got, err)
	}
	d := got[0]
	if d.IGDBID != 1 || d.Year != 2020 || d.Developer != "Supergiant Games" || d.Series != "Hades" || len(d.Platforms) != 1 {
		t.Errorf("facts: %+v", d)
	}
	if len(d.Modes) != 1 || d.Modes[0] != "Single player" || len(d.Perspectives) != 1 {
		t.Errorf("modes/perspectives: %+v", d)
	}
	want := map[string]string{"steam": "https://store.steampowered.com/app/1145360", "gog": "https://www.gog.com/game/hades",
		"epic": "https://store.epicgames.com/p/hades", "official": "https://www.supergiantgames.com/games/hades/"}
	if len(d.Links) != len(want) {
		t.Fatalf("links = %+v", d.Links)
	}
	for _, l := range d.Links {
		if want[l.Kind] != l.URL {
			t.Errorf("link %s = %s", l.Kind, l.URL)
		}
	}

	ttb, err := c.TimeToBeat(context.Background(), []int64{1, 2})
	if err != nil || ttb[1].Hastily != 36000 || ttb[1].Normally != 72000 || ttb[1].Completely != 0 || len(ttb) != 1 {
		t.Errorf("ttb = %+v, %v", ttb, err)
	}

	// Owner decision: no IGDB scores anywhere, so the app must never ask for them.
	for _, q := range queries {
		for _, banned := range []string{"rating", "hypes", "follows", "score"} {
			if strings.Contains(strings.ToLower(q), banned) {
				t.Errorf("query asks for %q: %s", banned, q)
			}
		}
		if !strings.Contains(q, "(1,2)") {
			t.Errorf("query does not batch the ids: %s", q)
		}
	}
}

// TestLiveEnrich checks the enrichment queries against the real IGDB (DELTABACKLOG_LIVE=1 plus the keys).
func TestLiveEnrich(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 with IGDB_CLIENT_ID and IGDB_CLIENT_SECRET to run")
	}
	c := New(Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
	ctx := context.Background()
	cands, err := c.SearchIGDB(ctx, "Hades II")
	if err != nil || len(cands) == 0 {
		t.Fatalf("search: %v %v", cands, err)
	}
	other, err := c.SearchIGDB(ctx, "The Witcher 3")
	if err != nil || len(other) == 0 {
		t.Fatalf("search: %v %v", other, err)
	}
	ids := []int64{cands[0].IGDBID, other[0].IGDBID}
	details, err := c.GameDetails(ctx, ids)
	if err != nil || len(details) != 2 {
		t.Fatalf("details: %+v %v", details, err)
	}
	for _, d := range details {
		t.Logf("%+v", d)
	}
	ttb, err := c.TimeToBeat(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ttb: %+v", ttb)
}
