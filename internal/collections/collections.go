// Package collections keeps the games of the IGDB collections behind the user's series, so the Series page can show the
// games the library does not have ("not registered"). The lists are cached in the database and read from IGDB only
// when the owner asks (Series > Refresh from IGDB) or `sync-igdb --enrich` runs, never on a page load.
//
// A user series is mapped to IGDB through its linked games: the collection of each game (game.igdb_collection_id) is the
// series' collection, whatever the series is called. A series without a linked game has none and lists nothing.
package collections

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Source is what a refresh needs from IGDB (satisfied by *meta.Client).
type Source interface {
	CollectionsOf(ctx context.Context, igdbIDs []int64) (map[int64]meta.CollectionRef, error)
	CollectionGames(ctx context.Context, collectionID int64) (string, []meta.CollectionGame, error)
}

// ReleaseSource is the optional part of the IGDB client that reads the release dates of library games; a Source that
// lacks it only refreshes the collections.
type ReleaseSource interface {
	ReleaseDates(ctx context.Context, igdbIDs []int64) (map[int64]meta.ReleaseOf, error)
}

// Options tune a refresh. The zero value downloads the posters into CoverDir from images.igdb.com only.
type Options struct {
	CoverDir string
	// Fetch downloads a poster (replaced in tests); Allowed vets its URL (default: images.igdb.com over https).
	Fetch    func(url, dir string, igdbID int64) (string, error)
	Allowed  func(rawURL string) bool
	Progress func(done, total int)
	// Sleep waits between retries after a 429 (replaced in tests).
	Sleep func(context.Context, time.Duration) error
}

// Report is what a refresh did.
type Report struct {
	Linked      int // games that got their collection id from IGDB
	Collections int // collections read
	Games       int // games in those collections (before the library's own are left out)
	Dates       int // library games whose release date was stored
	Covers      int // posters downloaded
	CoverErrors int // posters that failed (tried again next time)
	Errors      int // collections that could not be read
	Stopped     string
}

func (r *Report) String() string {
	s := fmt.Sprintf("%d collections read (%d games), %d game links, %d release dates, %d covers saved", r.Collections, r.Games, r.Linked, r.Dates, r.Covers)
	if r.Errors > 0 || r.CoverErrors > 0 {
		s += fmt.Sprintf(", %d collections and %d covers failed", r.Errors, r.CoverErrors)
	}
	if r.Stopped != "" {
		s += "; stopped: " + r.Stopped
	}
	return s
}

func igdbImageHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "images.igdb.com")
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retry429 calls f, waiting and trying again while IGDB answers 429 (at most three times).
func retry429(ctx context.Context, o Options, f func() error) error {
	wait := 2 * time.Second
	for attempt := 0; ; attempt++ {
		err := f()
		var he *meta.HTTPError
		if err == nil || !errors.As(err, &he) || he.Status != 429 || attempt >= 3 {
			return err
		}
		pause := wait
		if he.RetryAfter > 0 {
			pause = min(he.RetryAfter, time.Minute)
		}
		if err := o.Sleep(ctx, pause); err != nil {
			return err
		}
		wait *= 2
	}
}

