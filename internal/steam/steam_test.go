package steam

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

const (
	secret  = "SEKRIT0KEY0123456789ABCDEF012345"
	account = "76561198000000001"
)

// fakeSteam is an offline Steam Web API. It records every request URL so a test can look for the key in places it must not be.
type fakeSteam struct {
	srv     *httptest.Server
	hits    int
	urls    []string
	owned   string // the GetOwnedGames body
	vanity  string // the ResolveVanityURL body
	status  int    // forced status for every request when not 0
	private bool
}

func newFake(t *testing.T) (*fakeSteam, *Client) {
	t.Helper()
	f := &fakeSteam{owned: `{"response":{"game_count":0,"games":[]}}`, vanity: `{"response":{"success":42}}`}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		f.urls = append(f.urls, r.URL.String())
		if r.URL.Query().Get("key") != secret {
			http.Error(w, "<html>Forbidden</html>", http.StatusForbidden)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		switch r.URL.Path {
		case "/ISteamUser/ResolveVanityURL/v1/":
			w.Write([]byte(f.vanity))
		case "/IPlayerService/GetOwnedGames/v1/":
			q := r.URL.Query()
			if q.Get("steamid") != account || q.Get("include_appinfo") != "1" || q.Get("include_played_free_games") != "1" {
				t.Errorf("GetOwnedGames query %s", r.URL.RawQuery)
			}
			w.Write([]byte(f.owned))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	t.Cleanup(f.srv.Close)
	c := New(secret, account)
	c.BaseURL, c.Gap = f.srv.URL, 0
	return f, c
}

func TestOwnedReadsGamesAndHours(t *testing.T) {
	f, c := newFake(t)
	f.owned = `{"response":{"game_count":3,"games":[
		{"appid":367520,"name":"Hollow Knight","playtime_forever":125,"rtime_last_played":1700000000},
		{"appid":1,"name":"  ","playtime_forever":5},
		{"appid":620,"name":"Portal 2","playtime_forever":0}]}}`
	apps, err := c.Owned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0] != (App{367520, "Hollow Knight", 125, 1700000000}) || apps[1].Playtime != 0 || apps[1].LastPlayed != 0 {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestPrivateProfileHasAClearError(t *testing.T) {
	f, c := newFake(t)
	f.owned = `{"response":{}}`
	if _, err := c.Owned(context.Background()); !errors.Is(err, ErrPrivate) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(ErrPrivate.Error(), "Public") {
		t.Error("the message must say what to change")
	}
}

func TestPartialListIsRefused(t *testing.T) {
	f, c := newFake(t)
	f.owned = `{"response":{"game_count":5,"games":[{"appid":10,"name":"A","playtime_forever":1}]}}`
	if _, err := c.Owned(context.Background()); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("err = %v", err)
	}
}

// A bad key answers 403 and a dead server fails at the network: neither error may carry the key or the request URL.
func TestErrorsNeverHoldTheKey(t *testing.T) {
	_, c := newFake(t)
	c.key = "WRONG0" + secret
	_, err := c.Owned(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refused the API key") {
		t.Fatalf("bad key: %v", err)
	}
	for _, key := range []string{c.key, secret} {
		if strings.Contains(err.Error(), key) {
			t.Errorf("error holds the key: %v", err)
		}
	}

	dead, c2 := newFake(t)
	dead.srv.Close() // connection refused: Go's own error would print the full URL with ?key=...
	_, err = c2.Owned(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "key=") || strings.Contains(err.Error(), "http") {
		t.Fatalf("network error: %v", err)
	}

	f3, c3 := newFake(t)
	f3.status = http.StatusTooManyRequests
	var rl *RateLimitError
	if _, err := c3.Owned(context.Background()); !errors.As(err, &rl) {
		t.Fatalf("429: %v", err)
	}
	f3.status = http.StatusInternalServerError
	if _, err := c3.Owned(context.Background()); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("500: %v", err)
	}
}

func TestVanityNameIsResolvedOnce(t *testing.T) {
	f, c := newFake(t)
	c.account = "mygamer"
	f.vanity = `{"response":{"success":1,"steamid":"` + account + `"}}`
	f.owned = `{"response":{"game_count":1,"games":[{"appid":10,"name":"A","playtime_forever":1}]}}`
	for i := 0; i < 2; i++ {
		if apps, err := c.Owned(context.Background()); err != nil || len(apps) != 1 {
			t.Fatalf("run %d: %v %v", i, apps, err)
		}
	}
	vanityCalls := 0
	for _, u := range f.urls {
		if strings.Contains(u, "ResolveVanityURL") {
			vanityCalls++
			if !strings.Contains(u, "vanityurl=mygamer") {
				t.Errorf("vanity call %s", u)
			}
		}
	}
	if vanityCalls != 1 {
		t.Errorf("vanity resolved %d times, want once", vanityCalls)
	}

	f.vanity = `{"response":{"success":42,"message":"No match"}}`
	c.resolved, c.account = "", "nobody"
	if _, err := c.Owned(context.Background()); err == nil || !strings.Contains(err.Error(), "STEAM_ID") {
		t.Fatalf("unknown vanity: %v", err)
	}
}

func TestAccountSpellings(t *testing.T) {
	for in, want := range map[string]string{
		"76561198000000001":                             "76561198000000001",
		" mygamer ":                                     "mygamer",
		"https://steamcommunity.com/id/mygamer/":        "mygamer",
		"steamcommunity.com/profiles/76561198000000001": "76561198000000001",
	} {
		if got := cleanAccount(in); got != want {
			t.Errorf("cleanAccount(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOnlyTheSteamHostIsCalled(t *testing.T) {
	c := New(secret, account)
	c.BaseURL = "https://evil.example"
	if _, err := c.Owned(context.Background()); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
	if New("", account).Configured() || New(secret, "").Configured() || !New(secret, account).Configured() {
		t.Error("Configured needs both the key and the account")
	}
	if _, err := New("", "").Owned(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v", err)
	}
}

// ---------------- store ----------------

type fakeSource struct {
	apps []App
	err  error
}

func (f *fakeSource) Configured() bool                     { return true }
func (f *fakeSource) Owned(context.Context) ([]App, error) { return f.apps, f.err }

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, g := range []struct {
		id    int
		title string
	}{{1, "Hollow Knight"}, {2, "Portal 2"}, {3, "Dark Souls III"}, {4, "Twin"}, {5, "Twin"}, {6, "Linked Game"}} {
		if _, err := d.Exec(`INSERT INTO game(id, title, title_sort, status_id) VALUES (?, ?, lower(?), (SELECT id FROM status WHERE code = 'backlog'))`, g.id, g.title, g.title); err != nil {
			t.Fatal(err)
		}
	}
	d.Exec(`INSERT INTO game_link(game_id, kind, url) VALUES (6, 'steam', 'https://store.steampowered.com/app/9999/Different_Name/')`)
	return d
}

type hours struct {
	min  sql.NullInt64
	last sql.NullInt64
}

func gameHours(t *testing.T, d *sql.DB, id int) hours {
	t.Helper()
	var h hours
	if err := d.QueryRow(`SELECT playtime_min, steam_last_played FROM game WHERE id = ?`, id).Scan(&h.min, &h.last); err != nil {
		t.Fatal(err)
	}
	return h
}

func appRow(t *testing.T, d *sql.DB, id int64) Row {
	t.Helper()
	l, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range l.Apps {
		if r.AppID == id {
			return r
		}
	}
	t.Fatalf("app %d not stored", id)
	return Row{}
}

func gameOf(r Row) int64 {
	if r.GameID == nil {
		return 0
	}
	return *r.GameID
}

func TestSyncMatchesByLinkThenTitleAndStoresHours(t *testing.T) {
	d := testDB(t)
	src := &fakeSource{apps: []App{
		{367520, "Hollow Knight", 600, 1700000000},        // exact title
		{620, "Portal 2™", 90, 0},                         // title with a trademark sign
		{9999, "Something Else Entirely", 30, 1650000000}, // matched by the Steam link
		{1234, "Twin", 10, 0},                             // two library games share the title: no match
		{5555, "Not In The Library", 0, 0},
	}}
	rep, err := Sync(context.Background(), d, src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Owned != 5 || rep.Matched != 3 || rep.Unmatched != 2 || rep.Ignored != 0 {
		t.Fatalf("report %+v", rep)
	}
	for app, want := range map[int64]struct {
		game   int64
		source string
	}{367520: {1, "title"}, 620: {2, "title"}, 9999: {6, "link"}, 1234: {0, ""}, 5555: {0, ""}} {
		r := appRow(t, d, app)
		if gameOf(r) != want.game || (want.game != 0 && *r.Source != want.source) {
			t.Errorf("app %d: game %d source %v, want %+v", app, gameOf(r), r.Source, want)
		}
	}
	if h := gameHours(t, d, 1); h.min.Int64 != 600 || h.last.Int64 != 1700000000 {
		t.Errorf("Hollow Knight hours %+v", h)
	}
	if h := gameHours(t, d, 2); !h.min.Valid || h.min.Int64 != 90 || h.last.Valid {
		t.Errorf("Portal 2 hours %+v (played, never dated)", h)
	}
	if h := gameHours(t, d, 3); h.min.Valid {
		t.Errorf("an unmatched game has no hours: %+v", h)
	}
	if l, _ := Load(d); l.SyncedAt == "" {
		t.Error("the sync time is stored")
	}
}

func TestHoursNeverShrinkOnAPartialOrLowerRead(t *testing.T) {
	d := testDB(t)
	full := &fakeSource{apps: []App{{367520, "Hollow Knight", 600, 1700000000}, {620, "Portal 2", 90, 1600000000}}}
	if _, err := Sync(context.Background(), d, full); err != nil {
		t.Fatal(err)
	}
	// Steam answers with fewer minutes, and without Portal 2 at all.
	partial := &fakeSource{apps: []App{{367520, "Hollow Knight", 40, 1500000000}}}
	if _, err := Sync(context.Background(), d, partial); err != nil {
		t.Fatal(err)
	}
	if h := gameHours(t, d, 1); h.min.Int64 != 600 || h.last.Int64 != 1700000000 {
		t.Errorf("Hollow Knight shrank: %+v", h)
	}
	if h := gameHours(t, d, 2); h.min.Int64 != 90 {
		t.Errorf("Portal 2 lost its hours: %+v", h)
	}
	if r := appRow(t, d, 620); r.Playtime != 90 {
		t.Errorf("the missing app was changed: %+v", r)
	}
	// A failed read (private profile, bad key, no network) writes nothing at all.
	for _, e := range []error{ErrPrivate, errors.New("Steam could not be reached"), &RateLimitError{}} {
		if _, err := Sync(context.Background(), d, &fakeSource{err: e}); err == nil {
			t.Fatalf("%v: no error", e)
		}
	}
	if h := gameHours(t, d, 1); h.min.Int64 != 600 {
		t.Errorf("a failed read changed the hours: %+v", h)
	}
	// And more hours do raise them.
	if _, err := Sync(context.Background(), d, &fakeSource{apps: []App{{367520, "Hollow Knight", 700, 1800000000}}}); err != nil {
		t.Fatal(err)
	}
	if h := gameHours(t, d, 1); h.min.Int64 != 700 || h.last.Int64 != 1800000000 {
		t.Errorf("hours did not grow: %+v", h)
	}
}

func TestTheOwnersChoicesSurviveASync(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	src := &fakeSource{apps: []App{{367520, "Hollow Knight", 600, 0}, {620, "Portal 2", 90, 0}, {7, "Mystery", 20, 0}}}
	if _, err := Sync(ctx, d, src); err != nil {
		t.Fatal(err)
	}
	// Hollow Knight on Steam is really the library's "Dark Souls III"; Portal 2 is not in the library at all.
	if err := SetMatch(d, 367520, 3); err != nil {
		t.Fatal(err)
	}
	if err := SetNone(d, 620); err != nil {
		t.Fatal(err)
	}
	if err := SetIgnored(d, 7, true); err != nil {
		t.Fatal(err)
	}
	if h := gameHours(t, d, 1); h.min.Valid {
		t.Errorf("the game the app left keeps no hours: %+v", h)
	}
	if h := gameHours(t, d, 2); h.min.Valid {
		t.Errorf("no match, no hours: %+v", h)
	}
	if h := gameHours(t, d, 3); h.min.Int64 != 600 {
		t.Errorf("the chosen game gets the hours: %+v", h)
	}
	if _, err := Sync(ctx, d, src); err != nil {
		t.Fatal(err)
	}
	if r := appRow(t, d, 367520); gameOf(r) != 3 || *r.Source != "user" {
		t.Errorf("the owner's match changed: %+v", r)
	}
	if r := appRow(t, d, 620); r.GameID != nil || *r.Source != "none" {
		t.Errorf("the owner's \"none\" changed: %+v", r)
	}
	if r := appRow(t, d, 7); !r.Ignored || r.GameID != nil {
		t.Errorf("ignore lost: %+v", r)
	}
	if h := gameHours(t, d, 1); h.min.Valid {
		t.Errorf("a sync gave the hours back to the old match: %+v", h)
	}
	if rep, _ := Sync(ctx, d, src); rep.Ignored != 1 || rep.Unmatched != 1 || rep.Matched != 1 {
		t.Errorf("report %+v", rep)
	}

	// Ignoring is reversible; "match automatically" goes back to the title match and moves the hours with it.
	if err := SetIgnored(d, 7, false); err != nil || appRow(t, d, 7).Ignored {
		t.Errorf("unignore: %v", err)
	}
	if err := ClearMatch(d, 367520); err != nil {
		t.Fatal(err)
	}
	if r := appRow(t, d, 367520); gameOf(r) != 1 || *r.Source != "title" {
		t.Errorf("reset: %+v", r)
	}
	if h := gameHours(t, d, 1); h.min.Int64 != 600 {
		t.Errorf("hours back on game 1: %+v", h)
	}
	if h := gameHours(t, d, 3); h.min.Valid {
		t.Errorf("hours left game 3: %+v", h)
	}

	if err := SetMatch(d, 367520, 999); !errors.Is(err, ErrUnknownGame) {
		t.Errorf("unknown game: %v", err)
	}
	if err := SetMatch(d, 424242, 1); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("unknown app: %v", err)
	}
	if err := SetIgnored(d, 424242, true); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("unknown app ignore: %v", err)
	}
}

// Deleting a library game leaves the Steam app behind, unmatched, so the review list offers it again.
func TestDeletingAGameFreesItsApp(t *testing.T) {
	d := testDB(t)
	if _, err := Sync(context.Background(), d, &fakeSource{apps: []App{{367520, "Hollow Knight", 600, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM game WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if r := appRow(t, d, 367520); r.GameID != nil {
		t.Fatalf("still matched: %+v", r)
	}
	if rep, err := Sync(context.Background(), d, &fakeSource{apps: []App{{367520, "Hollow Knight", 600, 0}}}); err != nil || rep.Unmatched != 1 {
		t.Errorf("%+v %v", rep, err)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	d := testDB(t)
	rep, err := DryRun(context.Background(), d, &fakeSource{apps: []App{{367520, "Hollow Knight", 600, 0}}})
	if err != nil || rep.Owned != 1 || rep.Matched != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if l, _ := Load(d); len(l.Apps) != 0 || l.SyncedAt != "" {
		t.Errorf("dry run stored %+v", l)
	}
	if h := gameHours(t, d, 1); h.min.Valid {
		t.Errorf("dry run changed hours: %+v", h)
	}
}

func TestNotConfigured(t *testing.T) {
	d := testDB(t)
	if _, err := Sync(context.Background(), d, nil); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v", err)
	}
}
