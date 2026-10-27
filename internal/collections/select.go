package collections

import (
	"database/sql"
	"sort"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// StoreGameLinks replaces the IGDB collections of a game and records its parent game (0 = none, so a refresh knows the
// game was read). Shared by the enrichment and the refresh.
func StoreGameLinks(tx *sql.Tx, gameID int64, l meta.GameLinks) error {
	if _, err := tx.Exec("DELETE FROM game_collection WHERE game_id = ?", gameID); err != nil {
		return err
	}
	for _, c := range l.Collections {
		if c.ID <= 0 {
			continue
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO game_collection (game_id, collection_id, name) VALUES (?, ?, ?)", gameID, c.ID, c.Name); err != nil {
			return err
		}
	}
	_, err := tx.Exec("UPDATE game SET igdb_parent_id = ? WHERE id = ?", max(l.ParentID, 0), gameID)
	return err
}

// Picked is a collection chosen for a series.
type Picked struct {
	ID   int64
	Name string
}

// pickCollections maps every series to its IGDB collection(s), through the collections of its linked games: the one most
// of the series' games share wins; when several tie, the ones named like the series (or like a game's igdb_series)
// win; when that does not decide either, all the tied ones are used. A series without a linked game has none.
// The result is keyed by Key(series name).
func pickCollections(d *sql.DB) (map[string][]Picked, error) {
	rows, err := d.Query(`SELECT s.name, g.id, coalesce(g.igdb_series, ''), gc.collection_id, gc.name
		FROM game g JOIN series s ON s.id = g.series_id JOIN game_collection gc ON gc.game_id = g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type acc struct {
		games map[int64]map[int64]bool // collection -> game ids
		names map[int64]string
		like  map[string]bool // folded names the series is known by
	}
	by := map[string]*acc{}
	for rows.Next() {
		var sname, igdbSeries, cname string
		var gid, cid int64
		if err := rows.Scan(&sname, &gid, &igdbSeries, &cid, &cname); err != nil {
			return nil, err
		}
		k := Key(sname)
		a := by[k]
		if a == nil {
			a = &acc{games: map[int64]map[int64]bool{}, names: map[int64]string{}, like: map[string]bool{meta.FoldKey(sname): true}}
			by[k] = a
		}
		if a.games[cid] == nil {
			a.games[cid] = map[int64]bool{}
		}
		a.games[cid][gid] = true
		a.names[cid] = cname
		if igdbSeries != "" {
			a.like[meta.FoldKey(igdbSeries)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string][]Picked{}
	for k, a := range by {
		best := 0
		for _, g := range a.games {
			best = max(best, len(g))
		}
		var tied []int64
		for cid, g := range a.games {
			if len(g) == best {
				tied = append(tied, cid)
			}
		}
		if len(tied) > 1 {
			var named []int64
			for _, cid := range tied {
				if a.like[meta.FoldKey(a.names[cid])] {
					named = append(named, cid)
				}
			}
			if len(named) > 0 {
				tied = named
			}
		}
		sort.Slice(tied, func(i, j int) bool { return tied[i] < tied[j] })
		for _, cid := range tied {
			out[k] = append(out[k], Picked{ID: cid, Name: a.names[cid]})
		}
	}
	return out, nil
}

// family is the set of IGDB entries the library owns. IGDB splits one game into several entries (the original, an
// expanded edition, a remaster), so a game counts as owned when it is, or is the parent of, or has the same parent as,
// an owned entry.
type family struct{ owned, parents map[int64]bool }

func loadFamily(d *sql.DB) (family, error) {
	f := family{owned: map[int64]bool{}, parents: map[int64]bool{}}
	rows, err := d.Query(`SELECT igdb_id, coalesce(igdb_parent_id, 0) FROM game WHERE igdb_id IS NOT NULL`)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent int64
		if err := rows.Scan(&id, &parent); err != nil {
			return f, err
		}
		f.owned[id] = true
		if parent > 0 {
			f.parents[parent] = true
		}
	}
	return f, rows.Err()
}

// has reports whether the library owns the IGDB game igdbID (whose parent is parentID, 0 = none) or its family.
func (f family) has(igdbID, parentID int64) bool {
	if f.owned[igdbID] || f.parents[igdbID] {
		return true
	}
	return parentID > 0 && (f.owned[parentID] || f.parents[parentID])
}

// OwnedParents lists the parent games of the library's linked games, so the browser can apply the same family rule to
// games added since the list was read.
func OwnedParents(d *sql.DB) ([]int64, error) {
	f, err := loadFamily(d)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(f.parents))
	for p := range f.parents {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
