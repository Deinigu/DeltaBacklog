package igdblink

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Enricher is what the enrichment needs from IGDB (satisfied by *meta.Client).
type Enricher interface {
	GameDetails(ctx context.Context, ids []int64) ([]meta.Details, error)
	TimeToBeat(ctx context.Context, ids []int64) (map[int64]meta.TimeToBeat, error)
}

// EnrichOptions tune an enrichment run. The zero value is a real run at about four requests per second at most.
type EnrichOptions struct {
	DryRun     bool
	Refresh    bool                                       // read games again that were enriched before and rewrite only IGDB's own data (times, modes, perspectives, links)
	BatchSize  int                                        // games per request; default 20
	Interval   time.Duration                              // pause between requests; default 250ms (4 requests/s)
	Backoff    time.Duration                              // first wait after a 429, doubling each time; default 2s
	MaxRetries int                                        // 429s in a row before the run stops (resumable); default 6
	Limit      int                                        // stop after this many games (0 = all)
	BackupTo   string                                     // written before the first change when not a dry run
	Sleep      func(context.Context, time.Duration) error // replaced in tests
	Progress   func(done, total int)
}

// EnrichReport is what an enrichment run did (or, in a dry run, would do on a copy).
type EnrichReport struct {
	DryRun            bool
	Total             int // linked games to read at the start
	Enriched          int // games read from IGDB and stored
	Missing           int // linked games IGDB no longer returns
	TimeToBeat        int // games that got a time to beat
	Modes             int // games with game modes
	Perspectives      int
	Links             int // games with at least one store link
	FilledYear        int // fill-only-empty counters: games whose empty field was filled
	FilledGenres      int
	FilledThemes      int
	FilledDeveloper   int
	FilledSeries      int // series set on games read in this run
	SeriesCompleted   int // series set on games of earlier runs, because a game read now completed their shared collection
	FilledPlatforms   int
	PlatformsSkip     int      // games with no platform where IGDB lists several, so none was guessed
	PlatformsUnmapped int      // games with no platform whose single IGDB platform is not one the library has (never created)
	SeriesProposed    int      // games whose IGDB collection was NOT used as a series (see applySeriesProposals)
	SeriesExamples    []string // a few of them, "Game -> Collection"
	Errors            int      // batches that failed; those games are read again next run
	Stopped           string
	BackupFile        string
}

type linked struct{ id, igdbID int64 }

