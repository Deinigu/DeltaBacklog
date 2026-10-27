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
	links map[int64]meta.GameLinks
	games map[int64][]meta.CollectionGame
	asked []int64 // collection ids read
}

func (f *fake) GameLinks(_ context.Context, ids []int64) (map[int64]meta.GameLinks, error) {
	out := map[int64]meta.GameLinks{}
	for _, id := range ids {
		if l, ok := f.links[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}

func (f *fake) CollectionGames(_ context.Context, id int64) (string, []meta.CollectionGame, error) {
	f.asked = append(f.asked, id)
	return map[int64]string{7: "Zed Series", 8: "Other", 302: "Ace Attorney"}[id], f.games[id], nil
}

func zed() meta.GameLinks {
	return meta.GameLinks{Collections: []meta.CollectionRef{{ID: 7, Name: "Zed Series"}}}
}
func other() meta.GameLinks {
	return meta.GameLinks{Collections: []meta.CollectionRef{{ID: 8, Name: "Other"}}}
}

func newFake() *fake {
	return &fake{
		links: map[int64]meta.GameLinks{100: zed(), 101: zed(), 300: other()},
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
	if s.Missing[0].V != "c102" { // the poster URL's cache key follows IGDB's image
		t.Errorf("cover version = %q", s.Missing[0].V)
	}
}

// IGDB splits Ace Attorney into several entries per game: the owned DS entry (an expanded game, so not even in the
// collection list), its JP original (its parent) and the 2013 remaster (its child). Only really missing games are listed.
func TestOwnedGamesFamilyIsNotMissing(t *testing.T) {
	d := open(t)
	addGame(t, d, "Phoenix Wright: Ace Attorney", "Ace Attorney", 1426)
	addGame(t, d, "Justice for All", "Ace Attorney", 1427)
	f := &fake{
		links: map[int64]meta.GameLinks{
			1426: {Collections: []meta.CollectionRef{{ID: 302, Name: "Ace Attorney"}}, ParentID: 221280},
			1427: {Collections: []meta.CollectionRef{{ID: 302, Name: "Ace Attorney"}}, ParentID: 221289},
		},
		games: map[int64][]meta.CollectionGame{302: {
			{IGDBID: 221280, Name: "Gyakuten Saiban", Year: 2001, CoverID: "a1"},                                           // the parent of an owned game
			{IGDBID: 221285, Name: "Phoenix Wright: Ace Attorney", Year: 2013, GameType: 9, ParentID: 1426, CoverID: "a2"}, // a remaster of an owned game
			{IGDBID: 221289, Name: "Gyakuten Saiban 2", Year: 2002, CoverID: "a3"},
			{IGDBID: 221286, Name: "Justice for All", Year: 2013, GameType: 9, ParentID: 1427, CoverID: "a4"},
			{IGDBID: 230001, Name: "Ace Attorney Chronicles sibling", Year: 2013, GameType: 9, ParentID: 221280, CoverID: "a6"}, // shares a parent with an owned game
			{IGDBID: 222000, Name: "Dual Destinies", Year: 2013, CoverID: "a5"},                                                 // really missing
		}},
	}
	o, fetched := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	list, _ := Load(d)
	if len(list) != 1 || len(list[0].Missing) != 1 || list[0].Missing[0].Title != "Dual Destinies" {
		t.Fatalf("missing: %+v", list)
	}
	if len(*fetched) != 1 || (*fetched)[0] != 222000 { // no poster is downloaded for the owned family
		t.Errorf("fetched %v", *fetched)
	}
	roots, err := OwnedRoots(d)
	if err != nil || len(roots) != 2 || roots[0] != 221280 || roots[1] != 221289 {
		t.Errorf("owned roots %v, %v", roots, err)
	}
}

// pick fills game_collection for one series by hand and asks which collections the series gets.
func pick(t *testing.T, series, igdbSeries string, games [][]meta.CollectionRef) []int64 {
	t.Helper()
	d := open(t)
	for i, cols := range games {
		id := addGame(t, d, fmt.Sprintf("G%d", i), series, int64(1000+i))
		if igdbSeries != "" {
			d.Exec(`UPDATE game SET igdb_series = ? WHERE id = ?`, igdbSeries, id)
		}
		tx, _ := d.Begin()
		if err := StoreGameLinks(tx, id, meta.GameLinks{Collections: cols}); err != nil {
			t.Fatal(err)
		}
		tx.Commit()
	}
	p, err := pickCollections(d)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, c := range p[Key(series)] {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestPickCollections(t *testing.T) {
	zelda := meta.CollectionRef{ID: 1, Name: "The Legend of Zelda"}
	oot := meta.CollectionRef{ID: 2, Name: "Ocarina of Time"}
	zeldaMain := meta.CollectionRef{ID: 5, Name: "The Legend of Zelda"}
	mario := meta.CollectionRef{ID: 3, Name: "Super Mario"}
	kart := meta.CollectionRef{ID: 4, Name: "Mario Kart"}
	cases := []struct {
		name       string
		series, is string
		games      [][]meta.CollectionRef
		want       []int64
	}{
		{"the collection most games share wins", "Zelda", "", [][]meta.CollectionRef{{oot, zelda}, {zelda}}, []int64{1}},
		{"order inside IGDB's list does not matter", "Zelda", "", [][]meta.CollectionRef{{zelda, oot}, {zelda}}, []int64{1}},
		{"a tie goes to the collection named like the series", "The Legend of Zelda", "", [][]meta.CollectionRef{{oot, zelda}}, []int64{1}},
		{"a broad collection listed first does not win a tie against the series' name", "Mario Kart", "", [][]meta.CollectionRef{{mario, kart}}, []int64{4}},
		{"or like the IGDB series of a game", "My karts", "Mario Kart", [][]meta.CollectionRef{{mario, kart}}, []int64{4}},
		{"a tie nothing is named like keeps both", "Plumbers", "", [][]meta.CollectionRef{{mario, kart}}, []int64{3, 4}},
		// IGDB's first collection of Ocarina is its own small one; a series called "Zelda" must still get the whole series
		{"the user's own name beats igdb_series (Zelda with only Ocarina linked)", "Zelda", "Ocarina of Time", [][]meta.CollectionRef{{oot, zeldaMain}}, []int64{5}},
		{"igdb_series decides only when the series name matches nothing", "My games", "Ocarina of Time", [][]meta.CollectionRef{{oot, zeldaMain}}, []int64{2}},
		{"no collections, no pick", "Solo", "", [][]meta.CollectionRef{{}}, nil},
	}
	for _, c := range cases {
		got := pick(t, c.series, c.is, c.games)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
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
	// a refresh does not bring it back
	if _, err := Refresh(context.Background(), d, newFake(), o); err != nil {
		t.Fatal(err)
	}
	list, _ = Load(d)
	if len(list[0].Hidden) != 1 {
		t.Errorf("after refresh: %+v", list[0])
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

// A finished refresh drops the collections no series uses (a game unlinked or moved), the poster files nothing names,
// and the poster of a game that has become owned.
func TestRefreshPrunesWhatNothingUses(t *testing.T) {
	d := open(t)
	zedID := addGame(t, d, "Zed", "Zeds", 100)
	otherID := addGame(t, d, "Other", "Others", 300)
	f := newFake()
	o, _ := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	n := func(q string) (c int) { d.QueryRow(q).Scan(&c); return }
	if n(`SELECT count(*) FROM igdb_collection`) != 2 {
		t.Fatal("both collections should be cached")
	}
	stray := filepath.Join(o.CoverDir, "i999.jpg") // nothing names it, and it is old
	os.WriteFile(stray, []byte("x"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(stray, old, old)
	keep := filepath.Join(o.CoverDir, "p5.jpg") // a game's own poster is none of the cache's business
	os.WriteFile(keep, []byte("x"), 0o644)
	// the user adds Zed III: the cache no longer needs its poster
	addGame(t, d, "Zed III", "Zeds", 102)
	// the second game is unlinked: its collection is unused
	d.Exec(`DELETE FROM game_collection WHERE game_id = ?`, otherID)
	d.Exec(`UPDATE game SET igdb_id = NULL, igdb_parent_id = NULL WHERE id = ?`, otherID)
	_ = zedID
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	if n(`SELECT count(*) FROM igdb_collection`) != 1 || n(`SELECT count(*) FROM igdb_collection WHERE id = 8`) != 0 {
		t.Error("the collection no series points at was kept")
	}
	for _, name := range []string{stray, filepath.Join(o.CoverDir, "i102.jpg")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("%s was kept", filepath.Base(name))
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("a game's own poster was deleted")
	}
	if n(`SELECT count(*) FROM igdb_collection_game WHERE igdb_id = 102 AND cover_file IS NOT NULL`) != 0 {
		t.Error("the cache still names the poster of an owned game")
	}
}

// The progress covers the poster phase too: it ends at total, and the total grows by the posters to download.
func TestProgressIncludesThePosters(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	o, _ := opts(t)
	var last [2]int
	var beyond bool
	o.Progress = func(done, total int) {
		last = [2]int{done, total}
		if done > 1 {
			beyond = true // more than the one collection: the poster phase is reported
		}
	}
	if _, err := Refresh(context.Background(), d, newFake(), o); err != nil {
		t.Fatal(err)
	}
	if last != [2]int{3, 3} || !beyond { // 1 collection + 2 posters (101 and 102)
		t.Errorf("last progress %v, poster phase reported: %v", last, beyond)
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

// A chain of versions: the JP original <- the DS entry (an expanded game, in no collection list) <- the 2013 remaster
// and a port. Owning only the remaster still makes the whole chain owned.
func TestOwningTheRemasterOfAnExpandedEntryOwnsTheChain(t *testing.T) {
	d := open(t)
	addGame(t, d, "Phoenix Wright (HD)", "Ace Attorney", 221285)
	f := &fake{
		links: map[int64]meta.GameLinks{
			221285: {Collections: []meta.CollectionRef{{ID: 302, Name: "Ace Attorney"}}, ParentID: 1426},
			1426:   {ParentID: 221280}, // the expanded entry points at the JP original; IGDB lists it in no collection
			221280: {},
		},
		games: map[int64][]meta.CollectionGame{302: {
			{IGDBID: 221280, Name: "Gyakuten Saiban", Year: 2001, CoverID: "a1"},
			{IGDBID: 221285, Name: "Phoenix Wright: Ace Attorney", Year: 2013, GameType: 9, ParentID: 1426, CoverID: "a2"},
			{IGDBID: 221282, Name: "Phoenix Wright: Ace Attorney", Year: 2012, GameType: 11, ParentID: 1426, CoverID: "a3"}, // a port
			{IGDBID: 222000, Name: "Dual Destinies", Year: 2013, CoverID: "a5"},
		}},
	}
	o, fetched := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	list, _ := Load(d)
	if len(list) != 1 || len(list[0].Missing) != 1 || list[0].Missing[0].Title != "Dual Destinies" {
		t.Fatalf("missing: %+v", list)
	}
	if len(*fetched) != 1 {
		t.Errorf("fetched %v", *fetched)
	}
	var root int64
	d.QueryRow(`SELECT igdb_parent_id FROM game WHERE igdb_id = 221285`).Scan(&root)
	if root != 221280 { // the root, not the nearest parent
		t.Errorf("stored root = %d", root)
	}
}

// Standalone expansions and remakes are games of their own (the parent link the meta client never stores for them):
// owning Uncharted: The Lost Legacy does not own Uncharted 4, nor the other way round; owning Resident Evil 2 (2019)
// does not own the 1998 game or its remaster.
func TestExpansionsAndRemakesAreNotTheSameGame(t *testing.T) {
	for _, c := range []struct {
		name         string
		owned        int64
		listed, gone []int64
	}{
		{"own the Lost Legacy", 26193, []int64{7331}, []int64{26193}},
		{"own Uncharted 4", 7331, []int64{26193}, []int64{7331}},
		{"own Resident Evil 2 (2019)", 19686, []int64{880, 396728}, []int64{19686}},
	} {
		d := open(t)
		addGame(t, d, "Owned", "Series", c.owned)
		f := &fake{
			links: map[int64]meta.GameLinks{c.owned: {Collections: []meta.CollectionRef{{ID: 9, Name: "Series"}}}}, // no parent: the client drops it for these types
			games: map[int64][]meta.CollectionGame{9: {
				{IGDBID: 7331, Name: "Uncharted 4", Year: 2016}, {IGDBID: 26193, Name: "The Lost Legacy", Year: 2017, GameType: 4},
				{IGDBID: 880, Name: "Resident Evil 2", Year: 1998}, {IGDBID: 19686, Name: "Resident Evil 2", Year: 2019, GameType: 8},
				{IGDBID: 396728, Name: "Resident Evil 2 (remaster)", Year: 2020, GameType: 9, ParentID: 880},
			}},
		}
		o, _ := opts(t)
		if _, err := Refresh(context.Background(), d, f, o); err != nil {
			t.Fatal(err)
		}
		list, _ := Load(d)
		in := map[int64]bool{}
		for _, g := range list[0].Missing {
			in[g.IGDBID] = true
		}
		for _, id := range c.listed {
			if !in[id] {
				t.Errorf("%s: game %d is not listed as missing", c.name, id)
			}
		}
		for _, id := range c.gone {
			if in[id] {
				t.Errorf("%s: game %d is listed although the library owns it", c.name, id)
			}
		}
	}
}

// A file written a moment ago may be a download of another process that has not been recorded yet: the sweep leaves it.
func TestSweepSkipsFreshFiles(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	o, _ := opts(t)
	young := filepath.Join(o.CoverDir, "i777.jpg")
	os.WriteFile(young, []byte("x"), 0o644)
	if _, err := Refresh(context.Background(), d, newFake(), o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("a fresh unnamed poster file was swept")
	}
}