// Refresh reads from IGDB the collections of the library's series and stores their games and the posters of the games
// the library lacks. It is resumable: a collection that was read stays read when a later one fails.
func Refresh(ctx context.Context, d *sql.DB, src Source, o Options) (*Report, error) {
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

	// 1. Linked games of a series that do not know their collection yet.
	rows, err := d.Query(`SELECT id, igdb_id FROM game WHERE series_id IS NOT NULL AND igdb_id IS NOT NULL AND igdb_collection_id IS NULL ORDER BY id`)
	if err != nil {
		return rep, err
	}
	byIGDB := map[int64][]int64{} // igdb id -> game ids (an IGDB game is linked once, but be safe)
	var igdbIDs []int64
	for rows.Next() {
		var id, igdbID int64
		if err := rows.Scan(&id, &igdbID); err != nil {
			rows.Close()
			return rep, err
		}
		if len(byIGDB[igdbID]) == 0 {
			igdbIDs = append(igdbIDs, igdbID)
		}
		byIGDB[igdbID] = append(byIGDB[igdbID], id)
	}
	if err := rows.Close(); err != nil {
		return rep, err
	}
	const batch = 40
	for start := 0; start < len(igdbIDs); start += batch {
		ids := igdbIDs[start:min(start+batch, len(igdbIDs))]
		var got map[int64]meta.CollectionRef
		if err := retry429(ctx, o, func() (err error) { got, err = src.CollectionsOf(ctx, ids); return }); err != nil {
			return stop(rep, ctx, err)
		}
		for igdbID, ref := range got {
			for _, gid := range byIGDB[igdbID] {
				if _, err := d.Exec(`UPDATE game SET igdb_collection_id = ? WHERE id = ? AND igdb_collection_id IS NULL`, ref.ID, gid); err != nil {
					return rep, err
				}
				rep.Linked++
			}
		}
	}

	// 2. The collections of every series.
	var cols []int64
	crow, err := d.Query(`SELECT DISTINCT igdb_collection_id FROM game WHERE series_id IS NOT NULL AND igdb_collection_id IS NOT NULL ORDER BY 1`)
	if err != nil {
		return rep, err
	}
	for crow.Next() {
		var id int64
		if err := crow.Scan(&id); err != nil {
			crow.Close()
			return rep, err
		}
		cols = append(cols, id)
	}
	if err := crow.Close(); err != nil {
		return rep, err
	}
	for i, id := range cols {
		if o.Progress != nil {
			o.Progress(i, len(cols))
		}
		var name string
		var games []meta.CollectionGame
		if err := retry429(ctx, o, func() (err error) { name, games, err = src.CollectionGames(ctx, id); return }); err != nil {
			var he *meta.HTTPError
			if ctx.Err() != nil || (errors.As(err, &he) && he.Status == 429) {
				return stop(rep, ctx, err)
			}
			log.Printf("collections: %d: %v", id, err)
			if rep.Errors++; rep.Errors >= 5 {
				rep.Stopped = "5 collections in a row failed: " + err.Error()
				return rep, nil
			}
			continue
		}
		rep.Errors = 0 // "in a row"
		if err := store(d, o.CoverDir, id, name, games); err != nil {
			return rep, err
		}
		rep.Collections++
		rep.Games += len(games)
	}
	if o.Progress != nil {
		o.Progress(len(cols), len(cols))
	}

	// 3. The release dates of the library's own linked games (a wishlist game's date moves, a new one has none yet).
	if rs, ok := src.(ReleaseSource); ok {
		if err := refreshReleases(ctx, d, rs, o, rep); err != nil {
			return rep, err
		}
		if rep.Stopped != "" {
			return rep, nil
		}
	}

	// 4. Posters of the games the library lacks (the library's own games have theirs).
	prows, err := d.Query(`SELECT DISTINCT g.igdb_id, g.cover_image FROM igdb_collection_game g
		WHERE g.cover_image IS NOT NULL AND g.cover_file IS NULL
		  AND g.collection_id IN (SELECT igdb_collection_id FROM game WHERE series_id IS NOT NULL AND igdb_collection_id IS NOT NULL)
		  AND g.igdb_id NOT IN (SELECT igdb_id FROM game WHERE igdb_id IS NOT NULL)
		  AND NOT EXISTS (SELECT 1 FROM series_hidden h WHERE h.collection_id = g.collection_id AND h.igdb_id = g.igdb_id)
		ORDER BY g.igdb_id`)
	if err != nil {
		return rep, err
	}
	type want struct {
		igdbID int64
		image  string
	}
	var wants []want
	for prows.Next() {
		var w want
		if err := prows.Scan(&w.igdbID, &w.image); err != nil {
			prows.Close()
			return rep, err
		}
		wants = append(wants, w)
	}
	if err := prows.Close(); err != nil {
		return rep, err
	}
	for _, w := range wants {
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
			log.Printf("collections: cover of IGDB game %d: %v", w.igdbID, err)
			rep.CoverErrors++
			continue
		}
		if _, err := d.Exec(`UPDATE igdb_collection_game SET cover_file = ? WHERE igdb_id = ? AND cover_image = ?`, name, w.igdbID, w.image); err != nil {
			return rep, err
		}
		rep.Covers++
	}
	return rep, nil
}

// stop ends a refresh that IGDB (or the caller) interrupted; what was stored stays.
func stop(rep *Report, ctx context.Context, err error) (*Report, error) {
	if ctx.Err() != nil {
		rep.Stopped = "interrupted"
		return rep, nil
	}
	rep.Stopped = err.Error()
	return rep, nil
}

