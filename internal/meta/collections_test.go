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
			 {"id":10,"name":"Zed","first_release_date":852076800,"game_type":0,"cover":{"image_id":"co1"},"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":11,"name":"Zed II","first_release_date":1104537600,"game_type":8,"parent_game":10,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":15,"name":"Zed III","game_type":9,"parent_game":11,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":16,"name":"Zed Legacy","game_type":4,"parent_game":10,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":17,"name":"Zed Edition","game_type":0,"version_parent":10,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":12,"name":"Zed: DLC","first_release_date":1104537600,"game_type":1,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":13,"name":"Zed (Port)","game_type":11,"collections":[{"id":7,"name":"Zed Series"}]},
			 {"id":14,"name":"  ","game_type":0}]`))
		case strings.Contains(q, "fields collections.id"):
			w.Write([]byte(`[{"id":10,"game_type":10,"collections":[{"id":7,"name":"Zed Series"},{"id":8,"name":"Other"}],"parent_game":5},{"id":11,"version_parent":10},{"id":12,"game_type":4,"parent_game":10}]`))
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
	if name != "Zed Series" || len(games) != 5 || games[0].IGDBID != 10 || games[0].Year != 1997 || games[0].CoverID != "co1" || games[1].GameType != 8 {
		t.Errorf("name %q games %+v", name, games)
	}
	// Only a version of the same game has a parent: a remaster (type 9) through parent_game, an edition through
	// version_parent. A remake (type 8) or a standalone expansion (type 4) is a game of its own, whatever its parent_game.
	if games[0].ParentID != 0 || games[1].ParentID != 0 || games[2].ParentID != 11 || games[3].ParentID != 0 || games[4].ParentID != 10 {
		t.Errorf("parents: %+v", games)
	}
	if !strings.Contains(queries[0], "parent_game") || !strings.Contains(queries[0], "version_parent") {
		t.Errorf("query lacks the relation fields: %s", queries[0])
	}
	// the type filter is also part of the request, so IGDB does the work
	if !strings.Contains(queries[0], "game_type = (0,4,8,9)") || !strings.Contains(queries[0], "collections = (7)") {
		t.Errorf("query: %s", queries[0])
	}
	if strings.Contains(strings.ToLower(strings.Join(queries, " ")), "rating") {
		t.Error("scores must never be requested")
	}
}

func TestGameLinksReadsEveryCollectionAndTheParent(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	got, err := c.GameLinks(context.Background(), []int64{10, 11, 12, 99})
	if err != nil || len(got) != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if l := got[10]; len(l.Collections) != 2 || l.Collections[0].ID != 7 || l.Collections[1].Name != "Other" || l.ParentID != 5 {
		t.Errorf("game 10: %+v", l)
	}
	if l := got[11]; len(l.Collections) != 0 || l.ParentID != 10 { // version_parent counts for every type
		t.Errorf("game 11: %+v", l)
	}
	if l := got[12]; l.ParentID != 0 { // a standalone expansion is not a version of its parent (Uncharted: The Lost Legacy)
		t.Errorf("game 12: %+v", l)
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
		refs, err := c.GameLinks(ctx, []int64{cands[0].IGDBID})
		if err != nil || len(refs) != 1 {
			t.Fatalf("%s: collections %v %v", title, refs, err)
		}
		if len(refs[cands[0].IGDBID].Collections) == 0 {
			t.Fatalf("%s: no collection", title)
		}
		ref := refs[cands[0].IGDBID].Collections[0]
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

// parent_game links versions of one game (remaster, expanded game, port) but also a standalone expansion to the game it
// expands and a remake to the game it remakes: only the first kind makes a game "owned" with its parent.
func TestOnlyVersionsOfTheSameGameHaveAParent(t *testing.T) {
	cases := []struct {
		name string
		g    igdbGame
		want int64
	}{
		{"remaster", igdbGame{GameType: 9, ParentGame: 5}, 5},
		{"expanded game", igdbGame{GameType: 10, ParentGame: 5}, 5},
		{"port", igdbGame{GameType: 11, ParentGame: 5}, 5},
		{"edition by version_parent", igdbGame{GameType: 0, VersionParent: 6}, 6},
		{"version_parent first", igdbGame{GameType: 9, ParentGame: 5, VersionParent: 6}, 6},
		{"Uncharted: The Lost Legacy (standalone expansion of Uncharted 4)", igdbGame{GameType: 4, ParentGame: 7331}, 0},
		{"Resident Evil 2 (2019, a remake)", igdbGame{GameType: 8, ParentGame: 880}, 0},
		{"main game", igdbGame{GameType: 0}, 0},
	}
	for _, c := range cases {
		if got := c.g.parent(); got != c.want {
			t.Errorf("%s: parent %d, want %d", c.name, got, c.want)
		}
	}
}
