// Command deltabacklog serves the videogame tracker.
package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/api"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/igdblink"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
	"github.com/Deinigu/DeltaBacklog/internal/shots"
	"github.com/Deinigu/DeltaBacklog/web"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads KEY=VALUE lines from path into the environment. Variables that are already set win,
// so a container's real environment always overrides the file. A missing file is fine.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if !ok || k == "" || os.Getenv(k) != "" {
			continue
		}
		os.Setenv(k, v)
		// The Go runtime reads GOMEMLIMIT only at process start, so a value from the file must be applied by hand.
		if k == "GOMEMLIMIT" {
			if n, err := parseMemLimit(v); err != nil {
				log.Printf("ignoring GOMEMLIMIT in %s: %v", path, err)
			} else {
				debug.SetMemoryLimit(n)
			}
		}
	}
}

// parseMemLimit parses a GOMEMLIMIT value: a number of bytes with an optional B, KiB, MiB, GiB or TiB suffix.
func parseMemLimit(v string) (int64, error) {
	v = strings.TrimSpace(v)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40}, {"B", 1}} {
		if strings.HasSuffix(v, u.suffix) {
			v, mult = strings.TrimSuffix(v, u.suffix), u.mult
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult {
		return 0, fmt.Errorf("invalid memory limit %q (use for example 96MiB)", v)
	}
	return n * mult, nil
}

// screenshotsDir is where screenshots live: SCREENSHOTS_DIR when set (explicit: the program never creates it), else
// DATA_DIR/screenshots.
func screenshotsDir(dataDir string) (dir string, explicit bool) {
	if v := os.Getenv("SCREENSHOTS_DIR"); v != "" {
		return v, true
	}
	return filepath.Join(dataDir, "screenshots"), false
}

// envMB reads a size in megabytes (a positive integer) and returns bytes; def is used when unset or invalid.
func envMB(key string, def int64) int64 {
	if n, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil && n > 0 && n < 1<<20 {
		return n << 20
	}
	return def << 20
}

func main() {
	// `deltabacklog check <file>`: verify a database or backup file (read-only; never migrates or changes it).
	if len(os.Args) == 3 && os.Args[1] == "check" {
		r, err := db.Check(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("%s: integrity %s, schema version %d, %d games, %d plays, %d saved views", os.Args[2], r.Integrity, r.Version, r.Games, r.Plays, r.Views)
		if r.Integrity != "ok" || r.Games == 0 {
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] != "sync-igdb" && os.Args[1] != "backup" && os.Args[1] != "backup-screenshots" { // a typo must not start the server
		fmt.Fprint(os.Stderr, "deltabacklog "+version+"\n\n"+
			"usage: deltabacklog                      serve the app\n"+
			"       deltabacklog backup <file>         write a copy of the database\n"+
			"       deltabacklog backup-screenshots <dir>  copy screenshots missing from <dir> (never deletes)\n"+
			"       deltabacklog check <file>          verify a database or backup\n"+
			"       deltabacklog sync-igdb [--dry-run] link games to IGDB\n")
		if os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
			return
		}
		os.Exit(2)
	}

	loadDotEnv(".env")
	dataDir := env("DATA_DIR", "./data")
	addr := env("LISTEN_ADDR", ":8081")

	shotDir, shotExplicit := screenshotsDir(dataDir)

	// `deltabacklog sync-igdb [--dry-run] [--db path]`: link games to IGDB (see docs/developer-manual.md).
	if len(os.Args) > 1 && os.Args[1] == "sync-igdb" {
		mc := meta.New(meta.Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
		os.Exit(igdblink.Main(os.Args[2:], filepath.Join(dataDir, "app.db"), mc, os.Stdout))
	}

	// `deltabacklog backup <file>`: write a consistent copy of the database and exit (used by the nightly timer and
	// before an upgrade). The database is copied as it is: never migrated, never created when it is missing.
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 3 {
			log.Fatal("usage: deltabacklog backup <destination-file>")
		}
		if err := db.BackupFile(filepath.Join(dataDir, "app.db"), os.Args[2]); err != nil {
			log.Fatal(err)
		}
		log.Printf("backup written to %s", os.Args[2])
		return
	}

	// `deltabacklog backup-screenshots <dir>`: copy the screenshot files that <dir> does not have yet (used by the nightly
	// timer). Nothing already in <dir> is changed or removed, so the copy keeps pictures deleted in the app since.
	if len(os.Args) > 1 && os.Args[1] == "backup-screenshots" {
		if len(os.Args) != 3 {
			log.Fatal("usage: deltabacklog backup-screenshots <destination-dir>")
		}
		n, err := shots.SyncNew(shotDir, os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("%d screenshot files copied to %s", n, os.Args[2])
		return
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "covers"), 0o755); err != nil {
		log.Fatal(err)
	}
	// Only the default folder is created here. A configured SCREENSHOTS_DIR (an external drive) must already exist:
	// creating it would silently fill the SD card's empty mount point when the drive is missing.
	if !shotExplicit {
		if err := os.MkdirAll(shotDir, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	d, err := db.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	mc := meta.New(meta.Config{
		IGDBClientID:     os.Getenv("IGDB_CLIENT_ID"),
		IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET"),
		SteamGridDBKey:   os.Getenv("STEAMGRIDDB_API_KEY"),
	})
	igdb, sgdb := mc.Configured()
	log.Printf("metadata: IGDB %s, SteamGridDB %s", onOff(igdb), onOff(sgdb))

	srv := &api.Server{DB: d, CoverDir: filepath.Join(dataDir, "covers"), Meta: mc, ShotDir: shotDir,
		ShotMaxFile: envMB("SCREENSHOT_MAX_MB", 25), ShotMaxRequest: envMB("SCREENSHOT_REQUEST_MAX_MB", 200)}
	if !srv.ShotStorageOK() {
		log.Printf("warning: screenshot storage %s is not available (not mounted or not writable); uploads fail until it is", shotDir)
	}
	srv.Warm()
	handler := newHandler(srv, allowedHosts(os.Getenv("ALLOWED_HOSTS")))
	hs := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute, // a phone that sleeps with a keep-alive connection open must not hold it forever
	}
	log.Printf("DeltaBacklog %s listening on %s (data: %s)", version, addr, dataDir)
	log.Fatal(hs.ListenAndServe())
}

// newHandler is the complete handler chain: the API, covers, health check and the embedded web app, behind the
// cross-site and Host guard. main and the tests both use it.
func newHandler(srv *api.Server, allowed map[string]bool) http.Handler {
	apiHandler := srv.Handler()
	root := http.NewServeMux()
	root.Handle("/api/", apiHandler)
	root.Handle("/covers/", apiHandler)
	root.Handle("/screenshots/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.Handle("/", web.Handler())
	return protect(root, allowed)
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "not configured"
}
