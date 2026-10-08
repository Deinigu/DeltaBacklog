package igdblink

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Main runs `deltabacklog sync-igdb [--dry-run] [--db path] [--limit n]` and returns the process exit code.
// defaultDB is used when --db is not given.
func Main(args []string, defaultDB string, mc *meta.Client, out io.Writer) int {
	ok, _ := mc.Configured()
	return run(args, defaultDB, mc, ok, out)
}

// openDryRunCopy opens path read-only and copies it with VACUUM INTO into a temporary file, which is then migrated
// and used instead: the real file is never migrated or otherwise written. The returned func deletes the copy.
func openDryRunCopy(path string) (*sql.DB, func(), error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil, err
	}
	ro, err := sql.Open("sqlite", db.FileURI(path)+"?mode=ro")
	if err != nil {
		return nil, nil, err
	}
	defer ro.Close()
	dir, err := os.MkdirTemp("", "deltabacklog-dryrun-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	tmp := filepath.Join(dir, "copy.db")
	if _, err := ro.Exec("VACUUM INTO '" + strings.ReplaceAll(tmp, "'", "''") + "'"); err != nil {
		cleanup()
		return nil, nil, err
	}
	d, err := db.Open(tmp)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return d, func() { d.Close(); cleanup() }, nil
}

func run(args []string, defaultDB string, s Searcher, configured bool, out io.Writer) int {
	fs := flag.NewFlagSet("sync-igdb", flag.ContinueOnError)
	fs.SetOutput(out)
	dry := fs.Bool("dry-run", false, "search IGDB and print a report; works on a temporary copy, the database is not touched")
	dbPath := fs.String("db", defaultDB, "database file")
	limit := fs.Int("limit", 0, "stop after this many games (0 = all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !configured {
		fmt.Fprintln(out, "IGDB is not configured: set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET (in .env or the environment)")
		return 1
	}
	var d *sql.DB
	var err error
	if *dry {
		var cleanup func()
		d, cleanup, err = openDryRunCopy(*dbPath)
		if err == nil {
			defer cleanup()
		}
	} else {
		d, err = db.Open(*dbPath)
		if err == nil {
			defer d.Close()
		}
	}
	if err != nil {
		fmt.Fprintln(out, "cannot open the database:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts := Options{DryRun: *dry, Limit: *limit}
	if !*dry {
		opts.BackupTo = filepath.Join(filepath.Dir(*dbPath), "app-before-igdb-link-"+time.Now().Format("20060102-150405")+".db")
	}
	opts.Progress = func(done, total int, title string) {
		if done%25 == 0 {
			fmt.Fprintf(out, "  %d/%d  %s\n", done, total, title)
		}
	}
	rep, err := Run(ctx, d, s, opts)
	if rep != nil {
		PrintReport(out, rep)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if rep != nil && rep.Stopped != "" {
		return 1
	}
	return 0
}
