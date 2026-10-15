package meta

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// searchServer answers IGDB game queries with canned JSON by query kind and records the queries.
func searchServer(t *testing.T, search, exact string) (*Client, *[]string) {
	t.Helper()
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
		case strings.HasPrefix(q, "search "):
			w.Write([]byte(search))
		case strings.HasPrefix(q, "where name ~ "):
			w.Write([]byte(exact))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	c.IGDBGap = 0
	return c, &queries
}

func ids(cs []Candidate) []int64 {
	var out []int64
	for _, c := range cs {
		out = append(out, c.IGDBID)
	}
	return out
}

func wantIDs(t *testing.T, got []Candidate, want ...int64) {
	t.Helper()
	g := ids(got)
	if len(g) < len(want) {
		t.Fatalf("ids = %v, want prefix %v", g, want)
	}
	for i, w := range want {
		if g[i] != w {
			t.Fatalf("ids = %v, want prefix %v", g, want)
		}
	}
}

const godOfWarSearch = `[
 {"id":19560,"name":"God of War","first_release_date":1524182400,"game_type":0},
 {"id":112875,"name":"God of War Ragnarök","first_release_date":1667952000,"game_type":0},
 {"id":23827,"name":"God of War Saga","first_release_date":1346025600,"game_type":3},
 {"id":99,"name":"God of War: Mythology DLC","first_release_date":1346025600,"game_type":1}]`

const godOfWarExact = `[
 {"id":19560,"name":"God of War","first_release_date":1524182400,"game_type":0},
 {"id":549,"name":"God of War","first_release_date":1111449600,"game_type":0}]`

func TestSearchFindsTheOriginalBuriedByRelatedGames(t *testing.T) {
	c, _ := searchServer(t, godOfWarSearch, godOfWarExact)
	got, err := c.SearchIGDB(context.Background(), "God of War")
	if err != nil {
		t.Fatal(err)
	}
	// exact names first, the older one before the namesake; then IGDB's order with the bundle and DLC last
	wantIDs(t, got, 549, 19560, 112875, 23827, 99)
	if len(got) != 5 {
		t.Errorf("duplicates must be merged: %v", ids(got))
	}
}

func TestSearchYearHint(t *testing.T) {
	for _, title := range []string{"God of War 2018", "God of War (2018)"} {
		c, queries := searchServer(t, godOfWarSearch, godOfWarExact)
		got, err := c.SearchIGDB(context.Background(), title)
		if err != nil {
			t.Fatal(err)
		}
		wantIDs(t, got, 19560, 549) // both are named exactly, the 2018 one matches the year
		if !strings.HasPrefix((*queries)[1], `search "God of War"`) {
			t.Errorf("%q: the search without the year must be asked: %q", title, *queries)
		}
	}
	c, _ := searchServer(t, godOfWarSearch, godOfWarExact)
	got, _ := c.SearchIGDB(context.Background(), "God of War 2005")
	wantIDs(t, got, 549)
}

func TestSplitYear(t *testing.T) {
	for in, want := range map[string]struct {
		title string
		year  int
	}{
		"God of War 2005":  {"God of War", 2005},
		"Portal (2007)":    {"Portal", 2007},
		"Portal(2007)":     {"Portal", 2007},
		"Cyberpunk 2077":   {"Cyberpunk 2077", 0},
		"1942":             {"1942", 0},
		"Battlefield 1942": {"Battlefield 1942", 0},
		"Fallout 76":       {"Fallout 76", 0},
		"Hades":            {"Hades", 0},
		"Madden NFL 2005":  {"Madden NFL", 2005},
	} {
		if title, year := SplitYear(in); title != want.title || year != want.year {
			t.Errorf("SplitYear(%q) = %q, %d; want %q, %d", in, title, year, want.title, want.year)
		}
	}
}

