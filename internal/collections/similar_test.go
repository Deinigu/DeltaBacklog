package collections

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// simFake is an offline IGDB for the Similar page: similar maps a source to what IGDB lists as similar to it.
type simFake struct {
	*fake
	similar map[int64][]meta.CollectionGame
	asked   [][]int64
}

func (f *simFake) SimilarGames(_ context.Context, ids []int64) (map[int64][]meta.CollectionGame, error) {
	f.asked = append(f.asked, ids)
	out := map[int64][]meta.CollectionGame{}
	for _, id := range ids {
		out[id] = f.similar[id]
	}
	return out, nil
}

func newSim() *simFake {
	return &simFake{fake: &fake{links: map[int64]meta.GameLinks{}}, similar: map[int64][]meta.CollectionGame{}}
}

// play puts a game on a status by code; fav marks it a favourite.
func play(t *testing.T, d *sql.DB, id int64, code string, fav bool) {
	t.Helper()
	if _, err := d.Exec(`UPDATE game SET status_id = (SELECT id FROM status WHERE code = ?), favorite = ? WHERE id = ?`, code, fav, id); err != nil {
		t.Fatal(err)
	}
}

func byID(list *SimilarList) map[int64]Suggestion {
	m := map[int64]Suggestion{}
	for _, s := range list.Suggestions {
		m[s.IGDBID] = s
	}
	return m
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Only finished and favourite linked games suggest; what the library owns (with its family) is dropped, the wishlist is
// reported for the page to mark, and a game suggested by two sources lists both.
func TestSimilarSourcesAndHide(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Finished", "", 1)
	b := addGame(t, d, "Favourite", "", 2)
	c := addGame(t, d, "Backlog", "", 3)  // not played: no source
	w := addGame(t, d, "Wished", "", 4)   // wishlist: no source, and wished for
	o := addGame(t, d, "Owned HD", "", 9) // the remaster of 8
	u := addGame(t, d, "Unlinked", "", 0) // not linked: no source
	play(t, d, a, "finished", false)
	play(t, d, b, "backlog", true)
	play(t, d, c, "backlog", false)
	play(t, d, w, "wishlist", true)
	play(t, d, o, "backlog", false)
	play(t, d, u, "finished", false)
	d.Exec(`UPDATE game SET igdb_parent_id = 8 WHERE id = ?`, o)

	f := newSim()
	f.similar[1] = []meta.CollectionGame{
		{IGDBID: 8, Name: "Original", Year: 2001, CoverID: "c8"}, // owned through its remaster
		{IGDBID: 10, Name: "Both", Year: 2010, CoverID: "c10"},
		{IGDBID: 12, Name: "Hidden", Year: 2012, CoverID: "c12"},
		{IGDBID: 4, Name: "Wished", Year: 2022, CoverID: "c4"},
		{IGDBID: 13, Name: "No image", Year: 2013},
	}
	f.similar[2] = []meta.CollectionGame{{IGDBID: 10, Name: "Both", Year: 2010, CoverID: "c10"}}
	f.similar[3] = []meta.CollectionGame{{IGDBID: 99, Name: "Never asked", CoverID: "c99"}}
	f.similar[4] = f.similar[3]
	opt, fetched := opts(t)
	if err := HideSimilar(d, 12); err != ErrUnknownSimilar {
		t.Errorf("hiding what was never suggested: %v", err)
	}
	rep, err := RefreshSimilar(context.Background(), d, f, opt)
	if err != nil || rep.Stopped != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if len(f.asked) != 1 || len(f.asked[0]) != 2 {
		t.Fatalf("asked %v: want one batch with the finished and the favourite game only", f.asked)
	}
	if err := HideSimilar(d, 12); err != nil {
		t.Fatal(err)
	}
	list, err := LoadSimilar(d)
	if err != nil {
		t.Fatal(err)
	}
	got := byID(list)
	if _, ok := got[8]; ok {
		t.Error("the original of an owned remaster is suggested")
	}
	if _, ok := got[99]; ok {
		t.Error("a game that was not played suggested something")
	}
	if g := got[10]; len(g.Sources) != 2 || g.Sources[0] != 1 || g.Sources[1] != 2 || g.Hidden {
		t.Errorf("game 10: %+v", g)
	}
	if !got[12].Hidden {
		t.Error("game 12 is not flagged hidden")
	}
	if _, ok := got[4]; !ok || len(list.WishlistRoots) != 1 || list.WishlistRoots[0] != 4 {
		t.Errorf("wishlist: suggestion %v roots %v", ok, list.WishlistRoots)
	}
	if len(list.OwnedRoots) == 0 || list.FetchedAt == "" {
		t.Errorf("roots %v fetched %q", list.OwnedRoots, list.FetchedAt)
	}
	// posters: the wishlist game is not owned (4) and 10 are downloaded; 12 was hidden by then or not (order), 13 has no image
	for _, id := range *fetched {
		if id == 8 || id == 13 || id == 99 {
			t.Errorf("fetched the poster of %d", id)
		}
	}
	UnhideSimilar(d, 12)
	list, _ = LoadSimilar(d)
	if byID(list)[12].Hidden {
		t.Error("unhide did not work")
	}
}

// A remaster of an owned game is dropped, a remake of it is kept (the version-chain root decides).
func TestSimilarRemasterDroppedRemakeKept(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Owned", "", 880)
	play(t, d, a, "finished", false)
	f := newSim()
	f.similar[880] = []meta.CollectionGame{
		{IGDBID: 396728, Name: "Remaster", Year: 2020, GameType: 9, ParentID: 880},
		{IGDBID: 19686, Name: "Remake", Year: 2019, GameType: 8},
	}
	opt, _ := opts(t)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	list, _ := LoadSimilar(d)
	got := byID(list)
	if _, ok := got[396728]; ok {
		t.Error("the remaster of an owned game is suggested")
	}
	if _, ok := got[19686]; !ok {
		t.Error("the remake is not suggested")
	}
}

// A suggestion's version chain is followed through IGDB's links (the expanded entry in the middle is in no list).
func TestSimilarFollowsVersionChains(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Phoenix Wright", "", 221280)
	b := addGame(t, d, "Other", "", 5)
	play(t, d, a, "backlog", false)
	play(t, d, b, "finished", false)
	f := newSim()
	f.links[1426] = meta.GameLinks{ParentID: 221280}
	f.similar[5] = []meta.CollectionGame{{IGDBID: 221285, Name: "Phoenix Wright HD", GameType: 9, ParentID: 1426, CoverID: "x"}}
	opt, _ := opts(t)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	var parent int64
	d.QueryRow(`SELECT parent_igdb_id FROM similar_game WHERE igdb_id = 221285`).Scan(&parent)
	if parent != 221280 {
		t.Errorf("stored root %d", parent)
	}
	list, _ := LoadSimilar(d)
	if len(list.Suggestions) != 0 {
		t.Errorf("the remaster of an owned game, through its expanded entry, is suggested: %+v", list.Suggestions)
	}
}

// The two caches share the i<id>.jpg posters: the prunes keep the other cache's files, drop the files nothing names, and
// forget the suggestions of games that no longer suggest anything (the hide list stays).
func TestSimilarPruneSharesPostersWithSeries(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Finished", "", 1)
	play(t, d, a, "finished", false)
	f := newSim()
	f.similar[1] = []meta.CollectionGame{{IGDBID: 10, Name: "Ten", Year: 2010, CoverID: "c10"}, {IGDBID: 11, Name: "Eleven", Year: 2011, CoverID: "c11"}}
	opt, _ := opts(t)
	d.Exec(`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (7, 'S', '2026-01-01T00:00:00Z')`)
	d.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type, cover_image, cover_file) VALUES (7, 500, 'Series game', 0, 'cs', 'i500.jpg')`)
	series := filepath.Join(opt.CoverDir, "i500.jpg")
	stale := filepath.Join(opt.CoverDir, "i600.jpg")
	fresh := filepath.Join(opt.CoverDir, "i601.jpg")
	for _, p := range []string{series, stale, fresh} {
		os.WriteFile(p, []byte("x"), 0o644)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(series, old, old)
	os.Chtimes(stale, old, old)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	if !exists(series) || exists(stale) || !exists(fresh) {
		t.Errorf("series poster %v (want kept), stale %v (want swept), fresh %v (want kept)", exists(series), exists(stale), exists(fresh))
	}

	// the series cache also lists game 10, then drops it: the file stays for the similar cache
	d.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type, cover_image, cover_file) VALUES (7, 10, 'Ten', 0, 'c10', 'i10.jpg')`)
	if err := store(d, opt.CoverDir, 7, "S", nil); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(opt.CoverDir, "i10.jpg")) {
		t.Error("the series cache deleted a poster the similar cache names")
	}
	// and a series prune sweep leaves the similar posters alone
	os.Chtimes(filepath.Join(opt.CoverDir, "i11.jpg"), old, old)
	if err := prune(d, opt.CoverDir, map[int64]bool{7: true}, family{roots: map[int64]bool{}}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(opt.CoverDir, "i11.jpg")) {
		t.Error("the series prune swept a similar poster")
	}

	// the finished game goes back to the backlog: its suggestions go, the hide stays, the posters follow
	HideSimilar(d, 10)
	play(t, d, a, "backlog", false)
	os.Chtimes(filepath.Join(opt.CoverDir, "i10.jpg"), old, old)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	var games, links, fetches, hidden int
	d.QueryRow(`SELECT (SELECT count(*) FROM similar_game), (SELECT count(*) FROM similar_link), (SELECT count(*) FROM similar_fetch), (SELECT count(*) FROM similar_hidden)`).Scan(&games, &links, &fetches, &hidden)
	if games != 0 || links != 0 || fetches != 0 || hidden != 1 {
		t.Errorf("games %d links %d fetches %d hidden %d", games, links, fetches, hidden)
	}
	if exists(filepath.Join(opt.CoverDir, "i10.jpg")) || exists(filepath.Join(opt.CoverDir, "i11.jpg")) {
		t.Error("posters nothing names any more were kept")
	}
}

