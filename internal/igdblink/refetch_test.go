package igdblink

import (
	"context"
	"errors"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

func TestRefetchFillsOnlyEmptyOrReplaces(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	// the owner already has a genre, a year and a hand-typed time
	gid, _ := db.GetOrCreate(d, "genre", "Action")
	d.Exec("INSERT INTO game_genre(game_id, genre_id) VALUES (?, ?)", id, gid)
	d.Exec("UPDATE game SET release_year = 1999, time_to_beat_normally = 300, time_to_beat_src = 'manual', notes = 'mine' WHERE id = ?", id)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 72000}}}

	// the preview writes nothing
	res, err := Refetch(context.Background(), d, f, id, true, false)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]FieldChange{}
	for _, c := range res.Changes {
		fields[c.Field] = c
	}
	if c := fields["genres"]; c.Old != "Action" || c.New != "Indie, Roguelike" {
		t.Errorf("genres change = %+v", c)
	}
	if c := fields["release_year"]; c.Old != "1999" || c.New != "2020" {
		t.Errorf("year change = %+v", c)
	}
	if _, ok := fields["ttb_1"]; ok {
		t.Error("a time typed by hand must never change")
	}
	if g := names(t, d, "SELECT p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id WHERE x.game_id = ?", id); g != "Action" {
		t.Errorf("preview wrote genres: %s", g)
	}

	// fill empty: the genre and the year stay, the empty fields are filled
	res, err = Refetch(context.Background(), d, f, id, false, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Changes {
		if c.Field == "genres" || c.Field == "release_year" {
			t.Errorf("fill-empty changed %s", c.Field)
		}
	}
	var year, ttb int
	var dev, notes string
	d.QueryRow(`SELECT release_year, time_to_beat_normally, coalesce((SELECT name FROM company WHERE id = developer_id), ''), notes FROM game WHERE id = ?`, id).Scan(&year, &ttb, &dev, &notes)
	if year != 1999 || ttb != 300 || dev != "Supergiant Games" || notes != "mine" {
		t.Errorf("after fill: %d %d %q %q", year, ttb, dev, notes)
	}
	if _, ok := res.Undo["developer"]; !ok || res.Undo["developer"] != "" {
		t.Errorf("undo = %v", res.Undo)
	}

	// replace: IGDB's values win, the time and the notes still do not move
	res, err = Refetch(context.Background(), d, f, id, true, true)
	if err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT release_year, time_to_beat_normally, notes FROM game WHERE id = ?`, id).Scan(&year, &ttb, &notes)
	if year != 2020 || ttb != 300 || notes != "mine" {
		t.Errorf("after replace: %d %d %q", year, ttb, notes)
	}
	if g := names(t, d, "SELECT p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id WHERE x.game_id = ? ORDER BY 1", id); g != "Indie,Roguelike" {
		t.Errorf("genres after replace: %s", g)
	}
	if got, _ := res.Undo["release_year"].(int); got != 1999 {
		t.Errorf("undo year = %v", res.Undo["release_year"])
	}
}

func TestRefetchKeepsPlatformsAndFillsIgdbTimes(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	pid, _ := db.GetOrCreate(d, "platform", "Switch")
	db.GetOrCreate(d, "platform", "PC")
	d.Exec("INSERT INTO game_platform(game_id, platform_id) VALUES (?, ?)", id, pid)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 72000}}}
	if _, err := Refetch(context.Background(), d, f, id, true, true); err != nil {
		t.Fatal(err)
	}
	if g := names(t, d, "SELECT p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = ?", id); g != "Switch" {
		t.Errorf("platforms = %s", g)
	}
	var n int
	var src string
	d.QueryRow("SELECT time_to_beat_normally, time_to_beat_src FROM game WHERE id = ?", id).Scan(&n, &src)
	if n != 1200 || src != "igdb" {
		t.Errorf("igdb time = %d %q", n, src)
	}
}

func TestRefetchKeepsALibrarySeriesIGDBCannotRefill(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	sid, _ := db.GetOrCreate(d, "series", "My own series")
	d.Exec("UPDATE game SET series_id = ? WHERE id = ?", sid, id)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}}
	if _, err := Refetch(context.Background(), d, f, id, true, true); err != nil {
		t.Fatal(err)
	}
	var s string
	d.QueryRow("SELECT coalesce((SELECT name FROM series WHERE id = series_id), '') FROM game WHERE id = ?", id).Scan(&s)
	if s != "My own series" {
		t.Errorf("series = %q", s)
	}
}

func TestRefetchErrors(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	f := &fakeEnricher{details: map[int64]meta.Details{}}
	if _, err := Refetch(context.Background(), d, f, id, false, true); !errors.Is(err, ErrNotLinked) {
		t.Errorf("unlinked: %v", err)
	}
	if _, err := Refetch(context.Background(), d, f, 999, false, true); !errors.Is(err, ErrNoGame) {
		t.Errorf("missing game: %v", err)
	}
	link(t, d, id, 1)
	if _, err := Refetch(context.Background(), d, f, id, false, true); !errors.Is(err, ErrNotOnIGDB) {
		t.Errorf("unknown to IGDB: %v", err)
	}
	f.fail = true
	if _, err := Refetch(context.Background(), d, f, id, false, true); err == nil {
		t.Error("an IGDB failure must be an error")
	}
}

func TestRefetchKeepsHowLongToBeatTimes(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	d.Exec("UPDATE game SET time_to_beat_hastily = 90, time_to_beat_normally = 120, time_to_beat_src = 'hltb' WHERE id = ?", id)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 72000, Completely: 150000}}}
	for _, replace := range []bool{false, true} {
		if _, err := Refetch(context.Background(), d, f, id, replace, true); err != nil {
			t.Fatal(err)
		}
		var h, n, c *int
		var src string
		d.QueryRow("SELECT time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely, time_to_beat_src FROM game WHERE id = ?", id).Scan(&h, &n, &c, &src)
		if h == nil || *h != 90 || n == nil || *n != 120 || c != nil || src != "hltb" {
			t.Errorf("replace=%v: HowLongToBeat times changed: %v %v %v %q", replace, h, n, c, src)
		}
	}
}

// The times of the old IGDB game must not survive next to the new game's: IGDB's three times are written as a set.
func TestRefetchTimesComeFromOneGame(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	d.Exec("UPDATE game SET time_to_beat_hastily = 100, time_to_beat_normally = 200, time_to_beat_completely = 300, time_to_beat_src = 'igdb' WHERE id = ?", id)
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}, ttb: map[int64]meta.TimeToBeat{1: {Normally: 72000}}}
	if _, err := Refetch(context.Background(), d, f, id, false, true); err != nil {
		t.Fatal(err)
	}
	var h, n, c *int
	d.QueryRow("SELECT time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely FROM game WHERE id = ?", id).Scan(&h, &n, &c)
	if h != nil || n == nil || *n != 1200 || c != nil {
		t.Errorf("times = %v %v %v, want only the normal time of the new game", h, n, c)
	}
}

func TestRefetchPreviewShowsTheReleaseDate(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.Release = meta.Release{Date: 1589241600, Format: 0} // 2020-05-12
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	res, err := Refetch(context.Background(), d, f, id, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, c := range res.Changes {
		if c.Field == "release_date" {
			got = c.New
		}
	}
	if got != "2020-05-12" {
		t.Errorf("release date line = %q in %+v", got, res.Changes)
	}
}

// Replace never writes what is the owner's: status, ownership, favourite, notes, years played, cover and poster.
func TestRefetchLeavesTheOwnersFieldsAlone(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	oid, _ := db.GetOrCreate(d, "ownership", "Owned")
	d.Exec("UPDATE game SET ownership_id = ?, favorite = 1, vr = 1, completed = 1, notes = 'n', cover_file = 'c.jpg', poster_file = 'p.jpg', poster_user = 1 WHERE id = ?", oid, id)
	d.Exec("INSERT INTO play(game_id, year) VALUES (?, 2021)", id)
	var before [9]string
	read := func() (out [9]string) {
		d.QueryRow(`SELECT (SELECT code FROM status WHERE id = status_id), coalesce(ownership_id, ''), favorite, vr, completed, notes, coalesce(cover_file, ''),
			coalesce(poster_file, '') || poster_user, (SELECT group_concat(year) FROM play WHERE game_id = game.id) FROM game WHERE id = ?`, id).
			Scan(&out[0], &out[1], &out[2], &out[3], &out[4], &out[5], &out[6], &out[7], &out[8])
		return
	}
	before = read()
	f := &fakeEnricher{details: map[int64]meta.Details{1: hades()}}
	if _, err := Refetch(context.Background(), d, f, id, true, true); err != nil {
		t.Fatal(err)
	}
	if after := read(); after != before {
		t.Errorf("owner's fields changed: %v -> %v", before, after)
	}
}
