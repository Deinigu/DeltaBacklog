package deals

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

func TestScopeOwnership(t *testing.T) {
	d := newDB(t)
	for _, o := range []string{"Owned", "Pirated", "Borrowed", "Free to play", "Family Share", "Nope"} {
		d.Exec(`INSERT OR IGNORE INTO ownership(name) VALUES (?)`, o)
	}
	want := map[string]bool{"Owned": false, "Pirated": true, "Borrowed": true, "Free to play": false, "Family Share": true, "Nope": true, "": true}
	ids := map[string]int64{}
	var n int64
	for o := range want {
		n++
		ids[o] = n
		addGame(t, d, n, "G"+fmt.Sprint(n), "backlog", "")
		if o != "" {
			d.Exec(`UPDATE game SET ownership_id = (SELECT id FROM ownership WHERE name = ?) WHERE id = ?`, o, n)
		}
	}
	// The name is matched without regard to case.
	d.Exec(`UPDATE ownership SET name = 'PIRATED' WHERE name = 'Pirated'`)
	l, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	for o, shown := range want {
		if got := slices.Contains(l.Watched, ids[o]); got != shown {
			t.Errorf("ownership %q: shown %v, want %v", o, got, shown)
		}
	}
}

func TestScopePlayedAndStatus(t *testing.T) {
	d := newDB(t)
	var codes []string
	rows, _ := d.Query(`SELECT code FROM status`)
	for rows.Next() {
		var c string
		rows.Scan(&c)
		codes = append(codes, c)
	}
	rows.Close()
	if len(codes) < 9 {
		t.Fatalf("statuses: %v", codes)
	}
	id := int64(0)
	byStatus := map[string]int64{}
	for _, c := range codes {
		id++
		byStatus[c] = id
		addGame(t, d, id, "S "+c, c, "")
	}
	// A backlog game with a year, one with a period, and one with neither.
	addGame(t, d, 100, "Year", "backlog", "")
	addGame(t, d, 101, "Period", "backlog", "")
	addGame(t, d, 102, "Fresh", "up_next", "")
	d.Exec(`INSERT INTO play(game_id, year) VALUES (100, 2020)`)
	d.Exec(`INSERT INTO play(game_id, period) VALUES (101, 'childhood')`)
	l, _ := Load(d)
	got := map[int64]bool{}
	for _, g := range l.Watched {
		got[g] = true
	}
	for c, gid := range byStatus {
		if want := c == "backlog" || c == "up_next"; got[gid] != want {
			t.Errorf("status %s: shown %v, want %v", c, got[gid], want)
		}
	}
	if got[100] || got[101] || !got[102] {
		t.Errorf("played: year %v period %v fresh %v", got[100], got[101], got[102])
	}
}

func TestHideAndRefresh(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "Shown", "backlog", "")
	addGame(t, d, 2, "Hidden", "backlog", "")
	addGame(t, d, 3, "Finished", "finished", "")
	src := &fakeSource{byTitle: map[string]Game{"Shown": {ID: "a", Title: "Shown"}, "Hidden": {ID: "b", Title: "Hidden"}},
		prices: map[string]Overview{"a": {}, "b": {}}}
	if err := SetHidden(d, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := SetHidden(d, 2, true); err != nil {
		t.Fatalf("hiding twice must be fine: %v", err)
	}
	if err := SetHidden(d, 99, true); err != ErrUnknownGame {
		t.Errorf("unknown game: %v", err)
	}
	rep, err := Refresh(context.Background(), d, src, Options{})
	if err != nil || rep.Games != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	for _, ids := range src.overview {
		if slices.Contains(ids, "b") {
			t.Error("a hidden game must not be priced")
		}
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM deal_match WHERE game_id = 2`).Scan(&n)
	if n != 0 {
		t.Error("a hidden game must not be matched")
	}
	l, _ := Load(d)
	if !slices.Equal(l.Watched, []int64{1}) || !slices.Equal(l.Hidden, []int64{2}) {
		t.Errorf("watched %v hidden %v", l.Watched, l.Hidden)
	}
	// Unhide: back. A hidden game that is not eligible is not listed as hidden.
	SetHidden(d, 2, false)
	SetHidden(d, 3, true)
	l, _ = Load(d)
	if !slices.Equal(l.Watched, []int64{1, 2}) || len(l.Hidden) != 0 {
		t.Errorf("after unhide: watched %v hidden %v", l.Watched, l.Hidden)
	}
	// The stored price of a game that gets hidden is kept but not listed.
	rep, _ = Refresh(context.Background(), d, src, Options{})
	if rep.Games != 2 {
		t.Errorf("games %d", rep.Games)
	}
	SetHidden(d, 1, true)
	l, _ = Load(d)
	for _, p := range l.Prices {
		if p.GameID == 1 {
			t.Error("a hidden game's price must not be listed")
		}
	}
	d.QueryRow(`SELECT count(*) FROM deal_price WHERE game_id = 1`).Scan(&n)
	if n != 1 {
		t.Errorf("the stored price must be kept, got %d rows", n)
	}
}

// A refresh takes minutes: a price read for a game that was hidden (or left the scope) meanwhile is not stored.
func TestSavePriceSkipsGamesOutOfScope(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "A", "backlog", "")
	if err := SetMatch(d, 1, Game{ID: "a", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	o := Overview{Current: &Current{Shop: "Steam", Price: eur(500), Regular: eur(1000), Cut: 50}}
	count := func() (n int) {
		d.QueryRow(`SELECT count(*) FROM deal_price WHERE game_id = 1`).Scan(&n)
		return
	}
	SetHidden(d, 1, true)
	if ok, err := savePrice(d, 1, "a", "ES", o); err != nil || ok || count() != 0 {
		t.Fatalf("hidden: ok %v err %v rows %d", ok, err, count())
	}
	SetHidden(d, 1, false)
	d.Exec(`INSERT INTO play(game_id, year) VALUES (1, 2024)`)
	if ok, _ := savePrice(d, 1, "a", "ES", o); ok || count() != 0 {
		t.Fatalf("played: ok %v rows %d", ok, count())
	}
	d.Exec(`DELETE FROM play`)
	if ok, err := savePrice(d, 1, "a", "ES", o); err != nil || !ok || count() != 1 {
		t.Fatalf("in scope: ok %v err %v rows %d", ok, err, count())
	}
}