// A suggestion that becomes owned loses its poster file at the next refresh.
func TestSimilarOwnedSuggestionLosesItsPoster(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Finished", "", 1)
	play(t, d, a, "finished", false)
	f := newSim()
	f.similar[1] = []meta.CollectionGame{{IGDBID: 10, Name: "Ten", Year: 2010, CoverID: "c10"}}
	opt, _ := opts(t)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(opt.CoverDir, "i10.jpg")) {
		t.Fatal("no poster")
	}
	addGame(t, d, "Ten", "", 10)
	if _, err := RefreshSimilar(context.Background(), d, f, opt); err != nil {
		t.Fatal(err)
	}
	var file sql.NullString
	d.QueryRow(`SELECT cover_file FROM similar_game WHERE igdb_id = 10`).Scan(&file)
	if file.Valid || exists(filepath.Join(opt.CoverDir, "i10.jpg")) {
		t.Errorf("the owned suggestion keeps its poster (%v)", file)
	}
}

// A second refresh keeps the poster of a suggestion whose image did not change, and starts the links over.
func TestSimilarRefreshKeepsPostersAndReplacesLinks(t *testing.T) {
	d := open(t)
	a := addGame(t, d, "Finished", "", 1)
	play(t, d, a, "finished", false)
	f := newSim()
	f.similar[1] = []meta.CollectionGame{{IGDBID: 10, Name: "Ten", Year: 2010, CoverID: "c10"}, {IGDBID: 11, Name: "Eleven", CoverID: "c11"}}
	opt, fetched := opts(t)
	RefreshSimilar(context.Background(), d, f, opt)
	f.similar[1] = f.similar[1][:1] // IGDB dropped 11
	RefreshSimilar(context.Background(), d, f, opt)
	if len(*fetched) != 2 {
		t.Errorf("fetched %v: the poster of 10 was downloaded again", *fetched)
	}
	list, _ := LoadSimilar(d)
	if len(list.Suggestions) != 1 || list.Suggestions[0].IGDBID != 10 || list.Suggestions[0].Cover == nil {
		t.Errorf("%+v", list.Suggestions)
	}
}

