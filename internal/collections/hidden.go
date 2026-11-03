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

// HiddenSeries is a game hidden on the Series page, with the owner's series name (the key the Series page groups by).
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
	type src struct {
		title string
		year  int64 // latest year played, 0 = none
		id    int64
	}
	srcs := map[int64]src{} // IGDB id -> the library's source game
	rows, err := d.Query(`SELECT g.igdb_id, g.title, coalesce((SELECT max(p.year) FROM play p WHERE p.game_id = g.id), 0), g.id FROM (` + sourceSQL + `) x JOIN game g ON g.id = x.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s src
		if err := rows.Scan(&id, &s.title, &s.year, &s.id); err != nil {
			return nil, err
		}
		srcs[id] = s
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
		var from []src
		for _, id := range s.Sources {
			if x, ok := srcs[id]; ok {
				from = append(from, x)
			}
		}
		if len(from) == 0 {
			continue // the Similar page does not list it either
		}
		// newest first, like the Similar page: the latest year played, then the game added last
		sort.Slice(from, func(i, j int) bool {
			if from[i].year != from[j].year {
				return from[i].year > from[j].year
			}
			return from[i].id > from[j].id
		})
		for _, x := range from {
			h.Because = append(h.Because, x.title)
		}
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
		for _, g := range s.Hidden {
			out = append(out, HiddenSeries{CollectionID: g.CollectionID, IGDBID: g.IGDBID, Title: g.Title, Year: g.Year, Cover: g.Cover, V: g.V,
				Series: s.Key, HiddenAt: at[[2]int64{g.CollectionID, g.IGDBID}]})
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
