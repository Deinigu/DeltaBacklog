// Package expansions keeps the IGDB expansions (game_type 2) of the library's games: reading them from IGDB, storing them
// without ever touching what the owner entered, and importing the owned ones from a Steam file (steam.go).
package expansions

import (
	"context"
	"database/sql"
	"encoding/json"
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
	// AllCovers downloads the missing posters of the whole library (the background job); otherwise only those of the games
	// this fetch read, so a drawer's refresh never waits on other games' posters.
	AllCovers bool
}

// Report says what a fetch did.
type Report struct {
	Games         int // games IGDB was asked about
	Found         int // expansions and DLC IGDB lists for them
	DLC           int // of which DLC (IGDB game_type 1)
	WithExpansion int // games with at least one
	Covers        int
}

// Target is a linked game to ask IGDB about.
type Target struct{ GameID, IGDBID int64 }

const stamp = "2006-01-02T15:04:05Z"

// The release years an expansion may have (the CHECK of expansion.release_year).
const minYear, maxYear = 1950, 2100

// untracked is the SQL condition (on the alias e) of an expansion the owner has not touched: nothing is lost when it is
// replaced. Times of IGDB's own do not count as the owner's.
const untracked = `e.status_id IS NULL AND e.ownership_id IS NULL AND e.notes = '' AND (e.time_to_beat_src IS NULL OR e.time_to_beat_src = 'igdb')
	AND NOT EXISTS (SELECT 1 FROM expansion_play p WHERE p.expansion_id = e.id)`

// IsGameSQL is the SQL condition (on the alias e) of an expansion that is also a game of the library (the owner added it as its own
// card, for example from NocoDB): the card is its record, so it is never listed or counted inside its base game as well.
const IsGameSQL = `EXISTS (SELECT 1 FROM game lg WHERE lg.igdb_id = e.igdb_id)`

// kindOf is the stored kind of an add-on read from IGDB: a DLC, or else an expansion.
func kindOf(e meta.Expansion) string {
	if e.Kind == meta.KindDLC {
		return meta.KindDLC
	}
	return meta.KindExpansion
}

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
			rowIDs, dlc, err := save(tx, t.GameID, list, o.Now())
			if err != nil {
				tx.Rollback()
				return rep, err
			}
			stored = append(stored, rowIDs...)
			rep.Games++
			rep.Found += len(rowIDs)
			rep.DLC += dlc
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
		var only []int64 // the games read: their posters only, unless AllCovers (nil: the whole library)
		if !o.AllCovers {
			only = make([]int64, 0, len(targets))
			for _, t := range targets {
				only = append(only, t.GameID)
			}
		}
		n, err := fetchCovers(ctx, d, o, only)
		rep.Covers = n
		if err != nil && ctx.Err() != nil {
			return rep, ctx.Err()
		}
	}
	return rep, nil
}

