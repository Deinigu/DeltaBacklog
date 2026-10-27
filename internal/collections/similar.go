package collections

// Similar games: IGDB's similar_games of the library's finished and favourite games, cached in the database (migration
// 015) so the Similar page can list the games the library does not have. Like the collections, the lists are read from
// IGDB only when the owner asks (Similar > Refresh from IGDB) or `sync-igdb --enrich` runs, never on a page load. The
// posters are the same i<id>.jpg files the Series page keeps, so the two caches share the files and the sweep.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// SimilarSource is what a similar-games refresh needs from IGDB (satisfied by *meta.Client): the similar games, and the
// version links the root rule needs.
type SimilarSource interface {
	GameLinks(ctx context.Context, igdbIDs []int64) (map[int64]meta.GameLinks, error)
	SimilarGames(ctx context.Context, igdbIDs []int64) (map[int64][]meta.CollectionGame, error)
}

// maxSimilarPosters bounds the posters one refresh downloads (the best ranked suggestions first), to spare the Pi.
const maxSimilarPosters = 300

// sourceSQL selects the library games that suggest others: linked to IGDB, and finished or a favourite. A game on the
// wishlist is not played, so it is never a source.
const sourceSQL = `SELECT g.id, g.igdb_id FROM game g JOIN status s ON s.id = g.status_id
	WHERE g.igdb_id IS NOT NULL AND s.code <> 'wishlist' AND (s.code = 'finished' OR g.favorite = 1) ORDER BY g.id`

// RefreshSimilar reads the similar games of the library's finished and favourite games from IGDB and stores them with
// the posters of the ones the library lacks. It is resumable: a batch that was read stays stored when a later one fails
// (the run then stops with Report.Stopped). A run that finishes also drops the cache nothing uses any more.
func RefreshSimilar(ctx context.Context, d *sql.DB, src SimilarSource, o Options) (*Report, error) {
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Allowed == nil {
		o.Allowed = igdbImageHost
	}
	if o.Fetch == nil {
		o.Fetch = covers.FetchMissingPosterStrict
	}
	rep := &Report{}
	sources, err := similarSources(d)
	if err != nil {
		return rep, err
	}
	total := len(sources)
	progress := func(done int) {
		if o.Progress != nil {
			o.Progress(done, total)
		}
	}

	// 1. The similar games of every source, in batches (one request each), with the roots of their version chains.
	memo := map[int64]int64{}
	res := resolver{ctx: ctx, o: o, src: linksOnly{src}, memo: memo}
	const batch = 40
	for start := 0; start < len(sources); start += batch {
		progress(start)
		ids := sources[start:min(start+batch, len(sources))]
		var got map[int64][]meta.CollectionGame
		if err := retry429(ctx, o, func() (err error) { got, err = src.SimilarGames(ctx, ids); return }); err != nil {
			return stop(rep, ctx, err)
		}
		var all []int64
		for _, list := range got {
			for _, g := range list {
				memo[g.IGDBID] = g.ParentID
				all = append(all, g.IGDBID)
			}
		}
		if err := res.resolve(ctx, all); err != nil {
			return stop(rep, ctx, err)
		}
		if err := storeSimilar(d, got, res); err != nil {
			return rep, err
		}
		rep.Linked += len(got)
		for _, list := range got {
			rep.Games += len(list)
		}
	}
	progress(len(sources))

	// 2. Posters of the suggestions the library lacks, the best ranked first (not the hidden ones).
	fam, err := loadFamily(d)
	if err != nil {
		return rep, err
	}
	type want struct {
		igdbID int64
		image  string
	}
	var wants []want
	prows, err := d.Query(`SELECT g.igdb_id, g.cover_image, g.parent_igdb_id FROM similar_game g
		WHERE g.cover_image IS NOT NULL AND g.cover_file IS NULL
		  AND NOT EXISTS (SELECT 1 FROM similar_hidden h WHERE h.igdb_id = g.igdb_id)
		ORDER BY (SELECT count(*) FROM similar_link l WHERE l.similar_igdb_id = g.igdb_id) DESC, g.igdb_id`)
	if err != nil {
		return rep, err
	}
	for prows.Next() {
		var w want
		var parent int64
		if err := prows.Scan(&w.igdbID, &w.image, &parent); err != nil {
			prows.Close()
			return rep, err
		}
		if !fam.has(w.igdbID, parent) && len(wants) < maxSimilarPosters {
			wants = append(wants, w)
		}
	}
	if err := prows.Close(); err != nil {
		return rep, err
	}
	total = len(sources) + len(wants)
	for i, w := range wants {
		progress(len(sources) + i)
		if ctx.Err() != nil {
			rep.Stopped = "interrupted"
			return rep, nil
		}
		u := meta.CollectionCoverURL(w.image)
		if !o.Allowed(u) {
			rep.CoverErrors++
			continue
		}
		name, err := o.Fetch(u, o.CoverDir, w.igdbID)
		if err != nil {
			log.Printf("similar: cover of IGDB game %d: %v", w.igdbID, err)
			rep.CoverErrors++
			continue
		}
		if _, err := d.Exec(`UPDATE similar_game SET cover_file = ? WHERE igdb_id = ? AND cover_image = ?`, name, w.igdbID, w.image); err != nil {
			return rep, err
		}
		rep.Covers++
	}
	progress(total)

	// 3. A finished run drops what no source suggests any more.
	if err := pruneSimilar(d, o.CoverDir, fam); err != nil {
		return rep, err
	}
	return rep, nil
}

