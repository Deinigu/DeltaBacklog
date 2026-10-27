// Package collections keeps the games of the IGDB collections behind the user's series, so the Series page can show the
// games the library does not have ("not registered"). The lists are cached in the database and read from IGDB only
// when the owner asks (Series > Refresh from IGDB) or `sync-igdb --enrich` runs, never on a page load.
//
// A user series is mapped to IGDB through its linked games, whatever the series is called: the collection most of its
// linked games share (see pickCollections). A series with no linked game has none and lists nothing. IGDB splits one
// game into several entries (original, expanded edition, remaster); a game counts as owned with its whole family (see
// family), so only games that are really missing are listed.
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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Source is what a refresh needs from IGDB (satisfied by *meta.Client).
type Source interface {
	GameLinks(ctx context.Context, igdbIDs []int64) (map[int64]meta.GameLinks, error)
	CollectionGames(ctx context.Context, collectionID int64) (string, []meta.CollectionGame, error)
}

// Options tune a refresh. The zero value downloads the posters into CoverDir from images.igdb.com only.
type Options struct {
	CoverDir string
	// Fetch downloads a poster (replaced in tests); Allowed vets its URL (default: images.igdb.com over https).
	Fetch    func(url, dir string, igdbID int64) (string, error)
	Allowed  func(rawURL string) bool
	Progress func(done, total int) // collections first, then the posters
	// Sleep waits between retries after a 429 (replaced in tests).
	Sleep func(context.Context, time.Duration) error
}

// Report is what a refresh did.
type Report struct {
	Linked      int // games whose collections and parent were read from IGDB
	Collections int // collections read
	Games       int // games in those collections (before the library's own are left out)
	Covers      int // posters downloaded
	CoverErrors int // posters that failed (tried again next time)
	Errors      int // collections that could not be read
	Stopped     string
}

