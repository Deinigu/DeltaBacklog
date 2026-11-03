package collections

import (
	"database/sql"
	"sort"
)

// HiddenSimilar is a hidden Similar games suggestion with what the Hidden page shows about it.
type HiddenSimilar struct {
	IGDBID   int64    `json:"igdb_id"`
	Title    string   `json:"title"`
	Year     *int     `json:"year"`
	Cover    *string  `json:"cover"`
	V        string   `json:"v"`
	HiddenAt string   `json:"hidden_at"`
	Because  []string `json:"because"` // titles of the library games that suggested it (finished or favourite, linked to IGDB)
}

// HiddenSeries is a game hidden on the Series page, with the name of the IGDB collection it was hidden from.
type HiddenSeries struct {
	CollectionID int64   `json:"collection_id"`
	IGDBID       int64   `json:"igdb_id"`
	Title        string  `json:"title"`
	Year         *int    `json:"year"`
	Cover        *string `json:"cover"`
	V            string  `json:"v"`
	Series       string  `json:"series"`
	HiddenAt     string  `json:"hidden_at"`
}

// LoadHiddenSimilar lists the hidden suggestions that are still suggestions (not owned, at least one source), from the
// cache only, the newest hide first.
func LoadHiddenSimilar(d *sql.DB) ([]HiddenSimilar, error) {
	list, err := LoadSimilar(d)
	if err != nil {
		return nil, err
	}
	at, err := hiddenAt(d, `SELECT igdb_id, 0, hidden_at FROM similar_hidden`)
	if err != nil {
		return nil, err
	}
	titles := map[int64]string{} // IGDB id -> title of the library's source games
	rows, err := d.Query(`SELECT g.igdb_id, g.title FROM (` + sourceSQL + `) x JOIN game g ON g.id = x.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		titles[id] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []HiddenSimilar{}
	for _, s := range list.Suggestions {
		if !s.Hidden {
			continue
		}
		h := HiddenSimilar{IGDBID: s.IGDBID, Title: s.Title, Year: s.Year, Cover: s.Cover, V: s.V, HiddenAt: at[[2]int64{s.IGDBID, 0}], Because: []string{}}
		for _, src := range s.Sources {
			if t, ok := titles[src]; ok {
				h.Because = append(h.Because, t)
			}
		}
		if len(h.Because) == 0 {
			continue // the Similar page does not list it either
		}
		sort.Strings(h.Because)
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HiddenAt != out[j].HiddenAt {
			return out[i].HiddenAt > out[j].HiddenAt
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// LoadHiddenSeries lists the games hidden on the Series page that are still not owned, from the cache only.
func LoadHiddenSeries(d *sql.DB) ([]HiddenSeries, error) {
	all, err := Load(d)
	if err != nil {
		return nil, err
	}
	at, err := hiddenAt(d, `SELECT collection_id, igdb_id, hidden_at FROM series_hidden`)
	if err != nil {
		return nil, err
	}
	out := []HiddenSeries{}
	for _, s := range all {
		names := map[int64]string{}
		for _, c := range s.Collections {
			names[c.ID] = c.Name
		}
		for _, g := range s.Hidden {
			out = append(out, HiddenSeries{CollectionID: g.CollectionID, IGDBID: g.IGDBID, Title: g.Title, Year: g.Year, Cover: g.Cover, V: g.V,
				Series: names[g.CollectionID], HiddenAt: at[[2]int64{g.CollectionID, g.IGDBID}]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HiddenAt != out[j].HiddenAt {
			return out[i].HiddenAt > out[j].HiddenAt
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// hiddenAt maps (a, b) to hidden_at for a query selecting a, b, hidden_at.
func hiddenAt(d *sql.DB, q string) (map[[2]int64]string, error) {
	rows, err := d.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[[2]int64]string{}
	for rows.Next() {
		var a, b int64
		var t string
		if err := rows.Scan(&a, &b, &t); err != nil {
			return nil, err
		}
		m[[2]int64{a, b}] = t
	}
	return m, rows.Err()
}
