package hltb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

const answer = `{"count":4,"data":[
 {"game_id":1,"game_name":"Portal 2 Mod","game_type":"mod","comp_main":3600,"comp_plus":3600,"comp_100":3600,"release_world":2011},
 {"game_id":2,"game_name":"Portal","game_type":"game","comp_main":10800,"comp_plus":14400,"comp_100":27000,"release_world":2007,"review_score":90},
 {"game_id":3,"game_name":"Portal","game_type":"game","comp_main":600,"comp_plus":0,"comp_100":0,"release_world":1999},
 {"game_id":4,"game_name":"Empty","game_type":"game","comp_main":0,"comp_plus":0,"comp_100":0,"release_world":2000}]}`

func fake(t *testing.T, inits *atomic.Int32, search http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search/site/init", func(w http.ResponseWriter, r *http.Request) {
		n := inits.Add(1)
		w.Write([]byte(`{"token":"tok` + string(rune('0'+n)) + `"}`))
	})
	mux.HandleFunc("POST /api/search/site", search)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New()
	c.BaseURL = srv.URL
	c.Gap = 0
	return c
}

func TestSearchRanksFiltersAndConverts(t *testing.T) {
	var inits atomic.Int32
	c := fake(t, &inits, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-auth-token") != "tok1" || r.Header.Get("Referer") == "" || r.Header.Get("User-Agent") == "" {
			t.Errorf("headers: %v", r.Header)
		}
		w.Write([]byte(answer))
	})
	got, err := c.Search(context.Background(), "Portal", 2007)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("mods and empty entries are dropped, the 2007 one comes first: %+v", got)
	}
	if got[0].Hastily != 180 || got[0].Normally != 240 || got[0].Completely != 450 || got[0].Year != 2007 {
		t.Errorf("seconds -> minutes: %+v", got[0])
	}
	if got[1].Hastily != 10 || got[1].Normally != 0 {
		t.Errorf("missing kinds stay 0: %+v", got[1])
	}
	if _, err := c.Search(context.Background(), "Portal", 0); err != nil || inits.Load() != 1 {
		t.Errorf("the token is reused: inits=%d err=%v", inits.Load(), err)
	}
}

func TestSearchRefreshesAnExpiredToken(t *testing.T) {
	var inits atomic.Int32
	c := fake(t, &inits, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-auth-token") == "tok1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(answer))
	})
	got, err := c.Search(context.Background(), "Portal", 0)
	if err != nil || len(got) == 0 || inits.Load() != 2 {
		t.Fatalf("got %v err %v inits %d", got, err, inits.Load())
	}
}

func TestSearchErrors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"blocked", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, ErrUnavailable},
		{"moved", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }, ErrChanged},
		{"html", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>Just a moment</html>")) }, ErrChanged},
		{"other shape", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"results":[]}`)) }, ErrChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var inits atomic.Int32
			c := fake(t, &inits, tc.handler)
			if _, err := c.Search(context.Background(), "Portal", 0); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestUnreachableAndSlowSitesAreUnavailable(t *testing.T) {
	c := New()
	c.BaseURL = "http://127.0.0.1:1"
	if _, err := c.Search(context.Background(), "Portal", 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("refused: %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer slow.Close()
	c = New()
	c.BaseURL = slow.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Search(ctx, "Portal", 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("timeout: %v", err)
	}
}

func TestSearchesAreSpacedOut(t *testing.T) {
	var inits atomic.Int32
	c := fake(t, &inits, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(answer)) })
	c.Gap = 120 * time.Millisecond
	start := time.Now()
	for range 3 {
		if _, err := c.Search(context.Background(), "Portal", 0); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 220*time.Millisecond {
		t.Errorf("3 searches took %v", d)
	}
}

func TestBlankTitleSearchesNothing(t *testing.T) {
	c := New()
	c.BaseURL = "http://127.0.0.1:1"
	if got, err := c.Search(context.Background(), "  ", 0); err != nil || got == nil || len(got) != 0 {
		t.Errorf("%v %v", got, err)
	}
}

// TestLive asks the real site (DELTABACKLOG_LIVE=1). Run it rarely: it is not an API and should not be hammered.
func TestLive(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") != "1" {
		t.Skip("set DELTABACKLOG_LIVE=1 to run")
	}
	c := New()
	for _, q := range []struct {
		title string
		year  int
	}{{"God of War", 2005}, {"Portal", 2007}, {"Hollow Knight", 2017}} {
		got, err := c.Search(context.Background(), q.title, q.year)
		if err != nil || len(got) == 0 {
			t.Fatalf("%s: %v %v", q.title, got, err)
		}
		for _, x := range got[:min(3, len(got))] {
			t.Logf("%s (%d) -> %s (%d): main %d, extras %d, complete %d min", q.title, q.year, x.Name, x.Year, x.Hastily, x.Normally, x.Completely)
		}
	}
}
