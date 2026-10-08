package igdblink

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Main runs `deltabacklog sync-igdb [--dry-run] [--db path] [--limit n]` and returns the process exit code.
// defaultDB is used when --db is not given.
func Main(args []string, defaultDB string, mc *meta.Client, out io.Writer) int {
	fs := flag.NewFlagSet("sync-igdb", flag.ContinueOnError)
	fs.SetOutput(out)
	dry := fs.Bool("dry-run", false, "search IGDB and print a report, write nothing")
	dbPath := fs.String("db", defaultDB, "database file")
	limit := fs.Int("limit", 0, "stop after this many games (0 = all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if ok, _ := mc.Configured(); !ok {
		fmt.Fprintln(out, "IGDB is not configured: set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET (in .env or the environment)")
		return 1
	}
	d, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(out, "cannot open the database:", err)
		return 1
	}
	defer d.Close()

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
	rep, err := Run(ctx, d, mc, opts)
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
