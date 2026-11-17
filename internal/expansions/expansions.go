// Package expansions keeps the IGDB expansions (game_type 2) of the library's games: reading them from IGDB, storing them
// without ever touching what the owner entered, and importing the owned ones from a Steam file (steam.go).
package expansions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// FetchTTL is how long a game's expansions stay fresh: it is not asked again within this time.
const FetchTTL = 30 * 24 * time.Hour

// maxCoversPerRun bounds the posters one run downloads (the rest follow on the next run), to spare the Pi.
const maxCoversPerRun = 60

// timeBatch is how many expansions one time-to-beat request asks for.
const timeBatch = 50

// Source is what a fetch needs from IGDB (satisfied by *meta.Client).
type Source interface {
	Expansions(ctx context.Context, igdbIDs []int64) (map[int64][]meta.Expansion, error)
	TimeToBeat(ctx context.Context, igdbIDs []int64) (map[int64]meta.TimeToBeat, error)
}

// Options tune a fetch. The zero value downloads no posters.
type Options struct {
	CoverDir string
	// Fetch downloads a poster (replaced in tests); Allowed vets its URL (default: images.igdb.com over https).
	Fetch    func(url, dir string, igdbID int64) (string, error)
	Allowed  func(rawURL string) bool
	Progress func(done, total int)
	Now      func() time.Time
}

// Report says what a fetch did.
type Report struct {
	Games         int // games IGDB was asked about
	Found         int // expansions IGDB lists for them
	WithExpansion int // games with at least one
	Covers        int
}

// Target is a linked game to ask IGDB about.
type Target struct{ GameID, IGDBID int64 }

const stamp = "2006-01-02T15:04:05Z"

// untracked is the SQL condition (on the alias e) of an expansion the owner has not touched: nothing is lost when it is
// replaced. Times of IGDB's own do not count as the owner's.
const untracked = `e.status_id IS NULL AND e.ownership_id IS NULL AND e.notes = '' AND (e.time_to_beat_src IS NULL OR e.time_to_beat_src = 'igdb')
	AND NOT EXISTS (SELECT 1 FROM expansion_play p WHERE p.expansion_id = e.id)`

