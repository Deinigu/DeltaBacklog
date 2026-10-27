package collections

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// Game is a game of a series' IGDB collection that the library does not have.
type Game struct {
	IGDBID       int64   `json:"igdb_id"`
	Title        string  `json:"title"`
	Year         *int    `json:"year"`
	Cover        *string `json:"cover"` // poster file name under /covers/, nil until downloaded
	CollectionID int64   `json:"collection_id"`
	// IGDB's first release date (unix seconds, UTC) and how exact it is (date_format: 0 day, 1 month, 2 year, 3-6
	// quarter); both null when IGDB has no date (to be announced).
	Date      *int64  `json:"date"`
	Format    *int    `json:"format"`
	Platforms *string `json:"platforms"` // "PC, PS5", null when IGDB lists none
}

// Collection is a cached IGDB collection of a series.
type Collection struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	FetchedAt string `json:"fetched_at"`
}

// Series is what the cache holds for one of the library's series (Key is the lower-cased, trimmed series name, the
// key the Series page groups by).
type Series struct {
	Key         string       `json:"key"`
	Collections []Collection `json:"collections"`
	Missing     []Game       `json:"missing"`
	Hidden      []Game       `json:"hidden"`
}

// Key is how the Series page identifies a series: its name, trimmed and lower-cased.
func Key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Load returns, for every series with a cached collection, the games of the collection the library lacks (matched on
// igdb_id) and the ones the owner hid. Series without a cached collection are absent.
func Load(d *sql.DB) ([]Series, error) {
	// series -> collections, from the linked games
	rows, err := d.Query(`SELECT DISTINCT s.name, c.id, c.name, c.fetched_at
		FROM game g JOIN series s ON s.id = g.series_id JOIN igdb_collection c ON c.id = g.igdb_collection_id`)
	if err != nil {
		return nil, err
	}
	bySeries := map[string]*Series{}
	seen := map[string]map[int64]bool{}
	for rows.Next() {
		var sname string
		var c Collection
		if err := rows.Scan(&sname, &c.ID, &c.Name, &c.FetchedAt); err != nil {
			rows.Close()
			return nil, err
		}
		k := Key(sname)
		s := bySeries[k]
		if s == nil {
			s = &Series{Key: k, Collections: []Collection{}, Missing: []Game{}, Hidden: []Game{}}
			bySeries[k] = s
			seen[k] = map[int64]bool{}
		}
		if !seen[k][c.ID] {
			seen[k][c.ID] = true
			s.Collections = append(s.Collections, c)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, s := range bySeries {
		sort.Slice(s.Collections, func(i, j int) bool { return s.Collections[i].ID < s.Collections[j].ID })
		have := map[int64]bool{}
		for _, c := range s.Collections {
			grows, err := d.Query(`SELECT g.igdb_id, g.title, g.release_year, g.cover_file, g.first_release_date, g.release_date_format, g.platforms,
					EXISTS (SELECT 1 FROM series_hidden h WHERE h.collection_id = g.collection_id AND h.igdb_id = g.igdb_id)
				FROM igdb_collection_game g
				WHERE g.collection_id = ? AND g.igdb_id NOT IN (SELECT igdb_id FROM game WHERE igdb_id IS NOT NULL)
				ORDER BY coalesce(g.release_year, 9999), g.title, g.igdb_id`, c.ID)
			if err != nil {
				return nil, err
			}
			for grows.Next() {
				g := Game{CollectionID: c.ID}
				var year sql.NullInt64
				var cover sql.NullString
				var hidden bool
				if err := grows.Scan(&g.IGDBID, &g.Title, &year, &cover, &g.Date, &g.Format, &g.Platforms, &hidden); err != nil {
					grows.Close()
					return nil, err
				}
				if have[g.IGDBID] { // in two collections of the same series
					continue
				}
				have[g.IGDBID] = true
				if year.Valid {
					y := int(year.Int64)
					g.Year = &y
				}
				if cover.Valid && cover.String != "" {
					g.Cover = &cover.String
				}
				if hidden {
					s.Hidden = append(s.Hidden, g)
				} else {
					s.Missing = append(s.Missing, g)
				}
			}
			if err := grows.Close(); err != nil {
				return nil, err
			}
		}
		sortGames(s.Missing)
		sortGames(s.Hidden)
	}
	out := make([]Series, 0, len(bySeries))
	for _, s := range bySeries {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func sortGames(gs []Game) {
	sort.SliceStable(gs, func(i, j int) bool {
		yi, yj := 9999, 9999
		if gs[i].Year != nil {
			yi = *gs[i].Year
		}
		if gs[j].Year != nil {
			yj = *gs[j].Year
		}
		if yi != yj {
			return yi < yj
		}
		return gs[i].Title < gs[j].Title
	})
}

// ErrUnknown means the game is not in the cached collection.
var ErrUnknown = errors.New("that game is not in the cached collection")

// Hide stops a game of a collection from showing as "not registered"; Unhide brings it back.
func Hide(d *sql.DB, collectionID, igdbID int64) error {
	return setHidden(d, collectionID, igdbID, true)
}

// Unhide is the reverse of Hide.
func Unhide(d *sql.DB, collectionID, igdbID int64) error {
	return setHidden(d, collectionID, igdbID, false)
}

func setHidden(d *sql.DB, collectionID, igdbID int64, hide bool) error {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM igdb_collection_game WHERE collection_id = ? AND igdb_id = ?`, collectionID, igdbID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknown
	}
	if hide {
		_, err := d.Exec(`INSERT OR IGNORE INTO series_hidden(collection_id, igdb_id) VALUES (?, ?)`, collectionID, igdbID)
		return err
	}
	_, err := d.Exec(`DELETE FROM series_hidden WHERE collection_id = ? AND igdb_id = ?`, collectionID, igdbID)
	return err
}
