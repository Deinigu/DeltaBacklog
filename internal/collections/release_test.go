package collections

import (
	"context"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// dateFake adds ReleaseDates to the fake IGDB.
type dateFake struct {
	*fake
	dates map[int64]meta.ReleaseOf
	asked [][]int64
}

func (f *dateFake) ReleaseDates(_ context.Context, ids []int64) (map[int64]meta.ReleaseOf, error) {
	f.asked = append(f.asked, ids)
	out := map[int64]meta.ReleaseOf{}
	for _, id := range ids {
		if r, ok := f.dates[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}

func TestCollectionGamesStoreTheirDates(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	f := newFake()
	f.games[7][2].Release = meta.Release{Date: 1830211199, Format: meta.FormatYear} // Zed III: "2027"
	f.games[7][2].Platforms = []string{"PC", "PS5"}
	o, _ := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	list, err := Load(d)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	var z3, tba *Game
	for i := range list[0].Missing {
		switch list[0].Missing[i].IGDBID {
		case 102:
			z3 = &list[0].Missing[i]
		case 103:
			tba = &list[0].Missing[i]
		}
	}
	if z3 == nil || z3.Date == nil || *z3.Date != 1830211199 || *z3.Format != meta.FormatYear || z3.Platforms == nil || *z3.Platforms != "PC, PS5" {
		t.Errorf("zed III: %+v", z3)
	}
	if tba == nil || tba.Date != nil || tba.Format != nil || tba.Platforms != nil { // unknown stays NULL
		t.Errorf("zed origins: %+v", tba)
	}
}

func TestRefreshReadsDatesOfUndatedAndUnreleasedGames(t *testing.T) {
	d := open(t)
	now := time.Now()
	old := now.AddDate(-3, 0, 0).Unix()
	future := now.AddDate(0, 6, 0).Unix()
	undated := addGame(t, d, "Undated", "", 501)
	released := addGame(t, d, "Released long ago", "", 502)
	soon := addGame(t, d, "Not out yet", "", 503)
	unlinked := addGame(t, d, "Unlinked", "", 0)
	d.Exec(`UPDATE game SET first_release_date = ?, release_date_format = 0 WHERE id = ?`, old, released)
	d.Exec(`UPDATE game SET first_release_date = ?, release_date_format = 2 WHERE id = ?`, future, soon)

	f := &dateFake{fake: newFake(), dates: map[int64]meta.ReleaseOf{
		501: {Release: meta.Release{Date: old, Format: meta.FormatDay}},
		503: {}, // IGDB now says TBD: the date is cleared
		502: {Release: meta.Release{Date: now.Unix(), Format: 0}},
	}}
	o, _ := opts(t)
	rep, err := Refresh(context.Background(), d, f, o)
	if err != nil || rep.Stopped != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if len(f.asked) != 1 || len(f.asked[0]) != 2 { // undated, soon; not the long-released one
		t.Errorf("asked %v", f.asked)
	}
	get := func(id int64) (date, format *int64) {
		var dt, fm *int64
		d.QueryRow(`SELECT first_release_date, release_date_format FROM game WHERE id = ?`, id).Scan(&dt, &fm)
		return dt, fm
	}
	if dt, _ := get(undated); dt == nil || *dt != old {
		t.Errorf("undated: %v", dt)
	}
	if dt, fm := get(soon); dt != nil || fm != nil {
		t.Errorf("a date IGDB took back must clear: %v %v", dt, fm)
	}
	if dt, _ := get(released); dt == nil || *dt != old {
		t.Errorf("released long ago was touched: %v", dt)
	}
	if dt, _ := get(unlinked); dt != nil {
		t.Errorf("unlinked: %v", dt)
	}
	if rep.Dates != 1 {
		t.Errorf("report %+v", rep)
	}
}

// A library game owns its whole IGDB version family, so the original and the remaster of a game are not offered a
// second time as "not in your library".
func TestLibraryGameOwnsItsFamily(t *testing.T) {
	d := open(t)
	addGame(t, d, "Zed", "Zeds", 100)
	addGame(t, d, "Zed II", "Zeds", 101)
	f := newFake()
	f.games[7] = []meta.CollectionGame{
		{IGDBID: 100, Name: "Zed", Year: 1997},
		{IGDBID: 101, Name: "Zed II", Year: 2005, GameType: 9, ParentID: 100},
		{IGDBID: 102, Name: "Zed III", Release: meta.Release{Date: 1830211199, Format: meta.FormatYear}},
	}
	f.links[101] = meta.GameLinks{Collections: zed().Collections, ParentID: 100}
	o, _ := opts(t)
	if _, err := Refresh(context.Background(), d, f, o); err != nil {
		t.Fatal(err)
	}
	list, err := Load(d)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if len(list[0].Missing) != 1 || list[0].Missing[0].IGDBID != 102 || list[0].Missing[0].Date == nil {
		t.Errorf("missing: %+v", list[0].Missing)
	}
}