// save stores one game's expansions in the caller's transaction: new ones are added, known ones get IGDB's title, date,
// cover and Steam id (never the owner's status, ownership, notes, years, times or hidden mark), and ones IGDB no longer
// lists (or that are a game of the library) go unless the owner tracks them. It returns the row ids of the game's expansions
// and how many of them are DLC.
func save(tx *sql.Tx, gameID int64, list []meta.Expansion, now time.Time) ([]int64, int, error) {
	have := map[int64]int64{} // igdb id -> row id, for this game
	rows, err := tx.Query(`SELECT igdb_id, id FROM expansion WHERE game_id = ?`, gameID)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var ig, id int64
		if err := rows.Scan(&ig, &id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		have[ig] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var ids []int64
	dlc := 0
	listed := map[int64]bool{}
	for _, e := range list {
		// an expansion the library has as its own game is not kept inside its base game too (an untracked row of it goes below)
		var isGame bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM game WHERE igdb_id = ?)`, e.IGDBID).Scan(&isGame); err != nil {
			return nil, 0, err
		} else if isGame {
			continue
		}
		var date, year, steam, cover any
		if t := time.Unix(e.Release, 0).UTC(); e.Release > 0 && t.Year() >= minYear && t.Year() <= maxYear {
			date = t.Format("2006-01-02")
			year = t.Year()
		} // a placeholder date of IGDB's (year 1 or 2999) is no date: the column's CHECK would fail the whole batch
		if e.SteamAppID > 0 {
			steam = e.SteamAppID
		}
		if e.CoverID != "" {
			cover = e.CoverID
		}
		if id, ok := have[e.IGDBID]; ok {
			listed[e.IGDBID] = true
			// a changed cover drops the downloaded file name: the next run downloads the new one
			if _, err := tx.Exec(`UPDATE expansion SET title = ?, release_date = ?, release_year = ?, steam_app_id = ?, kind = ?,
				cover_file = CASE WHEN cover_image_id IS ? THEN cover_file ELSE NULL END, cover_image_id = ? WHERE id = ?`,
				e.Name, date, year, steam, kindOf(e), cover, cover, id); err != nil {
				return nil, 0, err
			}
			ids = append(ids, id)
			if kindOf(e) == meta.KindDLC {
				dlc++
			}
			continue
		}
		// an expansion IGDB lists under two games stays with the first
		var other int64
		if err := tx.QueryRow(`SELECT id FROM expansion WHERE igdb_id = ?`, e.IGDBID).Scan(&other); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, 0, err
		}
		res, err := tx.Exec(`INSERT INTO expansion(game_id, igdb_id, title, release_date, release_year, cover_image_id, steam_app_id, kind) VALUES (?,?,?,?,?,?,?,?)`,
			gameID, e.IGDBID, e.Name, date, year, cover, steam, kindOf(e))
		if err != nil {
			return nil, 0, err
		}
		id, _ := res.LastInsertId()
		listed[e.IGDBID] = true
		ids = append(ids, id)
		if kindOf(e) == meta.KindDLC {
			dlc++
		}
	}
	for ig, id := range have {
		if listed[ig] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM expansion AS e WHERE e.id = ? AND `+untracked, id); err != nil {
			return nil, 0, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO expansion_fetch(game_id, fetched_at) VALUES (?, ?) ON CONFLICT(game_id) DO UPDATE SET fetched_at = excluded.fetched_at`,
		gameID, now.UTC().Format(stamp)); err != nil {
		return nil, 0, err
	}
	return ids, dlc, nil
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

// fetchCovers downloads the posters of expansions that have an IGDB cover but no file yet (at most maxCoversPerRun): those of
// the games `only` when it is not nil, else of the whole library (the owner's first, and no DLC the owner has not added unless
// it is owned on Steam).
func fetchCovers(ctx context.Context, d *sql.DB, o Options, only []int64) (int, error) {
	q, args := `SELECT e.igdb_id, e.cover_image_id FROM expansion e WHERE e.cover_image_id IS NOT NULL AND e.cover_file IS NULL`, []any{}
	order := ` ORDER BY e.id`
	if only != nil {
		if len(only) == 0 {
			return 0, nil
		}
		q += ` AND e.game_id IN (SELECT value FROM json_each(?))`
		b, _ := json.Marshal(only)
		args = append(args, string(b))
	} else {
		// the whole library: not the DLC nobody added (IGDB lists hundreds of cosmetic and song packs for some games; their
		// posters come with a read of their game, in its details), and the owner's own ones first
		q += ` AND NOT (e.kind = 'dlc' AND e.steam_owned = 0 AND ` + untracked + `)`
		order = ` ORDER BY (` + untracked + `), e.id`
	}
	rows, err := d.Query(q+order+` LIMIT ?`, append(args, maxCoversPerRun)...)
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

// Text is the sentence a finished run shows: "Found 12 expansions for 7 games.", or with DLC "Found 5 expansions and 25 DLC for 7 games."
func (r Report) Text() string {
	found := plural(r.Found-r.DLC, "expansion", "expansions")
	if r.DLC > 0 {
		found += fmt.Sprintf(" and %d DLC", r.DLC)
	}
	return fmt.Sprintf("Found %s for %s.", found, plural(r.WithExpansion, "game", "games"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
