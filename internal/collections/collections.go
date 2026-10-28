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
	Progress func(done, total int) // collections first, then the posters
	// Sleep waits between retries after a 429 (replaced in tests).
	Sleep func(context.Context, time.Duration) error
}

// Report is what a refresh did.
type Report struct {
	Linked      int // games whose collections and parent were read from IGDB
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
	memo := map[int64]int64{} // igdb id -> its parent (0 = none), for the whole run
	res := resolver{ctx: ctx, o: o, src: src, memo: memo}
	n, err := linkLibrary(ctx, d, res)
	rep.Linked += n
	if err != nil {
		var de dbError
		if errors.As(err, &de) {
			return rep, de.err
		}
		return stop(rep, ctx, err)
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
		ids := make([]int64, len(games))
		for i, g := range games {
			memo[g.IGDBID] = g.ParentID
			ids[i] = g.IGDBID
		}
		if err := res.resolve(ctx, ids); err != nil {
			return stop(rep, ctx, err)
		}
		for i := range games {
			games[i].ParentID = res.root(games[i].IGDBID)
		}
		if err := store(d, o.CoverDir, id, name, games); err != nil {
			return rep, err
		}
		rep.Collections++
		rep.Games += len(games)
	}
	progress(len(cols))

	// 3. The release dates of the library's own linked games (a date can move, a new game has none yet).
	if rs, ok := src.(ReleaseSource); ok {
		if err := refreshReleases(ctx, d, rs, o, rep); err != nil {
			return rep, err
		}
		if rep.Stopped != "" {
			return rep, nil
		}
	}

	// 4. Posters of the games the library lacks (not its own family, not the hidden ones); they count in the progress.
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

	// 5. A finished run drops what no series uses any more.
	if err := prune(d, o.CoverDir, used, fam); err != nil {
		return rep, err
	}
	return rep, nil
}

// dbError marks a database failure among the errors linkLibrary returns (the others come from IGDB).
type dbError struct{ err error }

func (e dbError) Error() string { return e.err.Error() }

// linkLibrary reads from IGDB the collections and the version root of the linked library games that were never read
// (igdb_parent_id IS NULL) and stores them. It is a no-op when every root is known. The series refresh and the similar
// games refresh both start with it, so the family rules hold after either. It returns the number of games stored; res.memo
// keeps what was learnt about version chains for the rest of the run.
func linkLibrary(ctx context.Context, d *sql.DB, res resolver) (int, error) {
	linked := 0
	rows, err := d.Query(`SELECT id, igdb_id FROM game WHERE igdb_id IS NOT NULL AND igdb_parent_id IS NULL ORDER BY id`)
	if err != nil {
		return 0, dbError{err}
	}
	byIGDB := map[int64][]int64{} // igdb id -> game ids (an IGDB game is linked once, but be safe)
	var igdbIDs []int64
	for rows.Next() {
		var id, igdbID int64
		if err := rows.Scan(&id, &igdbID); err != nil {
			rows.Close()
			return 0, dbError{err}
		}
		if len(byIGDB[igdbID]) == 0 {
			igdbIDs = append(igdbIDs, igdbID)
		}
		byIGDB[igdbID] = append(byIGDB[igdbID], id)
	}
	if err := rows.Close(); err != nil {
		return 0, dbError{err}
	}
	const batch = 40
	for start := 0; start < len(igdbIDs); start += batch {
		ids := igdbIDs[start:min(start+batch, len(igdbIDs))]
		var got map[int64]meta.GameLinks
		if err := retry429(ctx, res.o, func() (err error) { got, err = res.src.GameLinks(ctx, ids); return }); err != nil {
			return linked, err
		}
		for id, l := range got {
			res.memo[id] = l.ParentID
		}
		if err := res.resolve(ctx, ids); err != nil {
			return linked, err
		}
		tx, err := d.Begin()
		if err != nil {
			return linked, dbError{err}
		}
		for igdbID, l := range got {
			l.ParentID = res.root(igdbID)
			for _, gid := range byIGDB[igdbID] {
				if err := StoreGameLinks(tx, gid, l); err != nil {
					tx.Rollback()
					return linked, dbError{err}
				}
				linked++
			}
		}
		if err := tx.Commit(); err != nil {
			return linked, dbError{err}
		}
	}
	return linked, nil
}

// resolver follows the "another version of" links up to their root. memo holds what is known: igdb id -> its parent
// (0 = none). Entries IGDB lists in no collection (an expanded edition, a port) are asked for on demand.
type resolver struct {
	ctx  context.Context
	o    Options
	src  Source
	memo map[int64]int64
}

// resolve makes sure the chain of every id is known up to its root (five links at most).
func (r resolver) resolve(ctx context.Context, ids []int64) error {
	for round := 0; round < 5; round++ {
		var need []int64
		seen := map[int64]bool{}
		for _, id := range ids {
			for p, depth := r.memo[id], 0; p > 0 && depth < 6; p, depth = r.memo[p], depth+1 {
				if _, known := r.memo[p]; !known && !seen[p] {
					seen[p] = true
					need = append(need, p)
					break
				}
			}
		}
		if len(need) == 0 {
			return nil
		}
		for start := 0; start < len(need); start += 40 {
			batch := need[start:min(start+40, len(need))]
			var got map[int64]meta.GameLinks
			if err := retry429(ctx, r.o, func() (err error) { got, err = r.src.GameLinks(ctx, batch); return }); err != nil {
				return err
			}
			for _, id := range batch {
				r.memo[id] = got[id].ParentID // IGDB does not know it: no parent
			}
		}
	}
	return nil
}

