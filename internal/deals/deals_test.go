package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

const secret = "sekrit-key-123"

// fakeITAD is an offline IsThereAnyDeal server.
func fakeITAD(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ITAD-API-Key") != secret || r.URL.Query().Get("key") != "" {
			http.Error(w, "key", http.StatusForbidden)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(secret)
	c.BaseURL, c.Gap = srv.URL, 0
	return c
}

func TestClientLookupAndSearch(t *testing.T) {
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/games/lookup/v1" && r.URL.Query().Get("appid") == "367520":
			w.Write([]byte(`{"found":true,"game":{"id":"u-hk","slug":"hollow-knight","title":"Hollow Knight","type":"game"}}`))
		case r.URL.Path == "/games/lookup/v1":
			w.Write([]byte(`{"found":false}`))
		case r.URL.Path == "/games/search/v1":
			w.Write([]byte(`[{"id":"a","slug":"s","title":"Hades","type":"game"},{"id":"","title":"broken"}]`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	g, ok, err := c.LookupAppID(ctx, 367520)
	if err != nil || !ok || g.ID != "u-hk" || g.Title != "Hollow Knight" {
		t.Fatalf("appid: %+v %v %v", g, ok, err)
	}
	if _, ok, err := c.LookupTitle(ctx, "Nope"); err != nil || ok {
		t.Fatalf("title: %v %v", ok, err)
	}
	list, err := c.Search(ctx, "hades")
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("search: %+v %v", list, err)
	}
}

func TestClientOverview(t *testing.T) {
	var body []string
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/games/overview/v2" || r.URL.Query().Get("country") != "ES" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"prices":[
		 {"id":"a","current":{"shop":{"id":61,"name":"Steam"},"price":{"amountInt":749,"currency":"EUR"},"regular":{"amountInt":1499,"currency":"EUR"},"cut":50,"url":"https://shop.example/a","expiry":null},
		  "lowest":{"shop":{"id":1,"name":"GOG"},"price":{"amountInt":599,"currency":"EUR"},"cut":60,"timestamp":"2025-11-27T10:00:00+01:00"}},
		 {"id":"b","current":{"shop":{"name":"X"},"price":{"amountInt":1,"currency":"EUR"},"regular":{"amountInt":1,"currency":"EUR"},"cut":250,"url":"javascript:alert(1)"},"lowest":null},
		 {"id":"c","current":null,"lowest":null}],"bundles":[]}`))
	})
	res, err := c.Overview(context.Background(), []string{"a", "b", "c"}, "ES")
	if err != nil || len(body) != 3 {
		t.Fatalf("%v %v", err, body)
	}
	a := res["a"]
	if a.Current.Price.Cents != 749 || a.Current.Cut != 50 || a.Current.Shop != "Steam" || a.Current.URL != "https://shop.example/a" || a.Low.Price.Cents != 599 || a.Low.Shop != "GOG" {
		t.Errorf("a = %+v %+v %+v", a, a.Current, a.Low)
	}
	if b := res["b"]; b.Current.Cut != 100 || b.Current.URL != "" || b.Low != nil {
		t.Errorf("b = %+v", b.Current)
	}
	if c := res["c"]; c.Current != nil || c.Low != nil {
		t.Errorf("c = %+v", c)
	}
	if _, err := c.Overview(context.Background(), make([]string, MaxBatch+1), "ES"); err == nil {
		t.Error("an oversized batch must be refused")
	}
}

func TestClientErrors(t *testing.T) {
	ctx := context.Background()
	if _, _, err := New("").LookupTitle(ctx, "x"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("missing key: %v", err)
	}
	if New("  ").Configured() {
		t.Error("a blank key is no key")
	}

	var mode atomic.Value
	mode.Store("429")
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case "429":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case "json":
			w.Write([]byte(`<html>`))
		case "big":
			w.Write([]byte(`{"found":false,"pad":"` + strings.Repeat("x", maxResponse) + `"}`))
		case "500":
			w.WriteHeader(500)
		case "redirect":
			http.Redirect(w, r, "https://evil.example/", http.StatusFound)
		}
	})
	_, _, err := c.LookupTitle(ctx, "x")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Errorf("429: %v", err)
	}
	for _, m := range []string{"json", "big", "500", "redirect"} {
		mode.Store(m)
		if _, _, err := c.LookupTitle(ctx, "x"); err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: %v", m, err)
		}
	}
	bad := New(secret)
	bad.BaseURL = "https://evil.example"
	if _, _, err := bad.LookupTitle(ctx, "x"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("a foreign host must be refused: %v", err)
	}
}

// ---------------- refresh ----------------

type fakeSource struct {
	byApp    map[int64]Game
	byTitle  map[string]Game
	prices   map[string]Overview
	err      error // returned by Overview
	lookups  int
	overview [][]string
}

func (f *fakeSource) Configured() bool { return true }
func (f *fakeSource) LookupAppID(_ context.Context, id int64) (Game, bool, error) {
	f.lookups++
	g, ok := f.byApp[id]
	return g, ok, nil
}
func (f *fakeSource) LookupTitle(_ context.Context, t string) (Game, bool, error) {
	f.lookups++
	g, ok := f.byTitle[t]
	return g, ok, nil
}
func (f *fakeSource) Search(context.Context, string) ([]Game, error) { return nil, nil }
func (f *fakeSource) Overview(_ context.Context, ids []string, _ string) (map[string]Overview, error) {
	f.overview = append(f.overview, ids)
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]Overview{}
	for _, id := range ids {
		out[id] = f.prices[id]
	}
	return out, nil
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func addGame(t *testing.T, d *sql.DB, id int64, title, status, steam string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO game(id, title, title_sort, status_id) VALUES (?, ?, ?, (SELECT id FROM status WHERE code = ?))`, id, title, strings.ToLower(title), status); err != nil {
		t.Fatal(err)
	}
	if steam != "" {
		if _, err := d.Exec(`INSERT INTO game_link(game_id, kind, url) VALUES (?, 'steam', ?)`, id, steam); err != nil {
			t.Fatal(err)
		}
	}
}

