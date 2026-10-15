package meta

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
		if !strings.HasPrefix((*queries)[0], `search "God of War"`) {
			t.Errorf("%q: the year must not reach the search text: %q", title, *queries)
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
	if len(got) != maxResults {
		t.Errorf("got %d results, want %d", len(got), maxResults)
	}
}

// TestLiveSearch prints the top results for titles the old search got wrong (DELTABACKLOG_LIVE=1 plus the keys).
func TestLiveSearch(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 with IGDB_CLIENT_ID and IGDB_CLIENT_SECRET to run")
	}
	c := New(Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
	for _, title := range []string{"God of War", "God of War 2005", "Portal", "Portal 2007", "Doom", "Prey", "Tomb Raider", "Halo", "Hitman"} {
		got, err := c.SearchIGDB(context.Background(), title)
		if err != nil {
			t.Fatal(err)
		}
		var top []string
		for _, g := range got[:min(5, len(got))] {
			top = append(top, g.Name+" ("+itoa(g.Year)+")")
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