// Due lists the IGDB-linked games whose expansions were never read, or (unless neverOnly) not within FetchTTL.
func Due(d *sql.DB, now time.Time, neverOnly bool) ([]Target, error) {
	q := `SELECT g.id, g.igdb_id FROM game g LEFT JOIN expansion_fetch f ON f.game_id = g.id WHERE g.igdb_id IS NOT NULL AND (f.fetched_at IS NULL`
	var args []any
	if !neverOnly {
		q += ` OR f.fetched_at < ?`
		args = append(args, now.UTC().Add(-FetchTTL).Format(stamp))
	}
	rows, err := d.Query(q+`) ORDER BY g.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.GameID, &t.IGDBID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// IsFresh reports whether the game's expansions were read within FetchTTL.
func IsFresh(d *sql.DB, gameID int64, now time.Time) bool {
	var at string
	if err := d.QueryRow(`SELECT fetched_at FROM expansion_fetch WHERE game_id = ?`, gameID).Scan(&at); err != nil {
		return false
	}
	t, err := time.Parse(stamp, at)
	return err == nil && now.Sub(t) < FetchTTL
}

func igdbImageHost(raw string) bool { return strings.HasPrefix(raw, "https://images.igdb.com/") }

// Fetch reads the expansions of the targets from IGDB, 100 games per request, and stores them. A batch that was read
// stays stored when a later one fails (the error is returned with the report so far). Cancelling ctx stops it.
func Fetch(ctx context.Context, d *sql.DB, src Source, targets []Target, o Options) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Allowed == nil {
		o.Allowed = igdbImageHost
	}
	if o.Fetch == nil {
		o.Fetch = covers.FetchExpansionPosterStrict
	}
	rep := Report{}
	progress := func(done int) {
		if o.Progress != nil {
			o.Progress(done, len(targets))
		}
	}
	progress(0)
	for i := 0; i < len(targets); i += meta.ExpansionBatch {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		end := min(i+meta.ExpansionBatch, len(targets))
		batch := targets[i:end]
		ids := make([]int64, len(batch))
		for k, t := range batch {
			ids[k] = t.IGDBID
		}
		got, err := src.Expansions(ctx, ids)
		if err != nil {
			return rep, err
		}
		tx, err := d.Begin()
		if err != nil {
			return rep, err
		}
		var stored []int64 // the row ids of the expansions of this batch
		for _, t := range batch {
			list := got[t.IGDBID]
			rowIDs, err := save(tx, t.GameID, list, o.Now())
			if err != nil {
				tx.Rollback()
				return rep, err
			}
			stored = append(stored, rowIDs...)
			rep.Games++
			rep.Found += len(rowIDs)
			if len(rowIDs) > 0 {
				rep.WithExpansion++
			}
		}
		if err := tx.Commit(); err != nil {
			return rep, err
		}
		if err := fillTimes(ctx, d, src, stored); err != nil && ctx.Err() != nil {
			return rep, ctx.Err()
		}
		progress(end)
	}
	if o.CoverDir != "" {
		n, err := fetchCovers(ctx, d, o)
		rep.Covers = n
		if err != nil && ctx.Err() != nil {
			return rep, ctx.Err()
		}
	}
	return rep, nil
}

// save stores one game's expansions in the caller's transaction: new ones are added, known ones get IGDB's title, date,
// cover and Steam id (never the owner's status, ownership, notes, years, times or hidden mark), and ones IGDB no longer
// lists go unless the owner tracks them. It returns the row ids of the game's expansions.
func save(tx *sql.Tx, gameID int64, list []meta.Expansion, now time.Time) ([]int64, error) {
	have := map[int64]int64{} // igdb id -> row id, for this game
	rows, err := tx.Query(`SELECT igdb_id, id FROM expansion WHERE game_id = ?`, gameID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ig, id int64
		if err := rows.Scan(&ig, &id); err != nil {
			rows.Close()
			return nil, err
		}
		have[ig] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var ids []int64
	listed := map[int64]bool{}
	for _, e := range list {
		var date, year, steam, cover any
		if e.Release > 0 {
			date = time.Unix(e.Release, 0).UTC().Format("2006-01-02")
			year = e.Year
		}
		if e.SteamAppID > 0 {
			steam = e.SteamAppID
		}
		if e.CoverID != "" {
			cover = e.CoverID
		}
		if id, ok := have[e.IGDBID]; ok {
			listed[e.IGDBID] = true
			// a changed cover drops the downloaded file name: the next run downloads the new one
			if _, err := tx.Exec(`UPDATE expansion SET title = ?, release_date = ?, release_year = ?, steam_app_id = ?,
				cover_file = CASE WHEN cover_image_id IS ? THEN cover_file ELSE NULL END, cover_image_id = ? WHERE id = ?`,
				e.Name, date, year, steam, cover, cover, id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
			continue
		}
		// an expansion IGDB lists under two games stays with the first
		var other int64
		if err := tx.QueryRow(`SELECT id FROM expansion WHERE igdb_id = ?`, e.IGDBID).Scan(&other); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		res, err := tx.Exec(`INSERT INTO expansion(game_id, igdb_id, title, release_date, release_year, cover_image_id, steam_app_id) VALUES (?,?,?,?,?,?,?)`,
			gameID, e.IGDBID, e.Name, date, year, cover, steam)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		listed[e.IGDBID] = true
		ids = append(ids, id)
	}
	for ig, id := range have {
		if listed[ig] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM expansion AS e WHERE e.id = ? AND `+untracked, id); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO expansion_fetch(game_id, fetched_at) VALUES (?, ?) ON CONFLICT(game_id) DO UPDATE SET fetched_at = excluded.fetched_at`,
		gameID, now.UTC().Format(stamp)); err != nil {
		return nil, err
	}
	return ids, nil
}

// fillTimes reads IGDB's time to beat for those of the expansions (row ids) that have none, never over a time the owner
// set or took from HowLongToBeat. A failed read is not fatal: the times come with the next refresh.
func fillTimes(ctx context.Context, d *sql.DB, src Source, rowIDs []int64) error {
	var igdbIDs []int64
	for _, id := range rowIDs {
		var ig int64
		err := d.QueryRow(`SELECT igdb_id FROM expansion WHERE id = ? AND time_to_beat_hastily IS NULL AND time_to_beat_normally IS NULL
			AND time_to_beat_completely IS NULL AND time_to_beat_src IS NULL`, id).Scan(&ig)
		if err == nil {
			igdbIDs = append(igdbIDs, ig)
		}
	}
	for i := 0; i < len(igdbIDs); i += timeBatch {
		if err := ctx.Err(); err != nil {
			return err
		}
		got, err := src.TimeToBeat(ctx, igdbIDs[i:min(i+timeBatch, len(igdbIDs))])
		if err != nil {
			log.Printf("expansions: time to beat: %v", err)
			return err
		}
		nz := func(v int) any {
			if v > 0 {
				return v
			}
			return nil
		}
		for ig, t := range got {
			if t.Hastily <= 0 && t.Normally <= 0 && t.Completely <= 0 {
				continue
			}
			if _, err := d.Exec(`UPDATE expansion SET time_to_beat_hastily = ?, time_to_beat_normally = ?, time_to_beat_completely = ?, time_to_beat_src = 'igdb'
				WHERE igdb_id = ? AND time_to_beat_hastily IS NULL AND time_to_beat_normally IS NULL AND time_to_beat_completely IS NULL AND time_to_beat_src IS NULL`,
				nz(t.Hastily), nz(t.Normally), nz(t.Completely), ig); err != nil {
				return err
			}
		}
	}
	return nil
}

// fetchCovers downloads the posters of expansions that have an IGDB cover but no file yet (at most maxCoversPerRun).
func fetchCovers(ctx context.Context, d *sql.DB, o Options) (int, error) {
	rows, err := d.Query(`SELECT igdb_id, cover_image_id FROM expansion WHERE cover_image_id IS NOT NULL AND cover_file IS NULL ORDER BY id LIMIT ?`, maxCoversPerRun)
	if err != nil {
		return 0, err
	}
	type want struct {
		igdbID int64
		image  string
	}
	var todo []want
	for rows.Next() {
		var w want
		if err := rows.Scan(&w.igdbID, &w.image); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, w)
	}
	rows.Close()
	n := 0
	for _, w := range todo {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		u := meta.CollectionCoverURL(w.image)
		if !o.Allowed(u) {
			continue
		}
		name, err := o.Fetch(u, o.CoverDir, w.igdbID)
		if err != nil {
			log.Printf("expansions: cover of IGDB game %d: %v", w.igdbID, err)
			continue
		}
		if _, err := d.Exec(`UPDATE expansion SET cover_file = ? WHERE igdb_id = ? AND cover_image_id = ?`, name, w.igdbID, w.image); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SweepCovers removes the x<id>.jpg posters no expansion names (unless they are fresh: a download may be in progress).
func SweepCovers(d *sql.DB, coverDir string) error {
	if coverDir == "" {
		return nil
	}
	named := map[string]bool{}
	rows, err := d.Query(`SELECT cover_file FROM expansion WHERE cover_file IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return err
		}
		named[f] = true
	}
	rows.Close()
	entries, err := os.ReadDir(coverDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "x") || !strings.HasSuffix(n, ".jpg") || named[n] {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(coverDir, n))
		}
	}
	return nil
}

// DropUntracked is run when a game's IGDB link changes: its expansions the owner never touched go, and the game is asked
// again the next time. Tracked ones (a status, ownership, notes, years, or a time of their own) stay.
func DropUntracked(tx *sql.Tx, gameID int64) error {
	if _, err := tx.Exec(`DELETE FROM expansion AS e WHERE e.game_id = ? AND `+untracked, gameID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM expansion_fetch WHERE game_id = ?`, gameID)
	return err
}

// Text is the sentence a finished run shows: "Found 12 expansions for 7 games."
func (r Report) Text() string {
	return fmt.Sprintf("Found %s for %s.", plural(r.Found, "expansion", "expansions"), plural(r.WithExpansion, "game", "games"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