func loadLinked(d *sql.DB, refresh bool) ([]linked, error) {
	q := "SELECT id, igdb_id FROM game WHERE igdb_id IS NOT NULL"
	if !refresh {
		q += " AND igdb_enriched_at IS NULL"
	}
	rows, err := d.Query(q + " ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []linked
	for rows.Next() {
		var l linked
		if err := rows.Scan(&l.id, &l.igdbID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// withBackoff calls f, waiting and retrying while IGDB answers 429.
func withBackoff(ctx context.Context, o EnrichOptions, f func() error) error {
	wait := o.Backoff
	for attempt := 0; ; attempt++ {
		err := f()
		var he *meta.HTTPError
		if err == nil || !errors.As(err, &he) || he.Status != 429 {
			return err
		}
		if attempt >= o.MaxRetries {
			return fmt.Errorf("IGDB keeps answering 429 (too many requests): %w", err)
		}
		pause := wait
		if he.RetryAfter > 0 { // the service said how long; believe it, within reason
			pause = min(he.RetryAfter, time.Minute)
		}
		if err := o.Sleep(ctx, pause); err != nil {
			return err
		}
		wait *= 2
	}
}

// Enrich reads every linked, not yet enriched game from IGDB and fills in what the owner has not entered:
// release year, genres, themes, developer, series and (when the game has none and IGDB names exactly one) the
// platform; and stores the data that is new in the app (time to beat, game modes, perspectives, store links).
// Owner-entered values are never overwritten. A batch is stored in one transaction together with its
// igdb_enriched_at marks, so a stopped run (Ctrl-C, 429s, no network) continues where it left off.
// A series is only filled when IGDB's collection matches a series the library already has, or when at least two
// library games share it (see applySeriesProposals): IGDB names a collection after almost every standalone game,
// and an empty series means "not part of a series" to the owner.
// The games' updated_at is left alone (the Stats page reads it). A dry run reads IGDB but writes nothing: it does all
// its work in one transaction that is rolled back at the end, so its report is what a real run would do.
func Enrich(ctx context.Context, d *sql.DB, e Enricher, o EnrichOptions) (*EnrichReport, error) {
	if o.BatchSize <= 0 {
		o.BatchSize = 20
	}
	if o.Interval == 0 {
		o.Interval = 250 * time.Millisecond
	}
	if o.Backoff == 0 {
		o.Backoff = 2 * time.Second
	}
	if o.MaxRetries == 0 {
		o.MaxRetries = 6
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	todo, err := loadLinked(d, o.Refresh)
	if err != nil {
		return nil, err
	}
	if o.Limit > 0 && len(todo) > o.Limit {
		todo = todo[:o.Limit]
	}
	rep := &EnrichReport{DryRun: o.DryRun, Total: len(todo)}
	if len(todo) == 0 {
		return rep, nil
	}
	if !o.DryRun && o.BackupTo != "" {
		if err := db.Backup(d, o.BackupTo); err != nil {
			return rep, err
		}
		rep.BackupFile = o.BackupTo
	}

	var dryTx *sql.Tx
	if o.DryRun {
		if dryTx, err = d.Begin(); err != nil {
			return rep, err
		}
		defer dryTx.Rollback()
	}
	readNow := map[int64]bool{} // games read from IGDB in this run
	consecutiveErrors := 0
	first := true
	// pace waits between requests, not before the first one.
	pace := func() error {
		if first {
			first = false
			return nil
		}
		return o.Sleep(ctx, o.Interval)
	}
	for start := 0; start < len(todo); start += o.BatchSize {
		batch := todo[start:min(start+o.BatchSize, len(todo))]
		ids := make([]int64, len(batch))
		for i, l := range batch {
			ids[i] = l.igdbID
		}
		if o.Progress != nil {
			o.Progress(start, len(todo))
		}

		var details []meta.Details
		var ttb map[int64]meta.TimeToBeat
		err := pace()
		if err == nil {
			err = withBackoff(ctx, o, func() (err error) { details, err = e.GameDetails(ctx, ids); return })
		}
		if err == nil {
			if err = pace(); err == nil {
				err = withBackoff(ctx, o, func() (err error) { ttb, err = e.TimeToBeat(ctx, ids); return })
			}
		}
		if err != nil {
			var he *meta.HTTPError
			switch {
			case ctx.Err() != nil:
				rep.Stopped = "interrupted"
				return rep, nil
			case errors.As(err, &he) && he.Status == 429:
				rep.Stopped = err.Error()
				return rep, nil
			}
			rep.Errors++
			if consecutiveErrors++; consecutiveErrors >= 5 {
				rep.Stopped = "5 requests in a row failed: " + err.Error()
				return rep, nil
			}
			continue // left unmarked: the next run reads these games again
		}
		consecutiveErrors = 0

		byID := make(map[int64]meta.Details, len(details))
		for _, det := range details {
			byID[det.IGDBID] = det
		}
		tx := dryTx
		if !o.DryRun {
			if tx, err = d.Begin(); err != nil {
				return rep, err
			}
		}
		for _, l := range batch {
			det, ok := byID[l.igdbID]
			if err := enrichGame(tx, l.id, det, ok, o.Refresh, ttb[l.igdbID], rep); err != nil {
				tx.Rollback()
				return rep, err
			}
			if ok {
				readNow[l.id] = true
				rep.Enriched++
			} else {
				rep.Missing++
			}
		}
		if !o.DryRun {
			if err := tx.Commit(); err != nil {
				return rep, err
			}
		}
	}
	if o.Progress != nil {
		o.Progress(len(todo), len(todo))
	}
	// The series pass looks at every enriched game, also those of earlier runs, so it needs no state of its own.
	tx := dryTx
	if !o.DryRun {
		if tx, err = d.Begin(); err != nil {
			return rep, err
		}
	}
	if err := applySeriesProposals(tx, rep, readNow); err != nil {
		tx.Rollback()
		return rep, err
	}
	if !o.DryRun {
		if err := tx.Commit(); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// maxMinutes is the longest time to beat taken at face value (1000 h): crowd-sourced times have wild outliers.
const maxMinutes = 1000 * 60

// minutes converts IGDB's seconds, rounded to the nearest minute; 0 when IGDB has no (or an absurd) value.
func minutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	if m := max(1, (seconds+30)/60); m <= maxMinutes {
		return m
	}
	return 0
}

// timesToBeat returns the three times as SQL values (nil = unknown). Times that contradict each other (quick longer
// than usual, usual longer than complete) are not trusted at all.
func timesToBeat(t meta.TimeToBeat) (h, n, c any) {
	hm, nm, cm := minutes(t.Hastily), minutes(t.Normally), minutes(t.Completely)
	both := func(a, b int) bool { return a > 0 && b > 0 }
	if (both(hm, nm) && hm > nm) || (both(nm, cm) && nm > cm) || (both(hm, cm) && hm > cm) {
		return nil, nil, nil
	}
	conv := func(m int) any {
		if m == 0 {
			return nil
		}
		return m
	}
	return conv(hm), conv(nm), conv(cm)
}

func isEmpty(tx *sql.Tx, query string, args ...any) (bool, error) {
	var n int
	if err := tx.QueryRow(query, args...).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// fillLinks stores names in a junction table for a game that has none yet. It reports whether it stored any.
func fillLinks(tx *sql.Tx, gameID int64, table, junction, fk string, names []string) (bool, error) {
	if len(names) == 0 {
		return false, nil
	}
	empty, err := isEmpty(tx, "SELECT count(*) FROM "+junction+" WHERE game_id = ?", gameID)
	if err != nil || !empty {
		return false, err
	}
	return true, replaceLinks(tx, gameID, table, junction, fk, names)
}

func replaceLinks(tx *sql.Tx, gameID int64, table, junction, fk string, names []string) error {
	if _, err := tx.Exec("DELETE FROM "+junction+" WHERE game_id = ?", gameID); err != nil {
		return err
	}
	for _, n := range names {
		id, err := db.GetOrCreate(tx, table, n)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO "+junction+" (game_id, "+fk+") VALUES (?, ?)", gameID, id); err != nil {
			return err
		}
	}
	return nil
}

// fillColumn sets a lookup-backed column of game only while it is NULL, and creates the lookup row only then
// (a game that already has a value must not leave IGDB's spelling behind as a new row). It reports whether it did.
func fillColumn(tx *sql.Tx, gameID int64, table, column, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var empty int
	if err := tx.QueryRow("SELECT "+column+" IS NULL FROM game WHERE id = ?", gameID).Scan(&empty); err != nil || empty == 0 {
		return false, err
	}
	id, err := db.GetOrCreate(tx, table, name)
	if err != nil {
		return false, err
	}
	res, err := tx.Exec("UPDATE game SET "+column+" = ? WHERE id = ? AND "+column+" IS NULL", id, gameID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// enrichGame stores one game. With refresh only IGDB's own data is rewritten (times, modes, perspectives, links):
// the fill-only-empty fields are left alone, because a field the owner emptied on purpose looks the same as one
// that was never set.
func enrichGame(tx *sql.Tx, gameID int64, det meta.Details, found, refresh bool, ttb meta.TimeToBeat, rep *EnrichReport) error {
	if found && !refresh {
		// ---- fill only empty fields ----
		if det.Year >= 1950 && det.Year <= 2100 {
			res, err := tx.Exec("UPDATE game SET release_year = ? WHERE id = ? AND release_year IS NULL", det.Year, gameID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				rep.FilledYear++
			}
		}
		if ok, err := fillColumn(tx, gameID, "company", "developer_id", det.Developer); err != nil {
			return err
		} else if ok {
			rep.FilledDeveloper++
		}
		var igdbSeries any
		if det.Series != "" {
			igdbSeries = det.Series
		}
		if _, err := tx.Exec("UPDATE game SET igdb_series = ? WHERE id = ?", igdbSeries, gameID); err != nil {
			return err
		}
		if ok, err := fillSeriesFromLibrary(tx, gameID, det.Series); err != nil {
			return err
		} else if ok {
			rep.FilledSeries++
		}
		if ok, err := fillLinks(tx, gameID, "genre", "game_genre", "genre_id", det.Genres); err != nil {
			return err
		} else if ok {
			rep.FilledGenres++
		}
		themes := make([]string, 0, len(det.Themes))
		for _, t := range det.Themes {
			themes = append(themes, db.NormalizeTheme(t))
		}
		if ok, err := fillLinks(tx, gameID, "theme", "game_theme", "theme_id", themes); err != nil {
			return err
		} else if ok {
			rep.FilledThemes++
		}
		// A game's platform is where the owner has it, not where it was released: with several IGDB platforms
		// there is nothing to guess from, so only a single one is filled in.
		if empty, err := isEmpty(tx, "SELECT count(*) FROM game_platform WHERE game_id = ?", gameID); err != nil {
			return err
		} else if empty && len(det.Platforms) > 0 {
			if len(det.Platforms) == 1 {
				pid, ok, err := resolvePlatform(tx, det.Platforms[0])
				if err != nil {
					return err
				}
				if !ok {
					rep.PlatformsUnmapped++
				} else {
					if _, err := tx.Exec("INSERT OR IGNORE INTO game_platform (game_id, platform_id) VALUES (?, ?)", gameID, pid); err != nil {
						return err
					}
					rep.FilledPlatforms++
				}
			} else {
				rep.PlatformsSkip++
			}
		}
	}
	if found {
		// ---- data that is new in the app: IGDB's values replace what an earlier run stored ----
		// The collections and the parent game behind the Series page's "not registered" games (IGDB's own data).
		if err := collections.StoreGameLinks(tx, gameID, meta.GameLinks{Collections: det.Collections, ParentID: det.ParentID}); err != nil {
			return err
		}
		if len(det.Modes) > 0 {
			if err := replaceLinks(tx, gameID, "game_mode", "game_game_mode", "game_mode_id", det.Modes); err != nil {
				return err
			}
			rep.Modes++
		}
		if len(det.Perspectives) > 0 {
			if err := replaceLinks(tx, gameID, "perspective", "game_perspective", "perspective_id", det.Perspectives); err != nil {
				return err
			}
			rep.Perspectives++
		}
		if len(det.Links) > 0 {
			if _, err := tx.Exec("DELETE FROM game_link WHERE game_id = ?", gameID); err != nil {
				return err
			}
			for _, l := range det.Links {
				if _, err := tx.Exec("INSERT OR REPLACE INTO game_link (game_id, kind, url) VALUES (?, ?, ?)", gameID, l.Kind, l.URL); err != nil {
					return err
				}
			}
			rep.Links++
		}
	}
	if h, n, c := timesToBeat(ttb); h != nil || n != nil || c != nil {
		// coalesce: a value IGDB no longer reports does not erase the one we had. Times the owner typed or took from
		// HowLongToBeat (time_to_beat_src hltb / manual) are not IGDB's to change.
		res, err := tx.Exec(`UPDATE game SET time_to_beat_hastily = coalesce(?, time_to_beat_hastily),
			time_to_beat_normally = coalesce(?, time_to_beat_normally), time_to_beat_completely = coalesce(?, time_to_beat_completely),
			time_to_beat_src = 'igdb'
			WHERE id = ? AND (time_to_beat_src IS NULL OR time_to_beat_src = 'igdb')`, h, n, c, gameID)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k > 0 { // not for games whose times are the owner's or HowLongToBeat's
			rep.TimeToBeat++
		}
	}
	_, err := tx.Exec("UPDATE game SET igdb_enriched_at = ? WHERE id = ?", now(), gameID)
	return err
}

// PrintEnrichReport writes a human summary of a run.
func PrintEnrichReport(w io.Writer, r *EnrichReport) {
	if r.DryRun {
		fmt.Fprintln(w, "DRY RUN: IGDB was read, nothing was written (the work was done on a temporary copy).")
	}
	fmt.Fprintf(w, "Linked games to read: %d\n", r.Total)
	fmt.Fprintf(w, "  read from IGDB:      %d (%d no longer on IGDB)\n", r.Enriched, r.Missing)
	fmt.Fprintf(w, "  time to beat:        %d games\n", r.TimeToBeat)
	fmt.Fprintf(w, "  game modes:          %d games\n", r.Modes)
	fmt.Fprintf(w, "  perspectives:        %d games\n", r.Perspectives)
	fmt.Fprintf(w, "  store links:         %d games\n", r.Links)
	fmt.Fprintln(w, "Empty fields filled (owner-entered values are never changed):")
	fmt.Fprintf(w, "  release year %d, genres %d, themes %d, developer %d, series %d, platform %d\n",
		r.FilledYear, r.FilledGenres, r.FilledThemes, r.FilledDeveloper, r.FilledSeries, r.FilledPlatforms)
	if r.PlatformsSkip > 0 {
		fmt.Fprintf(w, "  %d games have no platform and IGDB lists several: left empty\n", r.PlatformsSkip)
	}
	if r.SeriesCompleted > 0 {
		fmt.Fprintf(w, "  %d games read in earlier runs got a series because a game read now shares their IGDB collection\n", r.SeriesCompleted)
	}
	if r.PlatformsUnmapped > 0 {
		fmt.Fprintf(w, "  %d games have no platform and IGDB's one is not a platform of your library: left empty (none is created)\n", r.PlatformsUnmapped)
	}
	if r.SeriesProposed > 0 {
		fmt.Fprintf(w, "  %d games have an IGDB collection that was not used as a series (it matches no series of yours and no other game shares it), for example:\n", r.SeriesProposed)
		for _, e := range r.SeriesExamples {
			fmt.Fprintln(w, "    "+e)
		}
	}
	if r.Errors > 0 {
		fmt.Fprintf(w, "%d requests failed; those games are read again next run.\n", r.Errors)
	}
	if r.BackupFile != "" {
		fmt.Fprintln(w, "Backup written first:", r.BackupFile)
	}
	if r.Stopped != "" {
		fmt.Fprintln(w, "Stopped early:", r.Stopped, "(run the command again to continue)")
	}
}
