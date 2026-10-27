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
	Cover        *string `json:"cover"`  // poster file name under /covers/, nil until downloaded
	V            string  `json:"v"`      // IGDB's image id of the cover: the cache key of the poster URL
	Parent       int64   `json:"parent"` // the IGDB game this one is a version of, 0 = none (the browser applies the family rule too)
	CollectionID int64   `json:"collection_id"`
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

// Load returns, for every series whose collection(s) are cached, the games of the collection the library lacks and the
// ones the owner hid. A game counts as owned with its whole family (see family). Series without a cached collection
// are absent.
func Load(d *sql.DB) ([]Series, error) {
	picked, err := pickCollections(d)
	if err != nil {
		return nil, err
	}
	fam, err := loadFamily(d)
	if err != nil {
		return nil, err
	}
	var out []Series
	for key, ps := range picked {
		s := Series{Key: key, Collections: []Collection{}, Missing: []Game{}, Hidden: []Game{}}
		have := map[int64]bool{}
		for _, p := range ps {
			c := Collection{ID: p.ID}
			err := d.QueryRow(`SELECT name, fetched_at FROM igdb_collection WHERE id = ?`, p.ID).Scan(&c.Name, &c.FetchedAt)
			if errors.Is(err, sql.ErrNoRows) {
				continue // not read yet
			}
			if err != nil {
				return nil, err
			}
			s.Collections = append(s.Collections, c)
			grows, err := d.Query(`SELECT g.igdb_id, g.title, g.release_year, g.cover_file, coalesce(g.cover_image, ''), g.parent_igdb_id,
					EXISTS (SELECT 1 FROM series_hidden h WHERE h.collection_id = g.collection_id AND h.igdb_id = g.igdb_id)
				FROM igdb_collection_game g WHERE g.collection_id = ?`, c.ID)
			if err != nil {
				return nil, err
			}
			for grows.Next() {
				g := Game{CollectionID: c.ID}
				var year sql.NullInt64
				var cover sql.NullString
				var hidden bool
				if err := grows.Scan(&g.IGDBID, &g.Title, &year, &cover, &g.V, &g.Parent, &hidden); err != nil {
					grows.Close()
					return nil, err
				}
				if have[g.IGDBID] || fam.has(g.IGDBID, g.Parent) { // in two collections of the same series, or owned
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
		if len(s.Collections) == 0 {
			continue
		}
		sort.Slice(s.Collections, func(i, j int) bool { return s.Collections[i].ID < s.Collections[j].ID })
		sortGames(s.Missing)
		sortGames(s.Hidden)
		out = append(out, s)
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
