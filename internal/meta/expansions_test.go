package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func expansionFake(t *testing.T, queries *[]string, body string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	})
	mux.HandleFunc("/igdb/games", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*queries = append(*queries, string(b))
		w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	c.IGDBGap = 0
	return c
}

// Expansions (game_type 2) and DLC (1) are read, each once; standalone expansions and packs are not. The Steam app id comes
// from the new source field or the old category, and a reference that is an object is read too.
func TestExpansionsReadsExpansionsAndDLCWithSteamIDs(t *testing.T) {
	var queries []string
	c := expansionFake(t, &queries, `[
	 {"id":1942,"expansions":[
	  {"id":100,"name":"Hearts of Stone","game_type":2,"first_release_date":1432598400,"cover":{"image_id":"hos"},
	   "external_games":[{"uid":"1","external_game_source":5},{"uid":"378648","external_game_source":1}]},
	  {"id":101,"name":"Blood and Wine","game_type":2,"first_release_date":1464134400,"external_games":[{"uid":"378649","category":1}]},
	  {"id":103,"name":"Standalone","game_type":4},
	  {"id":104,"name":"Object source","game_type":2,"external_games":[{"uid":"777","external_game_source":{"id":1,"name":"Steam"}}]},
	  {"id":105,"name":"GOG only","game_type":2,"external_games":[{"uid":"55","external_game_source":5}]},
	  {"id":106,"name":" ","game_type":2}],
	  "dlcs":[
	  {"id":102,"name":"Some DLC","game_type":1,"external_games":[{"uid":"9","category":1}]},
	  {"id":100,"name":"Hearts of Stone","game_type":2},
	  {"id":107,"name":"Costume pack","game_type":13}]},
	 {"id":7}]`)
	got, err := c.Expansions(context.Background(), []int64{1942, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	list := got[1942]
	if len(list) != 5 {
		t.Fatalf("%+v", list)
	}
	for i, want := range []string{KindExpansion, KindExpansion, KindExpansion, KindExpansion, KindDLC} {
		if list[i].Kind != want {
			t.Errorf("%d: kind %q, want %q", i, list[i].Kind, want)
		}
	}
	if d := list[4]; d.IGDBID != 102 || d.SteamAppID != 9 {
		t.Errorf("the DLC: %+v", d)
	}
	h := list[0]
	if h.IGDBID != 100 || h.Name != "Hearts of Stone" || h.Year != 2015 || h.CoverID != "hos" || h.SteamAppID != 378648 || h.Release != 1432598400 {
		t.Errorf("%+v", h)
	}
	if list[1].SteamAppID != 378649 || list[2].SteamAppID != 777 || list[3].SteamAppID != 0 || list[1].Year != 2016 {
		t.Errorf("%+v", list)
	}
	if l, ok := got[7]; !ok || len(l) != 0 {
		t.Errorf("a game without expansions is present and empty: %+v", got)
	}
	if _, ok := got[8]; ok {
		t.Error("a game IGDB does not return is absent")
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "expansions.external_games.*") || !strings.Contains(queries[0], "dlcs.external_games.*") || !strings.Contains(queries[0], "where id = (1942,7,8)") {
		t.Errorf("query %v", queries)
	}
}

func TestExpansionsBatchLimit(t *testing.T) {
	var queries []string
	c := expansionFake(t, &queries, `[]`)
	ids := make([]int64, ExpansionBatch+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	if _, err := c.Expansions(context.Background(), ids); err == nil {
		t.Error("more than one batch must be refused")
	}
	if _, err := c.Expansions(context.Background(), ids[:ExpansionBatch]); err != nil || len(queries) != 1 {
		t.Errorf("a full batch is one request: %v %d", err, len(queries))
	}
	if got, err := c.Expansions(context.Background(), nil); err != nil || len(got) != 0 || len(queries) != 1 {
		t.Error("no ids, no request")
	}
}

// DELTABACKLOG_LIVE=1: The Witcher 3 (IGDB 1942) has Hearts of Stone and Blood and Wine, with Steam app ids.
func TestLiveExpansions(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") == "" {
		t.Skip("set DELTABACKLOG_LIVE=1 (and the IGDB keys) to run")
	}
	c := New(Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
	got, err := c.Expansions(context.Background(), []int64{1942})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]int64{}
	for _, e := range got[1942] {
		names[e.Name] = e.SteamAppID
		t.Logf("%s (%d) steam %s", e.Name, e.Year, strconv.FormatInt(e.SteamAppID, 10))
	}
	for _, want := range []string{"Hearts of Stone", "Blood and Wine"} {
		if names[want] == 0 {
			t.Errorf("%s: no expansion or no Steam app id in %v", want, names)
		}
	}
}
