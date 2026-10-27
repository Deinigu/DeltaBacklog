package collections

import (
	"database/sql"
	"sort"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// StoreCollections replaces the IGDB collections of a game. The game's root (igdb_parent_id) is left as it is: it depends
// only on the game's IGDB id (changing the id clears it), the enrichment does not follow version links, and a game never
// read stays NULL until the next collections refresh.
func StoreCollections(tx *sql.Tx, gameID int64, cols []meta.CollectionRef) error {
	return storeCollections(tx, gameID, cols)
}

func storeCollections(tx *sql.Tx, gameID int64, cols []meta.CollectionRef) error {
	if _, err := tx.Exec("DELETE FROM game_collection WHERE game_id = ?", gameID); err != nil {
		return err
	}
	for _, c := range cols {
		if c.ID <= 0 {
			continue
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO game_collection (game_id, collection_id, name) VALUES (?, ?, ?)", gameID, c.ID, c.Name); err != nil {
			return err
		}
	}
	return nil
}

// StoreGameLinks replaces the IGDB collections of a game and records the root of its version chain (l.ParentID: 0 = the
// game is its own root, which also tells a refresh that the game was read).
func StoreGameLinks(tx *sql.Tx, gameID int64, l meta.GameLinks) error {
	if err := storeCollections(tx, gameID, l.Collections); err != nil {
		return err
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
// of the series' games share wins; when several tie, the ones named like the series win, else the ones named like a
// game's igdb_series (IGDB's first collection of that game, which is arbitrary, so it only decides when the owner's own
// name does not); when that does not decide either, all the tied ones are used (the union). A series without a linked
// game has none. The result is keyed by Key(series name).
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
		own   string          // the folded series name
		igdb  map[string]bool // folded igdb_series of its games
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
			a = &acc{games: map[int64]map[int64]bool{}, names: map[int64]string{}, own: meta.FoldKey(sname), igdb: map[string]bool{}}
			by[k] = a
		}
		if a.games[cid] == nil {
			a.games[cid] = map[int64]bool{}
		}
		a.games[cid][gid] = true
		a.names[cid] = cname
		if igdbSeries != "" {
			a.igdb[meta.FoldKey(igdbSeries)] = true
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
			for _, like := range []func(string) bool{func(n string) bool { return namedLike(n, a.own) }, func(n string) bool { return a.igdb[n] }} {
				var named []int64
				for _, cid := range tied {
					if like(meta.FoldKey(a.names[cid])) {
						named = append(named, cid)
					}
				}
				if len(named) > 0 {
					tied = named
					break
				}
			}
		}
		sort.Slice(tied, func(i, j int) bool { return tied[i] < tied[j] })
		for _, cid := range tied {
			out[k] = append(out[k], Picked{ID: cid, Name: a.names[cid]})
		}
	}
	return out, nil
}

// namedLike reports whether a folded collection name is the folded series name, or one contains the other ("zelda" is the
// series of "the legend of zelda"; names under 4 letters must be equal, so "ico" does not match every "...ico...").
func namedLike(collection, series string) bool {
	if collection == series {
		return true
	}
	return len(series) >= 4 && len(collection) >= 4 && (strings.Contains(collection, series) || strings.Contains(series, collection))
}

// family is the set of IGDB games the library owns, as roots. IGDB splits one game into several entries (the original,
// an expanded edition, a remaster, a port) joined by parent links; a root is the entry a chain of such links ends in
// (see root resolution in refresh). A game counts as owned when its root is the root of an owned entry. Links that do
// not mean "another version of the same game" (a standalone expansion's parent, a remake's) are never stored, so those
// games stay their own roots.
type family struct{ roots map[int64]bool }

// rootOf is the root of a game whose stored root is root (0 = the game is its own root).
func rootOf(igdbID, root int64) int64 {
	if root > 0 {
		return root
	}
	return igdbID
}

func loadFamily(d *sql.DB) (family, error) {
	f := family{roots: map[int64]bool{}}
	rows, err := d.Query(`SELECT igdb_id, coalesce(igdb_parent_id, 0) FROM game WHERE igdb_id IS NOT NULL`)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, root int64
		if err := rows.Scan(&id, &root); err != nil {
			return f, err
		}
		f.roots[rootOf(id, root)] = true
	}
	return f, rows.Err()
}

// has reports whether the library owns the IGDB game igdbID (whose stored root is root, 0 = itself) or another version of it.
func (f family) has(igdbID, root int64) bool { return f.roots[rootOf(igdbID, root)] }

// OwnedRoots lists the roots of the library's linked games, so the browser can apply the same rule to games added since
// the list was read.
func OwnedRoots(d *sql.DB) ([]int64, error) {
	f, err := loadFamily(d)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(f.roots))
	for r := range f.roots {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