func eur(c int64) Money { return Money{c, "EUR"} }

func TestRefreshMatchesAndPrices(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "Hollow Knight", "backlog", "https://store.steampowered.com/app/367520/Hollow_Knight/")
	addGame(t, d, 2, "Disco Elysium", "backlog", "")
	addGame(t, d, 3, "Obscure Thing", "up_next", "")
	addGame(t, d, 4, "Hades", "finished", "")     // not watched
	addGame(t, d, 5, "Wrong Name", "backlog", "") // ITAD answers a different title: no automatic match
	src := &fakeSource{
		byApp:   map[int64]Game{367520: {ID: "hk", Title: "Hollow Knight"}},
		byTitle: map[string]Game{"Disco Elysium": {ID: "de", Title: "Disco Elysium - The Final Cut"}, "Wrong Name": {ID: "zz", Title: "Something Else"}},
		prices: map[string]Overview{
			"hk": {Current: &Current{Shop: "Steam", Price: eur(749), Regular: eur(1499), Cut: 50, URL: "https://s.example/hk"}, Low: &Low{Shop: "GOG", Price: eur(599), At: "2025-01-01T00:00:00Z"}},
		},
	}
	// "Disco Elysium - The Final Cut" is not the same title as the library's: strict matching refuses it.
	rep, err := Refresh(context.Background(), d, src, Options{})
	if err != nil || rep.Stopped != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if rep.Games != 4 || rep.Matched != 1 || rep.Unmatched != 3 || rep.Priced != 1 {
		t.Errorf("report %+v", rep)
	}
	l, _ := Load(d)
	if len(l.Prices) != 1 || l.Prices[0].GameID != 1 || *l.Prices[0].Price != 749 || *l.Prices[0].LowPrice != 599 || *l.Prices[0].Shop != "Steam" {
		t.Errorf("prices %+v", l.Prices)
	}
	src2 := map[int64]string{}
	for _, m := range l.Matches {
		src2[m.GameID] = m.Source + ":" + m.ITADID
	}
	if src2[1] != "steam:hk" || src2[2] != "title:" || src2[3] != "title:" || len(src2) != 4 {
		t.Errorf("matches %v", src2)
	}

	// A second refresh does not search again (the "no match" answers are trusted for a week) but reads the prices again.
	before := src.lookups
	if _, err := Refresh(context.Background(), d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	if src.lookups != before {
		t.Errorf("searched again: %d lookups", src.lookups-before)
	}
	// ... until they are old.
	d.Exec(`UPDATE deal_match SET matched_at = '2020-01-01T00:00:00Z' WHERE itad_id IS NULL`)
	Refresh(context.Background(), d, src, Options{})
	if src.lookups == before {
		t.Error("an old automatic no-match must be searched again")
	}
}

