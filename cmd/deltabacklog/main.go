// Command deltabacklog serves the videogame tracker.
package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/api"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/igdblink"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
	"github.com/Deinigu/DeltaBacklog/web"
)

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
	}
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

	if len(os.Args) > 1 && os.Args[1] != "sync-igdb" && os.Args[1] != "backup" { // a typo must not start the server
		fmt.Fprint(os.Stderr, "usage: deltabacklog                      serve the app\n"+
			"       deltabacklog backup <file>         write a copy of the database\n"+
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

	// `deltabacklog sync-igdb [--dry-run] [--db path]`: link games to IGDB (see docs/developer-manual.md).
	if len(os.Args) > 1 && os.Args[1] == "sync-igdb" {
		mc := meta.New(meta.Config{IGDBClientID: os.Getenv("IGDB_CLIENT_ID"), IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET")})
		os.Exit(igdblink.Main(os.Args[2:], filepath.Join(dataDir, "app.db"), mc, os.Stdout))
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "covers"), 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	// `deltabacklog backup <file>`: write a consistent copy of the database and exit (used by the nightly timer).
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 3 {
			log.Fatal("usage: deltabacklog backup <destination-file>")
		}
		if err := db.Backup(d, os.Args[2]); err != nil {
			log.Fatal(err)
		}
		log.Printf("backup written to %s", os.Args[2])
		return
	}

	mc := meta.New(meta.Config{
		IGDBClientID:     os.Getenv("IGDB_CLIENT_ID"),
		IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET"),
		SteamGridDBKey:   os.Getenv("STEAMGRIDDB_API_KEY"),
	})
	igdb, sgdb := mc.Configured()
	log.Printf("metadata: IGDB %s, SteamGridDB %s", onOff(igdb), onOff(sgdb))

	srv := &api.Server{DB: d, CoverDir: filepath.Join(dataDir, "covers"), Meta: mc}
	srv.Warm()
	apiHandler := srv.Handler()
	root := http.NewServeMux()
	root.Handle("/api/", apiHandler)
	root.Handle("/covers/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.Handle("/", web.Handler())
	hs := &http.Server{
		Addr:              addr,
		Handler:           protect(root, allowedHosts(os.Getenv("ALLOWED_HOSTS"))),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("DeltaBacklog listening on %s (data: %s)", addr, dataDir)
	log.Fatal(hs.ListenAndServe())
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "not configured"
}
