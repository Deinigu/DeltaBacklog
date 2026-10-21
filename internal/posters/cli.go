package posters

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// Main runs `deltabacklog fetch-posters [--dry-run] [--limit N] [--upgrade] [--db path]` and returns the exit code.
// It makes the poster of every game that has none, one at a time and a second apart; --upgrade also redoes the posters
// that are only a crop of the wide cover. --dry-run uses no network and writes nothing: it lists what would be done.
func Main(args []string, defaultDB, coverDir string, src Source, allowed func(string) bool, out io.Writer) int {
	fs := flag.NewFlagSet("fetch-posters", flag.ContinueOnError)
	fs.SetOutput(out)
	dry := fs.Bool("dry-run", false, "list the games that would get a poster; no network, nothing is written")
	limit := fs.Int("limit", 0, "stop after this many games (0 = all)")
	upgrade := fs.Bool("upgrade", false, "also redo the posters that are only a crop of the wide cover (never one you chose)")
	dbPath := fs.String("db", defaultDB, "database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 || *limit < 0 {
		fmt.Fprintln(out, "usage: deltabacklog fetch-posters [--dry-run] [--limit N] [--upgrade] [--db path]")
		return 2
	}
	var d *sql.DB
	var err error
	if *dry {
		if _, serr := os.Stat(*dbPath); serr != nil {
			fmt.Fprintln(out, serr)
			return 1
		}
		d, err = sql.Open("sqlite", db.FileURI(*dbPath)+"?mode=ro")
	} else {
		d, err = db.Open(*dbPath)
	}
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	defer d.Close()

	where := `poster_file IS NULL`
	if *upgrade {
		where = `(poster_file IS NULL OR (poster_src = 'crop' AND poster_user = 0))`
	}
	rows, err := d.Query(`SELECT id, title, igdb_id, cover_file, poster_file, poster_src FROM game WHERE ` + where + ` ORDER BY id`)
	if err != nil {
		fmt.Fprintln(out, "cannot read the games (is the database migrated? start the app once):", err)
		return 1
	}
	type job struct {
		id int64
		g  gameRow
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.g.title, &j.g.igdbID, &j.g.coverFile, &j.g.posterFile, &j.g.posterSrc); err != nil {
			rows.Close()
			fmt.Fprintln(out, err)
			return 1
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if *limit > 0 && len(jobs) > *limit {
		jobs = jobs[:*limit]
	}
	if *dry {
		for _, j := range jobs {
			fmt.Fprintf(out, "%5d  %-40.40s  %s\n", j.id, oneLine(j.g.title), describe(j.g))
		}
		fmt.Fprintf(out, "%d games would get a poster (dry run: nothing was changed)\n", len(jobs))
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	m := &Maker{DB: d, Dir: coverDir, Src: src, Allowed: allowed}
	counts := map[string]int{}
	failed := 0
	for i, j := range jobs {
		if ctx.Err() != nil {
			fmt.Fprintln(out, "interrupted")
			break
		}
		how, err := m.Make(ctx, j.id)
		if err != nil {
			failed++
			fmt.Fprintf(out, "[%d/%d] %s: no poster (%v)\n", i+1, len(jobs), oneLine(j.g.title), err)
		} else {
			counts[how]++
			fmt.Fprintf(out, "[%d/%d] %s: %s\n", i+1, len(jobs), oneLine(j.g.title), how)
		}
		time.Sleep(Pause)
	}
	fmt.Fprintf(out, "done: %d from IGDB, %d from SteamGridDB, %d cropped from the cover, %d without a poster\n", counts["igdb"], counts["sgdb"], counts["crop"], failed)
	return 0
}

// Pause is the wait between two games of the command line run (a variable so tests can shorten it).
var Pause = time.Second