// limitAfter answers 429 once ok calls were served.
type limitAfter struct {
	*simFake
	ok, calls int
}

func (l *limitAfter) SimilarGames(ctx context.Context, ids []int64) (map[int64][]meta.CollectionGame, error) {
	l.calls++
	if l.calls > l.ok {
		return nil, &meta.HTTPError{Status: 429, Text: "429"}
	}
	return l.simFake.SimilarGames(ctx, ids)
}

// When IGDB keeps answering 429 the run stops; the batch that was stored stays.
func TestSimilarRateLimitStopsAndKeeps(t *testing.T) {
	d := open(t)
	for i := int64(1); i <= 45; i++ { // two batches of 40
		id := addGame(t, d, "G", "", i)
		play(t, d, id, "finished", false)
	}
	f := newSim()
	f.similar[1] = []meta.CollectionGame{{IGDBID: 1000, Name: "Kept", CoverID: "k"}}
	opt, _ := opts(t)
	var sleeps int
	opt.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	rep, err := RefreshSimilar(context.Background(), d, &limitAfter{simFake: f, ok: 1}, opt)
	if err != nil || rep.Stopped == "" || sleeps != 3 {
		t.Fatalf("%+v %v sleeps %d", rep, err, sleeps)
	}
	list, _ := LoadSimilar(d)
	if len(list.Suggestions) != 1 {
		t.Errorf("the first batch was not kept: %+v", list.Suggestions)
	}
}