func TestOwnerChoiceWins(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "Disco Elysium", "backlog", "")
	addGame(t, d, 2, "Mystery", "backlog", "")
	src := &fakeSource{byTitle: map[string]Game{"Mystery": {ID: "m", Title: "Mystery"}},
		prices: map[string]Overview{"de": {Current: &Current{Shop: "GOG", Price: eur(100), Regular: eur(200), Cut: 50}}}}
	if err := SetMatch(d, 1, Game{ID: "de", Title: "Disco Elysium - The Final Cut", Slug: "disco"}); err != nil {
		t.Fatal(err)
	}
	if err := SetMatch(d, 2, Game{}); err != nil { // "no match"
		t.Fatal(err)
	}
	if err := SetMatch(d, 99, Game{}); !errors.Is(err, ErrUnknownGame) {
		t.Errorf("unknown game: %v", err)
	}
	if _, err := Refresh(context.Background(), d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	l, _ := Load(d)
	if len(l.Prices) != 1 || l.Prices[0].GameID != 1 {
		t.Errorf("prices %+v", l.Prices)
	}
	if src.lookups != 0 {
		t.Errorf("the owner's choices must not be searched: %d", src.lookups)
	}
	// Resetting a choice lets the next refresh search again; changing it drops the old prices.
	if err := ClearMatch(d, 2); err != nil {
		t.Fatal(err)
	}
	Refresh(context.Background(), d, src, Options{})
	l, _ = Load(d)
	if len(l.Matches) != 2 || l.Matches[1].Source != "title" || l.Matches[1].ITADID != "m" {
		t.Errorf("matches %+v", l.Matches)
	}
	SetMatch(d, 1, Game{})
	if l, _ = Load(d); len(l.Prices) != 1 || l.Prices[0].GameID != 2 {
		t.Errorf("old prices must go with the old match: %+v", l.Prices)
	}
}

func TestRefreshStopsOnRateLimit(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "A", "backlog", "")
	src := &fakeSource{byTitle: map[string]Game{"A": {ID: "a", Title: "A"}}, err: &RateLimitError{RetryAfter: time.Second}}
	rep, err := Refresh(context.Background(), d, src, Options{})
	if err != nil || !strings.Contains(rep.Stopped, "rate limit") {
		t.Fatalf("%+v %v", rep, err)
	}
	if l, _ := Load(d); len(l.Matches) != 1 { // the match found before the limit is kept
		t.Errorf("matches %+v", l.Matches)
	}
	src.err = errors.New("boom")
	if _, err := Refresh(context.Background(), d, src, Options{}); err == nil {
		t.Error("other errors fail the run")
	}
	if _, err := Refresh(context.Background(), d, New(""), Options{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("missing key: %v", err)
	}
}

// hookSource runs hook from inside Overview, the way the owner would act while a refresh is waiting for ITAD.
type hookSource struct {
	*fakeSource
	hook func()
}

func (h *hookSource) Overview(ctx context.Context, ids []string, country string) (map[string]Overview, error) {
	if h.hook != nil {
		h.hook()
	}
	return h.fakeSource.Overview(ctx, ids, country)
}

func priceCount(d *sql.DB) int {
	var n int
	d.QueryRow(`SELECT count(*) FROM deal_price`).Scan(&n)
	return n
}

func TestRefreshDoesNotOverwriteWhatTheOwnerDidMeanwhile(t *testing.T) {
	cheap := Overview{Current: &Current{Shop: "Steam", Price: eur(100), Regular: eur(200), Cut: 50}}
	setup := func(t *testing.T) (*sql.DB, *hookSource) {
		d := newDB(t)
		addGame(t, d, 1, "Alpha", "backlog", "")
		src := &hookSource{fakeSource: &fakeSource{byTitle: map[string]Game{"Alpha": {ID: "auto", Title: "Alpha"}},
			prices: map[string]Overview{"auto": cheap, "picked": {Current: &Current{Shop: "GOG", Price: eur(999), Regular: eur(999)}}}}}
		return d, src
	}

	t.Run("a match picked during the run", func(t *testing.T) {
		d, src := setup(t)
		src.hook = func() {
			SetMatch(d, 1, Game{ID: "picked", Title: "Alpha Deluxe"})
			RefreshOne(context.Background(), d, src.fakeSource, 1)
		}
		rep, err := Refresh(context.Background(), d, src, Options{})
		if err != nil || rep.Priced != 0 {
			t.Fatalf("%+v %v", rep, err)
		}
		l, _ := Load(d)
		if len(l.Prices) != 1 || *l.Prices[0].Price != 999 || l.Matches[0].ITADID != "picked" {
			t.Errorf("the owner's pick and its price must stay: %+v %+v", l.Matches, l.Prices)
		}
	})

	t.Run("a match picked before the run reaches the game", func(t *testing.T) {
		d, src := setup(t)
		src.fakeSource.byTitle["Alpha"] = Game{ID: "auto", Title: "Alpha"}
		// The lookup finds "auto", but by the time it is stored the owner has chosen "picked".
		lk := &lookupHook{hookSource: src, before: func() { SetMatch(d, 1, Game{ID: "picked", Title: "Alpha Deluxe"}) }}
		rep, err := Refresh(context.Background(), d, lk, Options{})
		if err != nil {
			t.Fatal(err)
		}
		l, _ := Load(d)
		if l.Matches[0].ITADID != "picked" || rep.Priced != 1 || len(l.Prices) != 1 || *l.Prices[0].Price != 999 {
			t.Errorf("%+v %+v %+v", rep, l.Matches, l.Prices)
		}
	})

	t.Run("a match reset during the run", func(t *testing.T) {
		d, src := setup(t)
		src.hook = func() { ClearMatch(d, 1) }
		if _, err := Refresh(context.Background(), d, src, Options{}); err != nil {
			t.Fatal(err)
		}
		if n := priceCount(d); n != 0 {
			t.Errorf("a game with no match must get no price: %d", n)
		}
	})

	t.Run("the country changed during the run", func(t *testing.T) {
		d, src := setup(t)
		src.hook = func() { SetCountry(d, "US") }
		rep, err := Refresh(context.Background(), d, src, Options{})
		if err != nil || rep.Priced != 0 || priceCount(d) != 0 {
			t.Errorf("prices of the old country must not be saved: %+v %v %d", rep, err, priceCount(d))
		}
	})

	t.Run("RefreshOne guards the same way", func(t *testing.T) {
		d, src := setup(t)
		SetMatch(d, 1, Game{ID: "auto", Title: "Alpha"})
		src.hook = func() { SetMatch(d, 1, Game{ID: "picked", Title: "Alpha Deluxe"}) }
		if err := RefreshOne(context.Background(), d, src, 1); err != nil {
			t.Fatal(err)
		}
		if n := priceCount(d); n != 0 {
			t.Errorf("the price of the old match must not be saved: %d", n)
		}
		src.hook = func() { SetCountry(d, "FR") }
		SetMatch(d, 1, Game{ID: "auto", Title: "Alpha"})
		RefreshOne(context.Background(), d, src, 1)
		if n := priceCount(d); n != 0 {
			t.Errorf("the price of the old country must not be saved: %d", n)
		}
	})
}