// store replaces the cached games of one collection in one transaction. A game that stays keeps its poster file (unless
// IGDB changed the image); posters of games that left the collection are deleted from disk.
func store(d *sql.DB, coverDir string, id int64, name string, games []meta.CollectionGame) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if name == "" {
		_ = tx.QueryRow(`SELECT name FROM igdb_collection WHERE id = ?`, id).Scan(&name)
	}
	if name == "" {
		name = fmt.Sprintf("Collection %d", id)
	}
	if _, err := tx.Exec(`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, fetched_at = excluded.fetched_at`, id, name); err != nil {
		return err
	}
	old := map[int64][2]string{} // igdb id -> image, file
	rows, err := tx.Query(`SELECT igdb_id, coalesce(cover_image,''), coalesce(cover_file,'') FROM igdb_collection_game WHERE collection_id = ?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var gid int64
		var img, file string
		if err := rows.Scan(&gid, &img, &file); err != nil {
			rows.Close()
			return err
		}
		old[gid] = [2]string{img, file}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM igdb_collection_game WHERE collection_id = ?`, id); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, g := range games {
		var year, img, file, date, format, platforms any
		if g.Year > 0 {
			year = g.Year
		}
		if g.Release.Date > 0 {
			date, format = g.Release.Date, g.Release.Format
		}
		if len(g.Platforms) > 0 {
			platforms = strings.Join(g.Platforms, ", ")
		}
		if g.CoverID != "" {
			img = g.CoverID
		}
		if o, ok := old[g.IGDBID]; ok && o[0] == g.CoverID && o[1] != "" {
			file = o[1]
			keep[o[1]] = true
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO igdb_collection_game(collection_id, igdb_id, title, release_year, game_type, cover_image, cover_file,
				first_release_date, release_date_format, platforms)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, g.IGDBID, g.Name, year, g.GameType, img, file, date, format, platforms); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// The same game in another cached collection may still use a file: only delete what no row mentions any more.
	for _, o := range old {
		if o[1] == "" || keep[o[1]] {
			continue
		}
		var n int
		if d.QueryRow(`SELECT count(*) FROM igdb_collection_game WHERE cover_file = ?`, o[1]).Scan(&n) == nil && n == 0 && coverDir != "" {
			os.Remove(filepath.Join(coverDir, filepath.Base(o[1])))
		}
	}
	return nil
}

// dateGraceDays is how long after its release a linked game is still read again, so a date that slipped, or the
// release itself, is noticed; older dates are final.
const dateGraceDays = 90

// refreshReleases stores the release date of the linked games that are on the wishlist, have no date yet or are
// not out (or only just). The date is IGDB's own data, so it is overwritten; a game IGDB gives no date keeps NULL.
func refreshReleases(ctx context.Context, d *sql.DB, rs ReleaseSource, o Options, rep *Report) error {
	cutoff := time.Now().AddDate(0, 0, -dateGraceDays).Unix()
	rows, err := d.Query(`SELECT g.id, g.igdb_id FROM game g JOIN status s ON s.id = g.status_id
		WHERE g.igdb_id IS NOT NULL AND (s.code = 'wishlist' OR g.first_release_date IS NULL OR g.first_release_date > ?) ORDER BY g.id`, cutoff)
	if err != nil {
		return err
	}
	byIGDB := map[int64][]int64{}
	var igdbIDs []int64
	for rows.Next() {
		var id, igdbID int64
		if err := rows.Scan(&id, &igdbID); err != nil {
			rows.Close()
			return err
		}
		if len(byIGDB[igdbID]) == 0 {
			igdbIDs = append(igdbIDs, igdbID)
		}
		byIGDB[igdbID] = append(byIGDB[igdbID], id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	const batch = 40
	for start := 0; start < len(igdbIDs); start += batch {
		ids := igdbIDs[start:min(start+batch, len(igdbIDs))]
		var got map[int64]meta.ReleaseOf
		if err := retry429(ctx, o, func() (err error) { got, err = rs.ReleaseDates(ctx, ids); return }); err != nil {
			_, _ = stop(rep, ctx, err)
			return nil
		}
		for igdbID, r := range got {
			for _, gid := range byIGDB[igdbID] {
				if err := SetRelease(d, gid, r.Release); err != nil {
					return err
				}
				if r.Release.Date > 0 {
					rep.Dates++
				}
			}
		}
	}
	return nil
}

// SetRelease stores (or, for a zero Release, clears) the IGDB release date of a library game.
func SetRelease(x interface {
	Exec(query string, args ...any) (sql.Result, error)
}, gameID int64, r meta.Release) error {
	var date, format any
	if r.Date > 0 {
		date, format = r.Date, r.Format
	}
	_, err := x.Exec(`UPDATE game SET first_release_date = ?, release_date_format = ? WHERE id = ?`, date, format, gameID)
	return err
}
