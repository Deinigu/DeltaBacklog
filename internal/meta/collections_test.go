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

func collectionFake(t *testing.T, queries *[]string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	})
	mux.HandleFunc("/igdb/games", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := string(b)
		*queries = append(*queries, q)
		switch {
		case strings.Contains(q, "where collections ="): // a DLC and a port slip through the fake: the client must drop them too
			w.Write([]byte(`[
			 {"id":10,"name":"Zed","first_release_date":852076800,"game_type":0,"cover":{"image_id":"co1"},"collections":[{"id":7,"name":"Zed Series"}],
			  "release_dates":[{"date":852076800,"date_format":0}],"platforms":[{"abbreviation":"PC"},{"name":"Switch"}]},
			 {"id":11,"name":"Zed II","first_release_date":1104537600,"game_type":8,"collections":[{"id":7,"name":"Zed Series"}],"release_dates":[{"date":1104537600,"date_format":7}]},
			 {"id":12,"name":"Zed: DLC","first_release_date":1104537600,"game_type":1,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":13,"name":"Zed (Port)","game_type":11,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":14,"name":"  ","game_type":0}]`))
		case strings.Contains(q, "fields id,first_release_date"): // ReleaseDates
			w.Write([]byte(`[{"id":10,"first_release_date":1830211199,"release_dates":[{"date":1830211199,"date_format":2}],"platforms":[{"abbreviation":"PC"}]}]`))
		case strings.Contains(q, "fields collections.id"):
			w.Write([]byte(`[{"id":10,"collections":[{"id":7,"name":"Zed Series"},{"id":8,"name":"Other"}]},{"id":99}]`))
		default: // one game as a candidate
			w.Write([]byte(`[{"id":10,"name":"Zed","first_release_date":852076800,"genres":[{"name":"RPG"}],"collections":[{"id":7,"name":"Zed Series"}],"cover":{"image_id":"co1"}}]`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	c.IGDBGap = 0
	return c
}

func TestCollectionGamesKeepsOnlyRealGames(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	name, games, err := c.CollectionGames(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Zed Series" || len(games) != 2 || games[0].IGDBID != 10 || games[0].Year != 1997 || games[0].CoverID != "co1" || games[1].GameType != 8 {
		t.Errorf("name %q games %+v", name, games)
	}
	// the type filter is also part of the request, so IGDB does the work
	if !strings.Contains(queries[0], "game_type = (0,4,8,9)") || !strings.Contains(queries[0], "collections = (7)") {
		t.Errorf("query: %s", queries[0])
	}
	if strings.Contains(strings.ToLower(strings.Join(queries, " ")), "rating") {
		t.Error("scores must never be requested")
	}
}

func TestCollectionsOfTakesTheFirstCollection(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	got, err := c.CollectionsOf(context.Background(), []int64{10, 99})
	if err != nil || len(got) != 1 || got[10].ID != 7 || got[10].Name != "Zed Series" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestGameCandidate(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	cand, err := c.GameCandidate(context.Background(), 10)
	if err != nil || cand.IGDBID != 10 || cand.Name != "Zed" || cand.Year != 1997 || cand.Series != "Zed Series" || cand.CoverURL == "" {
		t.Errorf("candidate %+v, %v", cand, err)
	}
}

func TestIsRealGameType(t *testing.T) {
	for typ, want := range map[int]bool{0: true, 4: true, 8: true, 9: true, 1: false, 2: false, 3: false, 5: false, 6: false, 7: false, 10: false, 11: false, 12: false, 13: false, 14: false} {
		if IsRealGameType(typ) != want {
			t.Errorf("type %d: want %v", typ, want)
		}
	}
}

// TestLiveCollections reads the collections of a few well-known series from the real IGDB (DELTABACKLOG_LIVE=1 plus the
// keys): the game's collection id, then the real games of that collection.
func TestLiveCollections(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 with IGDB_CLIENT_ID and IGDB_CLIENT_SECRET to run")
	}
	c := New(Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
	ctx := context.Background()
	for _, title := range []string{"The Legend of Zelda: Ocarina of Time", "Dark Souls", "Portal 2", "Phoenix Wright: Ace Attorney"} {
		cands, err := c.SearchIGDB(ctx, title)
		if err != nil || len(cands) == 0 {
			t.Fatalf("%s: search %v %v", title, cands, err)
		}
		refs, err := c.CollectionsOf(ctx, []int64{cands[0].IGDBID})
		if err != nil || len(refs) != 1 {
			t.Fatalf("%s: collections %v %v", title, refs, err)
		}
		ref := refs[cands[0].IGDBID]
		name, games, err := c.CollectionGames(ctx, ref.ID)
		if err != nil || len(games) == 0 {
			t.Fatalf("%s: games %v %v", title, games, err)
		}
		var withCover int
		for _, g := range games {
			if !IsRealGameType(g.GameType) {
				t.Errorf("%s: a game of type %d slipped through", g.Name, g.GameType)
			}
			if g.CoverID != "" {
				withCover++
			}
		}
		t.Logf("%s -> collection %d %q: %d real games, %d with a cover", title, ref.ID, name, len(games), withCover)
	}
}
