package steam

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// Main runs `deltabacklog sync-steam [--dry-run] [--db path]` and returns the exit code.
func Main(args []string, defaultDB string, src Source, out io.Writer) int {
	fs := flag.NewFlagSet("sync-steam", flag.ContinueOnError)
	fs.SetOutput(out)
	dry := fs.Bool("dry-run", false, "read Steam and show the counts, change nothing")
	dbPath := fs.String("db", defaultDB, "database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if src == nil || !src.Configured() {
		fmt.Fprintln(out, ErrNotConfigured)
		return 1
	}
	d, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(out, "cannot open the database:", err)
		return 1
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	run := Sync
	if *dry {
		run = DryRun
	}
	rep, err := run(ctx, d, src)
	if err != nil {
		fmt.Fprintln(out, "Steam sync failed:", err)
		return 1
	}
	fmt.Fprintf(out, "%s%s\n", map[bool]string{true: "(dry run, nothing written) "}[*dry], rep)
	return 0
}