// RootOf resolves the root of the version chain of one IGDB game whose nearest parent is parent (0 = none), asking IGDB
// for the links of the parents (a few requests at most). 0 means the game is its own root.
func RootOf(ctx context.Context, src Source, igdbID, parent int64) (int64, error) {
	r := resolver{ctx: ctx, o: Options{Sleep: sleep}, src: src, memo: map[int64]int64{igdbID: parent}}
	if err := r.resolve(ctx, []int64{igdbID}); err != nil {
		return 0, err
	}
	return r.root(igdbID), nil
}

// root is the last game of the chain starting at id; 0 when id is its own root.
func (r resolver) root(id int64) int64 {
	cur := id
	for i := 0; i < 8; i++ {
		p := r.memo[cur]
		if p <= 0 || p == id {
			break
		}
		cur = p
	}
	if cur == id {
		return 0
	}
	return cur
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

// sweepGrace is how old an unnamed poster file must be before the sweep deletes it.
const sweepGrace = 10 * time.Minute

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
	grows, err := d.Query(`SELECT collection_id, igdb_id, parent_igdb_id, cover_file FROM igdb_collection_game WHERE cover_file IS NOT NULL`)
	if err != nil {
		return err
	}
	type key struct {
		col, id int64
		file    string
	}
	var owned []key
	for grows.Next() {
		var k key
		var parent int64
		if err := grows.Scan(&k.col, &k.id, &parent, &k.file); err != nil {
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
		// the file goes at once when no other row names it (this run made the decision, nothing is mid-download)
		if coverDir != "" && !fileInUse(d, k.file) {
			os.Remove(filepath.Join(coverDir, filepath.Base(k.file)))
		}
	}
	return sweepPosters(d, coverDir)
}

// fileInUse reports whether a row of the series cache or of the similar-games cache names the poster file (the two caches
// share the i<id>.jpg files). A failed query counts as in use: nothing is deleted on a doubt.
func fileInUse(d *sql.DB, file string) bool {
	var n int
	return d.QueryRow(`SELECT (SELECT count(*) FROM igdb_collection_game WHERE cover_file = ?1) + (SELECT count(*) FROM similar_game WHERE cover_file = ?1)`, file).Scan(&n) != nil || n > 0
}

// sweepPosters removes the poster files in coverDir (i<id>.jpg) that no row of either cache names, unless they are fresh.
func sweepPosters(d *sql.DB, coverDir string) error {
	if coverDir == "" {
		return nil
	}
	named := map[string]bool{}
	frows, err := d.Query(`SELECT cover_file FROM igdb_collection_game WHERE cover_file IS NOT NULL UNION SELECT cover_file FROM similar_game WHERE cover_file IS NOT NULL`)
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
		n := e.Name()
		if e.IsDir() || !missingFile.MatchString(n) || named[n] {
			continue
		}
		// A file written in the last minutes may belong to a download of another process (the server's refresh and
		// `sync-igdb --enrich` share the folder) that has not recorded it yet.
		if info, err := e.Info(); err != nil || time.Since(info.ModTime()) < sweepGrace {
			continue
		}
		os.Remove(filepath.Join(coverDir, n))
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
		if _, err := tx.Exec(`INSERT OR REPLACE INTO igdb_collection_game(collection_id, igdb_id, title, release_year, game_type, parent_igdb_id, cover_image, cover_file,
				first_release_date, release_date_format, platforms)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, id, g.IGDBID, g.Name, year, g.GameType, max(g.ParentID, 0), img, file, date, format, platforms); err != nil {
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
		if coverDir != "" && !fileInUse(d, o[1]) {
			os.Remove(filepath.Join(coverDir, filepath.Base(o[1])))
		}
	}
	return nil
}

// dateGraceDays is how long after its release a linked game is still read again, so a date that slipped, or the
// release itself, is noticed; older dates are final.
const dateGraceDays = 90

// refreshReleases stores the release date of the linked games that have no date yet or are not out (or
// only just). The date is IGDB's own data, so it is overwritten; a game IGDB gives no date keeps NULL.
func refreshReleases(ctx context.Context, d *sql.DB, rs ReleaseSource, o Options, rep *Report) error {
	cutoff := time.Now().AddDate(0, 0, -dateGraceDays).Unix()
	rows, err := d.Query(`SELECT id, igdb_id FROM game
		WHERE igdb_id IS NOT NULL AND (first_release_date IS NULL OR first_release_date > ?) ORDER BY id`, cutoff)
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
func SetRelease(x Execer, gameID int64, r meta.Release) error {
	date, format := releaseArgs(r)
	_, err := x.Exec(`UPDATE game SET first_release_date = ?, release_date_format = ? WHERE id = ?`, date, format, gameID)
	return err
}

// SetReleaseIfLinked is SetRelease for a date read in the background: it writes only while the game still has that IGDB
// id, so a lookup that finishes after the game was relinked or unlinked cannot store the old link's date.
func SetReleaseIfLinked(x Execer, gameID, igdbID int64, r meta.Release) error {
	date, format := releaseArgs(r)
	_, err := x.Exec(`UPDATE game SET first_release_date = ?, release_date_format = ? WHERE id = ? AND igdb_id = ?`, date, format, gameID, igdbID)
	return err
}

// Execer is satisfied by *sql.DB and *sql.Tx.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func releaseArgs(r meta.Release) (date, format any) {
	if r.Date > 0 {
		return r.Date, r.Format
	}
	return nil, nil
}