// SimilarString summarises a RefreshSimilar run (Linked counts the games read, Games the suggestions found).
func (r *Report) SimilarString() string {
	s := fmt.Sprintf("%d games read, %d suggestions found, %d covers saved", r.Linked, r.Games, r.Covers)
	if r.CoverErrors > 0 {
		s += fmt.Sprintf(", %d covers failed", r.CoverErrors)
	}
	if r.Stopped != "" {
		s += "; stopped: " + r.Stopped
	}
	return s
}

// linksOnly narrows a SimilarSource to the Source the version-chain resolver needs.
type linksOnly struct{ SimilarSource }

func (linksOnly) CollectionGames(context.Context, int64) (string, []meta.CollectionGame, error) {
	return "", nil, errors.New("not used")
}

// similarSources lists the IGDB ids of the games that suggest others, each once.
func similarSources(d *sql.DB) ([]int64, error) {
	rows, err := d.Query(sourceSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	var out []int64
	for rows.Next() {
		var id, igdbID int64
		if err := rows.Scan(&id, &igdbID); err != nil {
			return nil, err
		}
		if !seen[igdbID] {
			seen[igdbID] = true
			out = append(out, igdbID)
		}
	}
	return out, rows.Err()
}

// storeSimilar replaces the suggestions of the sources in got in one transaction. A suggestion that stays keeps its
// poster file (unless IGDB changed the image); the files of the ones that left are deleted once no row names them.
func storeSimilar(d *sql.DB, got map[int64][]meta.CollectionGame, res resolver) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for source, list := range got {
		if _, err := tx.Exec(`DELETE FROM similar_link WHERE source_igdb_id = ?`, source); err != nil {
			return err
		}
		for _, g := range list {
			if g.IGDBID == source {
				continue
			}
			var year, img, date, format any
			if g.Year > 0 {
				year = g.Year
			}
			if g.Release.Date > 0 {
				date, format = g.Release.Date, g.Release.Format
			}
			if g.CoverID != "" {
				img = g.CoverID
			}
			// the file survives while the image id stays the same
			if _, err := tx.Exec(`INSERT INTO similar_game(igdb_id, title, release_year, game_type, parent_igdb_id, cover_image, first_release_date, release_date_format)
				VALUES (?,?,?,?,?,?,?,?)
				ON CONFLICT(igdb_id) DO UPDATE SET title = excluded.title, release_year = excluded.release_year, game_type = excluded.game_type,
					parent_igdb_id = excluded.parent_igdb_id, first_release_date = excluded.first_release_date, release_date_format = excluded.release_date_format,
					cover_file = CASE WHEN similar_game.cover_image IS excluded.cover_image THEN similar_game.cover_file END,
					cover_image = excluded.cover_image`,
				g.IGDBID, g.Name, year, g.GameType, res.root(g.IGDBID), img, date, format); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (?, ?)`, source, g.IGDBID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO similar_fetch(source_igdb_id, fetched_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%SZ','now'))
			ON CONFLICT(source_igdb_id) DO UPDATE SET fetched_at = excluded.fetched_at`, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// pruneSimilar drops the cache nothing uses: the links and fetch marks of games that no longer suggest anything (not
// linked, not finished or favourite any more, deleted), the suggestions no link names, the poster of a suggestion the
// library has become to own, and the poster files no row of either cache names. The hide list is kept as it is.
func pruneSimilar(d *sql.DB, coverDir string, fam family) error {
	live := `(SELECT g.igdb_id FROM game g JOIN status s ON s.id = g.status_id
		WHERE g.igdb_id IS NOT NULL AND s.code <> 'wishlist' AND (s.code = 'finished' OR g.favorite = 1))`
	for _, q := range []string{
		`DELETE FROM similar_link WHERE source_igdb_id NOT IN ` + live,
		`DELETE FROM similar_fetch WHERE source_igdb_id NOT IN ` + live,
		`DELETE FROM similar_game WHERE igdb_id NOT IN (SELECT similar_igdb_id FROM similar_link)`,
	} {
		if _, err := d.Exec(q); err != nil {
			return err
		}
	}
	rows, err := d.Query(`SELECT igdb_id, parent_igdb_id, cover_file FROM similar_game WHERE cover_file IS NOT NULL`)
	if err != nil {
		return err
	}
	type owned struct {
		id   int64
		file string
	}
	var drop []owned
	for rows.Next() {
		var o owned
		var parent int64
		if err := rows.Scan(&o.id, &parent, &o.file); err != nil {
			rows.Close()
			return err
		}
		if fam.has(o.id, parent) { // a game the library has needs no poster from the cache
			drop = append(drop, o)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, o := range drop {
		if _, err := d.Exec(`UPDATE similar_game SET cover_file = NULL WHERE igdb_id = ?`, o.id); err != nil {
			return err
		}
		if coverDir != "" && !fileInUse(d, o.file) {
			os.Remove(filepath.Join(coverDir, filepath.Base(o.file)))
		}
	}
	return sweepPosters(d, coverDir)
}

// ---- reading the cache ----

// Suggestion is a game IGDB lists as similar to games of the library, and that the library does not have.
type Suggestion struct {
	IGDBID  int64   `json:"igdb_id"`
	Title   string  `json:"title"`
	Year    *int    `json:"year"`
	Cover   *string `json:"cover"`  // poster file name under /covers/, nil until downloaded
	V       string  `json:"v"`      // IGDB's image id of the cover: the cache key of the poster URL
	Parent  int64   `json:"parent"` // root of its version chain, 0 = itself (the browser applies the family rule too)
	Date    *int64  `json:"date"`
	Format  *int    `json:"format"`
	Sources []int64 `json:"sources"` // IGDB ids of the games that suggested it
	Hidden  bool    `json:"hidden"`
}

// SimilarList is what the Similar page needs.
type SimilarList struct {
	Suggestions   []Suggestion `json:"suggestions"`
	OwnedRoots    []int64      `json:"owned_roots"`    // roots of the library's games, wishlist excluded
	WishlistRoots []int64      `json:"wishlist_roots"` // roots of the games on the wishlist
	FetchedAt     string       `json:"fetched_at"`     // when the oldest source was read; "" before the first refresh
}

// rootsWhere lists the roots of the version chains of the library's linked games whose status matches cond, sorted.
func rootsWhere(d *sql.DB, cond string) ([]int64, error) {
	rows, err := d.Query(`SELECT g.igdb_id, coalesce(g.igdb_parent_id, 0) FROM game g JOIN status s ON s.id = g.status_id WHERE g.igdb_id IS NOT NULL AND ` + cond)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	out := []int64{}
	for rows.Next() {
		var id, root int64
		if err := rows.Scan(&id, &root); err != nil {
			return nil, err
		}
		if r := rootOf(id, root); !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, rows.Err()
}

// LoadSimilar reads the cached suggestions: every cached game the library does not own (the whole family, wishlist
// games not counted: those come back as WishlistRoots for the page to mark), hidden ones flagged. One query for the
// suggestions; the page ranks them.
func LoadSimilar(d *sql.DB) (*SimilarList, error) {
	owned, err := rootsWhere(d, `s.code <> 'wishlist'`)
	if err != nil {
		return nil, err
	}
	wish, err := rootsWhere(d, `s.code = 'wishlist'`)
	if err != nil {
		return nil, err
	}
	ownedSet := map[int64]bool{}
	for _, r := range owned {
		ownedSet[r] = true
	}
	out := &SimilarList{Suggestions: []Suggestion{}, OwnedRoots: owned, WishlistRoots: wish}
	rows, err := d.Query(`SELECT g.igdb_id, g.title, g.release_year, g.cover_file, coalesce(g.cover_image,''), g.parent_igdb_id, g.first_release_date, g.release_date_format,
			EXISTS (SELECT 1 FROM similar_hidden h WHERE h.igdb_id = g.igdb_id), group_concat(l.source_igdb_id)
		FROM similar_game g JOIN similar_link l ON l.similar_igdb_id = g.igdb_id GROUP BY g.igdb_id ORDER BY g.igdb_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s Suggestion
		var year sql.NullInt64
		var cover sql.NullString
		var srcs string
		if err := rows.Scan(&s.IGDBID, &s.Title, &year, &cover, &s.V, &s.Parent, &s.Date, &s.Format, &s.Hidden, &srcs); err != nil {
			return nil, err
		}
		if ownedSet[rootOf(s.IGDBID, s.Parent)] {
			continue
		}
		if year.Valid {
			y := int(year.Int64)
			s.Year = &y
		}
		if cover.Valid && cover.String != "" {
			s.Cover = &cover.String
		}
		s.Sources = []int64{}
		for _, f := range strings.Split(srcs, ",") {
			if id, err := strconv.ParseInt(f, 10, 64); err == nil {
				s.Sources = append(s.Sources, id)
			}
		}
		sort.Slice(s.Sources, func(i, j int) bool { return s.Sources[i] < s.Sources[j] })
		out.Suggestions = append(out.Suggestions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var fetched sql.NullString
	if err := d.QueryRow(`SELECT min(fetched_at) FROM similar_fetch`).Scan(&fetched); err != nil {
		return nil, err
	}
	out.FetchedAt = fetched.String
	return out, nil
}

// ErrUnknownSimilar means the game is not in the cached suggestions.
var ErrUnknownSimilar = errors.New("that game is not a cached suggestion")

// HideSimilar stops a suggestion from being listed; UnhideSimilar brings it back. The list is keyed by IGDB id and is
// not the Series page's (series_hidden).
func HideSimilar(d *sql.DB, igdbID int64) error {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM similar_game WHERE igdb_id = ?`, igdbID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknownSimilar
	}
	_, err := d.Exec(`INSERT OR IGNORE INTO similar_hidden(igdb_id) VALUES (?)`, igdbID)
	return err
}

// UnhideSimilar is the reverse of HideSimilar.
func UnhideSimilar(d *sql.DB, igdbID int64) error {
	_, err := d.Exec(`DELETE FROM similar_hidden WHERE igdb_id = ?`, igdbID)
	return err
}
