package collections

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// fake is an offline IGDB: Zed Series (collection 7) has four real games, Other (8) one.
type fake struct {
	collOf map[int64]meta.CollectionRef
	games  map[int64][]meta.CollectionGame
	asked  []int64 // collection ids read
}

func (f *fake) CollectionsOf(_ context.Context, ids []int64) (map[int64]meta.CollectionRef, error) {
	out := map[int64]meta.CollectionRef{}
	for _, id := range ids {
		if r, ok := f.collOf[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}

func (f *fake) CollectionGames(_ context.Context, id int64) (string, []meta.CollectionGame, error) {
	f.asked = append(f.asked, id)
	return map[int64]string{7: "Zed Series", 8: "Other"}[id], f.games[id], nil
}

func newFake() *fake {
	return &fake{
		collOf: map[int64]meta.CollectionRef{100: {ID: 7, Name: "Zed Series"}, 101: {ID: 7, Name: "Zed Series"}, 300: {ID: 8, Name: "Other"}},
		games: map[int64][]meta.CollectionGame{
			7: {{IGDBID: 100, Name: "Zed", Year: 1997, CoverID: "c100"}, {IGDBID: 101, Name: "Zed II", Year: 2005, GameType: 8, CoverID: "c101"},
				{IGDBID: 102, Name: "Zed III", Year: 2011, CoverID: "c102"}, {IGDBID: 103, Name: "Zed Origins"}},
			8: {{IGDBID: 300, Name: "Other One", Year: 2001}},
		},
	}
}

func open(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func addGame(t *testing.T, d *sql.DB, title, series string, igdb int64) int64 {
	t.Helper()
	var sid, ig any
	if series != "" {
		id, err := db.GetOrCreate(d, "series", series)
		if err != nil {
			t.Fatal(err)
		}
		sid = id
	}
	if igdb > 0 {
		ig = igdb
	}
	res, err := d.Exec(`INSERT INTO game(title, title_sort, status_id, series_id, igdb_id) VALUES (?, lower(?), 4, ?, ?)`, title, title, sid, ig)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// opts writes a fake poster per game and lists the IGDB ids it was asked for.
func opts(t *testing.T) (Options, *[]int64) {
	var fetched []int64
	return Options{CoverDir: t.TempDir(), Fetch: func(url, dir string, igdbID int64) (string, error) {
		fetched = append(fetched, igdbID)
		name := fmt.Sprintf("i%d.jpg", igdbID)
		return name, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}}, &fetched
}

// The user's series is called "Zeds", IGDB's collection "Zed Series": the linked game decides.
func TestRefreshAndLoad(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	addGame(t, d, "Zed II", "Zeds", 101) // also in the library: must not be "not registered"
	addGame(t, d, "Lonely", "Solo", 0)   // a series without a linked game: nothing
	addGame(t, d, "No series", "", 300)  // a linked game outside any series: its collection is not read
	f := newFake()
	o, fetched := opts(t)
	rep, err := Refresh(context.Background(), d, f, o)
	if err != nil || rep.Stopped != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if len(f.asked) != 1 || f.asked[0] != 7 {
		t.Errorf("collections read: %v", f.asked)
	}
	// only 102 has a cover to fetch: 100 and 101 are in the library, 103 has no cover image
	if len(*fetched) != 1 || (*fetched)[0] != 102 || rep.Covers != 1 {
		t.Errorf("fetched %v, report %+v", *fetched, rep)
	}
	list, err := Load(d)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	s := list[0]
	if s.Key != "zeds" || len(s.Collections) != 1 || s.Collections[0].Name != "Zed Series" {
		t.Errorf("series: %+v", s)
	}
	if len(s.Missing) != 2 || s.Missing[0].IGDBID != 102 || s.Missing[1].IGDBID != 103 { // release order, unknown year last
		t.Fatalf("missing: %+v", s.Missing)
	}
	if s.Missing[0].Cover == nil || s.Missing[1].Cover != nil {
		t.Errorf("covers: %+v", s.Missing)
	}
}

// A game the user adds later disappears from "not registered" without another refresh (matched on igdb_id).
func TestAddedGameLeavesTheMissingList(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	o, _ := opts(t)
	if _, err := Refresh(context.Background(), d, newFake(), o); err != nil {
		t.Fatal(err)
	}
	list, _ := Load(d)
	if len(list[0].Missing) != 3 {
		t.Fatalf("missing: %+v", list[0].Missing)
	}
	addGame(t, d, "Zed III", "Zeds", 102)
	list, _ = Load(d)
	for _, g := range list[0].Missing {
		if g.IGDBID == 102 {
			t.Error("a game in the library is still listed as missing")
		}
	}
}

func TestHideAndUnhide(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	o, _ := opts(t)
	if _, err := Refresh(context.Background(), d, newFake(), o); err != nil {
		t.Fatal(err)
	}
	if err := Hide(d, 7, 102); err != nil {
		t.Fatal(err)
	}
	if err := Hide(d, 7, 102); err != nil { // twice is fine
		t.Fatal(err)
	}
	if err := Hide(d, 7, 555); err != ErrUnknown {
		t.Errorf("unknown game: %v", err)
	}
	list, _ := Load(d)
	if len(list[0].Missing) != 2 || len(list[0].Hidden) != 1 || list[0].Hidden[0].IGDBID != 102 {
		t.Errorf("after hide: %+v", list[0])
	}
	if err := Unhide(d, 7, 102); err != nil {
		t.Fatal(err)
	}
	list, _ = Load(d)
	if len(list[0].Missing) != 3 || len(list[0].Hidden) != 0 {
		t.Errorf("after unhide: %+v", list[0])
	}
}

// A second refresh keeps the posters it has, and a game that left the collection loses its file.
func TestRefreshKeepsAndCleansPosters(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	f := newFake()
	o, fetched := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(o.CoverDir, "i102.jpg")
	if _, err := os.Stat(poster); err != nil {
		t.Fatal("poster not written")
	}
	*fetched = nil
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	if len(*fetched) != 0 {
		t.Errorf("downloaded again: %v", *fetched)
	}
	f.games[7] = f.games[7][:1] // IGDB dropped the other games
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(poster); !os.IsNotExist(err) {
		t.Error("the poster of a game that left the collection was kept")
	}
}

func TestOnlyIGDBImageHostsAreFetched(t *testing.T) {
	if !igdbImageHost("https://images.igdb.com/igdb/image/upload/t_cover_big/x.jpg") || igdbImageHost("http://images.igdb.com/x") || igdbImageHost("https://evil.example/x") {
		t.Error("host check")
	}
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	var called bool
	rep, err := Refresh(context.Background(), d, newFake(), Options{CoverDir: t.TempDir(), Allowed: func(string) bool { return false },
		Fetch: func(string, string, int64) (string, error) { called = true; return "", nil }})
	if err != nil || called || rep.CoverErrors == 0 {
		t.Errorf("fetched a disallowed url: %v %+v %v", called, rep, err)
	}
}

// flaky fails the read of collection 8.
type flaky struct {
	*fake
	fail error
}

func (f flaky) CollectionGames(ctx context.Context, id int64) (string, []meta.CollectionGame, error) {
	if id == 8 {
		return "", nil, f.fail
	}
	return f.fake.CollectionGames(ctx, id)
}

// A collection IGDB failed on does not undo the ones read before it.
func TestFailureKeepsOtherCollections(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	addGame(t, d, "Other", "Others", 300)
	o, _ := opts(t)
	rep, err := Refresh(context.Background(), d, flaky{newFake(), os.ErrDeadlineExceeded}, o)
	if err != nil || rep.Collections != 1 || rep.Errors != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	list, _ := Load(d)
	if len(list) != 1 || list[0].Key != "zeds" {
		t.Errorf("list: %+v", list)
	}
}

// A 429 is waited out a few times; when IGDB keeps refusing the run stops and what was stored stays.
func TestRateLimitStopsTheRun(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	addGame(t, d, "Other", "Others", 300)
	o, _ := opts(t)
	var sleeps int
	o.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	rep, err := Refresh(context.Background(), d, flaky{newFake(), &meta.HTTPError{Status: 429, Text: "429"}}, o)
	if err != nil || rep.Stopped == "" || rep.Collections != 1 || sleeps != 3 {
		t.Fatalf("%+v %v (sleeps %d)", rep, err, sleeps)
	}
}