func (r *Report) String() string {
	s := fmt.Sprintf("%d collections read (%d games), %d game links, %d covers saved", r.Collections, r.Games, r.Linked, r.Covers)
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
// the library lacks. It is resumable: a collection that was read stays read when a later one fails. A run that
// finishes also drops the cache nothing uses any more.
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

	// 1. Linked games whose collections and parent game were never read (a game read by the enrichment already is).
	rows, err := d.Query(`SELECT id, igdb_id FROM game WHERE igdb_id IS NOT NULL AND igdb_parent_id IS NULL ORDER BY id`)
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
		var got map[int64]meta.GameLinks
		if err := retry429(ctx, o, func() (err error) { got, err = src.GameLinks(ctx, ids); return }); err != nil {
			return stop(rep, ctx, err)
		}
		tx, err := d.Begin()
		if err != nil {
			return rep, err
		}
		for igdbID, l := range got {
			for _, gid := range byIGDB[igdbID] {
				if err := StoreGameLinks(tx, gid, l); err != nil {
					tx.Rollback()
					return rep, err
				}
				rep.Linked++
			}
		}
		if err := tx.Commit(); err != nil {
			return rep, err
		}
	}

	// 2. The collection(s) of every series (see pickCollections).
	picked, err := pickCollections(d)
	if err != nil {
		return rep, err
	}
	used := map[int64]bool{}
	var cols []int64
	for _, ps := range picked {
		for _, p := range ps {
			if !used[p.ID] {
				used[p.ID] = true
				cols = append(cols, p.ID)
			}
		}
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i] < cols[j] })
	total := len(cols)
	progress := func(done int) {
		if o.Progress != nil {
			o.Progress(done, total)
		}
	}
	for i, id := range cols {
		progress(i)
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
	progress(len(cols))

	// 3. Posters of the games the library lacks (not its own family, not the hidden ones); they count in the progress.
	fam, err := loadFamily(d)
	if err != nil {
		return rep, err
	}
	type want struct {
		igdbID int64
		image  string
	}
	var wants []want
	queued := map[int64]bool{}
	for _, id := range cols {
		prows, err := d.Query(`SELECT g.igdb_id, g.cover_image, g.parent_igdb_id FROM igdb_collection_game g
			WHERE g.collection_id = ? AND g.cover_image IS NOT NULL AND g.cover_file IS NULL
			  AND NOT EXISTS (SELECT 1 FROM series_hidden h WHERE h.collection_id = g.collection_id AND h.igdb_id = g.igdb_id)
			ORDER BY g.igdb_id`, id)
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
			if !queued[w.igdbID] && !fam.has(w.igdbID, parent) {
				queued[w.igdbID] = true
				wants = append(wants, w)
			}
		}
		if err := prows.Close(); err != nil {
			return rep, err
		}
	}
	total = len(cols) + len(wants)
	for i, w := range wants {
		progress(len(cols) + i)
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
	progress(total)

	// 4. A finished run drops what no series uses any more.
	if err := prune(d, o.CoverDir, used, fam); err != nil {
		return rep, err
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

// missingFile matches the poster files of games the library does not have (covers.MissingName).
var missingFile = regexp.MustCompile(`^i[0-9]+\.jpg$`)

// prune deletes the cached collections no series points at (their games go with them by cascade), forgets the poster
// of a game that has become owned (with its family), and removes poster files that no row names any more.
func prune(d *sql.DB, coverDir string, used map[int64]bool, fam family) error {
	rows, err := d.Query(`SELECT id FROM igdb_collection`)
	if err != nil {
		return err
	}
	var stale []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !used[id] {
			stale = append(stale, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := d.Exec(`DELETE FROM igdb_collection WHERE id = ?`, id); err != nil {
			return err
		}
	}
	grows, err := d.Query(`SELECT collection_id, igdb_id, parent_igdb_id FROM igdb_collection_game WHERE cover_file IS NOT NULL`)
	if err != nil {
		return err
	}
	type key struct{ col, id int64 }
	var owned []key
	for grows.Next() {
		var k key
		var parent int64
		if err := grows.Scan(&k.col, &k.id, &parent); err != nil {
			grows.Close()
			return err
		}
		if fam.has(k.id, parent) { // a game the library has needs no poster from the cache
			owned = append(owned, k)
		}
	}
	if err := grows.Close(); err != nil {
		return err
	}
	for _, k := range owned {
		if _, err := d.Exec(`UPDATE igdb_collection_game SET cover_file = NULL WHERE collection_id = ? AND igdb_id = ?`, k.col, k.id); err != nil {
			return err
		}
	}
	if coverDir == "" {
		return nil
	}
	named := map[string]bool{}
	frows, err := d.Query(`SELECT DISTINCT cover_file FROM igdb_collection_game WHERE cover_file IS NOT NULL`)
	if err != nil {
		return err
	}
	for frows.Next() {
		var f string
		if err := frows.Scan(&f); err != nil {
			frows.Close()
			return err
		}
		named[f] = true
	}
	if err := frows.Close(); err != nil {
		return err
	}
	entries, err := os.ReadDir(coverDir)
	if err != nil {
		return nil // no folder, nothing to clean
	}
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && missingFile.MatchString(n) && !named[n] {
			os.Remove(filepath.Join(coverDir, n))
		}
	}
	return nil
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
		var year, img, file any
		if g.Year > 0 {
			year = g.Year
		}
		if g.CoverID != "" {
			img = g.CoverID
		}
		if o, ok := old[g.IGDBID]; ok && o[0] == g.CoverID && o[1] != "" {
			file = o[1]
			keep[o[1]] = true
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO igdb_collection_game(collection_id, igdb_id, title, release_year, game_type, parent_igdb_id, cover_image, cover_file)
			VALUES (?,?,?,?,?,?,?,?)`, id, g.IGDBID, g.Name, year, g.GameType, max(g.ParentID, 0), img, file); err != nil {
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