// lookupHook runs before a title lookup answers.
type lookupHook struct {
	*hookSource
	before func()
}

func (l *lookupHook) LookupTitle(ctx context.Context, t string) (Game, bool, error) {
	g, ok, err := l.hookSource.LookupTitle(ctx, t)
	l.before()
	return g, ok, err
}

func TestRefreshBatchesAndSharedIDs(t *testing.T) {
	d := newDB(t)
	src := &fakeSource{byTitle: map[string]Game{}, prices: map[string]Overview{}}
	for i := int64(1); i <= 120; i++ {
		title := "G" + string(rune('A'+i%26)) + strings.Repeat("x", int(i))
		addGame(t, d, i, title, "backlog", "")
		src.byTitle[title] = Game{ID: title, Title: title}
	}
	addGame(t, d, 200, "Twin", "backlog", "")
	addGame(t, d, 201, "Twin", "backlog", "")
	src.byTitle["Twin"] = Game{ID: "twin", Title: "Twin"}
	var last [2]int
	rep, err := Refresh(context.Background(), d, src, Options{Progress: func(done, total int) { last = [2]int{done, total} }})
	if err != nil || rep.Priced != 122 {
		t.Fatalf("%+v %v", rep, err)
	}
	if len(src.overview) != 3 || len(src.overview[0]) != MaxBatch || last[0] != last[1] {
		t.Errorf("batches %d, progress %v", len(src.overview), last)
	}
}

func TestCountry(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "A", "backlog", "")
	if Country(d) != "ES" {
		t.Fatal("default country")
	}
	if err := SetMatch(d, 1, Game{ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := savePrice(d, 1, "x", "ES", Overview{Current: &Current{Price: eur(1), Regular: eur(2)}}); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if err := SetCountry(d, "es"); err == nil {
		t.Error("lower case is refused here; the API upper-cases first")
	}
	if err := SetCountry(d, "ES"); err != nil {
		t.Fatal(err)
	}
	if l, _ := Load(d); len(l.Prices) != 0 {
		// same country: nothing is dropped (the row was added after the default)
		t.Log("kept")
	}
	if err := SetCountry(d, "US"); err != nil || Country(d) != "US" {
		t.Fatal(err)
	}
	if l, _ := Load(d); len(l.Prices) != 0 {
		t.Errorf("prices of the old country must go: %+v", l.Prices)
	}
}

func TestSameTitle(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Hollow Knight", "hollow knight", true},
		{"Baldur’s Gate 3", "Baldur's Gate 3", true},
		{"Tom & Jerry", "Tom and Jerry", true},
		{"Disco Elysium", "Disco Elysium - The Final Cut", false},
		{"Hades", "Hades II", false},
		{"", "", false},
	} {
		if got := SameTitle(c.a, c.b); got != c.want {
			t.Errorf("SameTitle(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