func TestSearchKeepsNameWithYearAsExactMatch(t *testing.T) {
	// "Madden NFL 2005" is a real name: the exact query for the whole title must be asked and win.
	c, queries := searchServer(t, `[{"id":1,"name":"Madden NFL","game_type":0},{"id":2,"name":"Madden NFL 06","first_release_date":1124000000}]`,
		`[{"id":3,"name":"Madden NFL 2005","first_release_date":1090000000}]`)
	got, err := c.SearchIGDB(context.Background(), "Madden NFL 2005")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs(t, got, 3)
	if joined := strings.Join(*queries, "|"); !strings.Contains(joined, `where name ~ "Madden NFL 2005"`) {
		t.Errorf("queries = %s", joined)
	}
}

func TestSearchDemotesBundlesAndDLC(t *testing.T) {
	c, _ := searchServer(t, `[
 {"id":1,"name":"Portal Pack","game_type":3},
 {"id":2,"name":"Portal Stories: Mel","game_type":0},
 {"id":3,"name":"Portal Mod","game_type":5},
 {"id":4,"name":"Portal Knights","game_type":0}]`, `[]`)
	got, _ := c.SearchIGDB(context.Background(), "Portal")
	wantIDs(t, got, 2, 4, 1, 3)
}

func TestSearchCapsResults(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= 40; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"Doom %c"}`, i, 'A'+i%26)
	}
	b.WriteString("]")
	c, _ := searchServer(t, b.String(), `[]`)
	got, _ := c.SearchIGDB(context.Background(), "Doom")
	if len(got) != searchCap {
		t.Errorf("got %d results, want %d", len(got), searchCap)
	}
}

// TestLiveSearch prints the top results for titles the old search got wrong (DELTABACKLOG_LIVE=1 plus the keys).
func TestLiveSearch(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 with IGDB_CLIENT_ID and IGDB_CLIENT_SECRET to run")
	}
	c := New(Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
	for _, title := range []string{"God of War", "God of War 2005", "Portal", "Portal 2007", "Doom", "Prey", "Tomb Raider", "Halo", "Hitman", "Tetris", "FIFA 2003", "Madden NFL 2005"} {
		got, err := c.SearchIGDB(context.Background(), title)
		if err != nil {
			t.Fatal(err)
		}
		got = TopResults(got, title)
		var top []string
		show := 5
		if title == "Tetris" || title == "Doom" {
			show = maxResults
		}
		for _, g := range got[:min(show, len(got))] {
			top = append(top, g.Name+" ("+itoa(g.Year)+", "+strings.Join(g.Platforms, "/")+")")
		}
		t.Logf("%-16s -> %s", title, strings.Join(top, "; "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "?"
	}
	return string([]byte{byte('0' + n/1000%10), byte('0' + n/100%10), byte('0' + n/10%10), byte('0' + n%10)})
}

func TestSearchNamesakesRankByTypeThenStoresThenAge(t *testing.T) {
	c, _ := searchServer(t, `[]`, `[
 {"id":1,"name":"Doom","first_release_date":757382400,"game_type":0,"external_games":[1,2,3]},
 {"id":2,"name":"Doom","first_release_date":780000000,"game_type":11,"external_games":[1,2,3,4,5,6,7,8]},
 {"id":3,"name":"Doom","first_release_date":1100000000,"game_type":9,"external_games":[1,2,3,4]},
 {"id":4,"name":"Doom","first_release_date":1400000000,"game_type":5,"external_games":[1,2,3,4,5,6,7,8,9]},
 {"id":5,"name":"Doom","first_release_date":1463097600,"game_type":0,"external_games":[1,2,3,4,5,6,7,8,9,10]},
 {"id":6,"name":"Doom","first_release_date":600000000,"game_type":0}]`)
	got, err := c.SearchIGDB(context.Background(), "Doom")
	if err != nil {
		t.Fatal(err)
	}
	// most store ids first whatever the type, a mod (DLC, bundle) last
	wantIDs(t, got, 5, 2, 3, 1, 6, 4)
}

func TestSearchKeepsFranchiseHitsNextToManyNamesakes(t *testing.T) {
	var exact strings.Builder
	exact.WriteString("[")
	for i := 1; i <= 10; i++ {
		if i > 1 {
			exact.WriteString(",")
		}
		fmt.Fprintf(&exact, `{"id":%d,"name":"Tetris","first_release_date":%d,"game_type":8}`, i, 500000000+i)
	}
	exact.WriteString("]")
	c, _ := searchServer(t, `[{"id":100,"name":"Tetris 99","game_type":0},{"id":101,"name":"Tetris Effect","game_type":0},
 {"id":102,"name":"Tetris Worlds","game_type":0},{"id":103,"name":"Tetris Attack","game_type":0},
 {"id":104,"name":"Tetris DS","game_type":0},{"id":105,"name":"Tetris Plus","game_type":0},{"id":106,"name":"Tetris Party","game_type":0}]`, exact.String())
	all, _ := c.SearchIGDB(context.Background(), "Tetris")
	if len(all) != 17 {
		t.Fatalf("SearchIGDB must keep every namesake for the sync: %v", ids(all))
	}
	got := TopResults(all, "Tetris")
	if len(got) != maxResults {
		t.Fatalf("got %d: %v", len(got), ids(got))
	}
	wantIDs(t, got, 1, 2, 3, 4, 5, 6, 100, 101, 102, 103, 104, 105)
}

func TestSearchFullTitleWithYearIsSearchedToo(t *testing.T) {
	// IGDB names it "FIFA Soccer 2003": only the search for the whole query finds it.
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
		if strings.HasPrefix(q, `search "FIFA 2003"`) {
			w.Write([]byte(`[{"id":7,"name":"FIFA Soccer 2003","first_release_date":1033000000}]`))
			return
		}
		w.Write([]byte(`[{"id":8,"name":"FIFA 98","first_release_date":880000000}]`))
	}))
	defer srv.Close()
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	c.IGDBGap = 0
	got, err := c.SearchIGDB(context.Background(), "FIFA 2003")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs(t, got, 7, 8)
	if len(queries) < 2 || queries[0] != `search "FIFA 2003"` || queries[1] != `search "FIFA"` {
		t.Errorf("queries = %q", queries)
	}
}

func TestSearchKeepsHitsWhenAnExtraQueryFails(t *testing.T) {
	for _, status := range []int{400, 500, 429} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/token") {
				w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
				return
			}
			body := make([]byte, 2000)
			n, _ := r.Body.Read(body)
			if strings.HasPrefix(string(body[:n]), "where name ~ ") {
				http.Error(w, "boom", status)
				return
			}
			w.Write([]byte(`[{"id":1,"name":"Hades"}]`))
		}))
		c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
		c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
		c.IGDBGap = 0
		got, err := c.SearchIGDB(context.Background(), "Hades")
		srv.Close()
		if status == 429 {
			var he *HTTPError
			if !errors.As(err, &he) || he.Status != 429 {
				t.Errorf("429 must be returned so the caller backs off: %v", err)
			}
			continue
		}
		if err != nil || len(got) != 1 {
			t.Errorf("status %d: got %v, %v", status, got, err)
		}
	}
}

func TestIGDBRequestsArePaced(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		atomic.AddInt32(&n, 1)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := New(Config{IGDBClientID: "cid", IGDBClientSecret: "sec"})
	c.TokenURL, c.IGDBURL = srv.URL+"/token", srv.URL+"/igdb"
	c.IGDBGap = 40 * time.Millisecond
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.GameDetails(context.Background(), []int64{1}) }()
	}
	wg.Wait()
	if got := time.Since(start); got < 4*40*time.Millisecond {
		t.Errorf("5 concurrent requests took %v, want at least 160ms", got)
	}
}
