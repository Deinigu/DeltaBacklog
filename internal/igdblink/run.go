package igdblink

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Searcher is what the sync needs from IGDB (satisfied by *meta.Client).
type Searcher interface {
	SearchIGDB(ctx context.Context, title string) ([]meta.Candidate, error)
}

// Options tune a sync run. The zero value is a real run at IGDB's limit of about four requests per second.
type Options struct {
	DryRun     bool
	Interval   time.Duration                              // pause between searches; default 250ms (4 requests/s)
	Backoff    time.Duration                              // first wait after a 429, doubling each time; default 2s
	MaxRetries int                                        // 429s in a row before the run stops (resumable); default 6
	Limit      int                                        // stop after this many games (0 = all)
	BackupTo   string                                     // written before the first change when not a dry run
	Sleep      func(context.Context, time.Duration) error // replaced in tests
	Progress   func(done, total int, title string)
}

// Example is one game in a report, with what IGDB answered.
type Example struct {
	Title string
	Match string // best candidate "Name (year), score N"; empty for not found
}

// Report is what a run found. In a dry run nothing of it was written.
type Report struct {
	DryRun     bool
	Total      int // games to search at the start: no igdb_id, never searched, not marked "not on IGDB"
	Searched   int
	Confident  int
	Uncertain  int
	NotFound   int
	Conflicts  int    // confident matches whose IGDB game is already linked to another library game (counted as uncertain)
	Errors     int    // searches that failed; those games are tried again next run
	Stopped    string // why the run ended early; empty when it finished
	BackupFile string
	Examples   map[Bucket][]Example
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type pending struct {
	id        int64
	title     string
	platforms []string
}

// loadPending returns the games still to search: no link, never searched, not declared absent from IGDB.
func loadPending(d *sql.DB) ([]pending, error) {
	rows, err := d.Query(`SELECT g.id, g.title,
			coalesce((SELECT group_concat(p.name, char(10)) FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = g.id), '')
		FROM game g WHERE g.igdb_id IS NULL AND g.igdb_checked_at IS NULL AND g.igdb_not_found = 0
		ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pending
	for rows.Next() {
		var p pending
		var plats string
		if err := rows.Scan(&p.id, &p.title, &plats); err != nil {
			return nil, err
		}
		if plats != "" {
			p.platforms = strings.Split(plats, "\n")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// searchWithBackoff searches once, waiting and retrying while IGDB answers 429.
func searchWithBackoff(ctx context.Context, s Searcher, title string, o Options) ([]meta.Candidate, error) {
	wait := o.Backoff
	for attempt := 0; ; attempt++ {
		cands, err := s.SearchIGDB(ctx, title)
		var he *meta.HTTPError
		if err == nil || !errors.As(err, &he) || he.Status != 429 {
			return cands, err
		}
		if attempt >= o.MaxRetries {
			return nil, fmt.Errorf("IGDB keeps answering 429 (too many requests): %w", err)
		}
		if err := o.Sleep(ctx, wait); err != nil {
			return nil, err
		}
		wait *= 2
	}
}

// Run links every unlinked, unchecked game. It is resumable: a game is marked checked in the same transaction
// as its outcome, so a stopped run (Ctrl-C, 429s, no network) continues where it left off.
// A dry run searches IGDB but writes nothing, not even the backup.
func Run(ctx context.Context, d *sql.DB, s Searcher, o Options) (*Report, error) {
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
	todo, err := loadPending(d)
	if err != nil {
		return nil, err
	}
	if o.Limit > 0 && len(todo) > o.Limit {
		todo = todo[:o.Limit]
	}
	rep := &Report{DryRun: o.DryRun, Total: len(todo), Examples: map[Bucket][]Example{}}
	if len(todo) == 0 {
		return rep, nil
	}
	if !o.DryRun && o.BackupTo != "" {
		if err := db.Backup(d, o.BackupTo); err != nil {
			return rep, err
		}
		rep.BackupFile = o.BackupTo
	}

	consecutiveErrors := 0
	claimed := map[int64]int64{} // IGDB id -> the game this run linked it to (a dry run writes nothing, so the database cannot tell)
	for i, g := range todo {
		if i > 0 {
			if err := o.Sleep(ctx, o.Interval); err != nil {
				rep.Stopped = "interrupted"
				return rep, nil
			}
		}
		if o.Progress != nil {
			o.Progress(i, len(todo), g.title)
		}
		cands, err := searchWithBackoff(ctx, s, g.title, o)
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
				rep.Stopped = "5 searches in a row failed: " + err.Error()
				return rep, nil
			}
			continue // left unchecked: the next run tries this game again
		}
		consecutiveErrors = 0
		rep.Searched++
		res := Classify(g.title, g.platforms, cands)
		if res.Bucket == Confident {
			other, mine := claimed[res.Best.IGDBID]
			taken := mine && other != g.id
			if !taken {
				var err error
				if taken, err = linkedElsewhere(d, res.Best.IGDBID, g.id); err != nil {
					return rep, err
				}
			}
			if taken { // igdb_id is unique: two library games cannot both take it, so a person decides
				res.Bucket, res.Best = Uncertain, nil
				rep.Conflicts++
			} else {
				claimed[res.Best.IGDBID] = g.id
			}
		}
		rep.count(res, g.title)
		if !o.DryRun {
			if err := apply(d, g.id, res); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

const maxExamples = 8

func (r *Report) count(res Result, title string) {
	switch res.Bucket {
	case Confident:
		r.Confident++
	case Uncertain:
		r.Uncertain++
	default:
		r.NotFound++
	}
	if len(r.Examples[res.Bucket]) >= maxExamples {
		return
	}
	ex := Example{Title: title}
	if len(res.Candidates) > 0 {
		c := res.Candidates[0]
		year := "year unknown"
		if c.Year != 0 {
			year = fmt.Sprint(c.Year)
		}
		ex.Match = fmt.Sprintf("%s (%s), score %d", c.Name, year, c.Score)
	}
	r.Examples[res.Bucket] = append(r.Examples[res.Bucket], ex)
}

// linkedElsewhere reports whether another game already has this IGDB id.
func linkedElsewhere(d *sql.DB, igdbID, except int64) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM game WHERE igdb_id = ? AND id <> ?", igdbID, except).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// apply stores one game's outcome in a single transaction.
func apply(d *sql.DB, gameID int64, res Result) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := now()
	if res.Bucket == Confident {
		if _, err := tx.Exec("UPDATE game SET igdb_id = ?, igdb_synced_at = ?, igdb_checked_at = ? WHERE id = ?", res.Best.IGDBID, t, t, gameID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.Exec("UPDATE game SET igdb_checked_at = ? WHERE id = ?", t, gameID); err != nil {
		return err
	}
	for _, c := range res.Candidates {
		var year any
		if c.Year != 0 {
			year = c.Year
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO sync_candidate (game_id, igdb_id, name, year, score, platforms, slug) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			gameID, c.IGDBID, c.Name, year, c.Score, strings.Join(c.Platforms, ", "), c.Slug); err != nil {
			return err
		}
	}
	return tx.Commit()
}
