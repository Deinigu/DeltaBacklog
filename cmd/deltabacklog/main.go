// Command deltabacklog serves the videogame tracker.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/api"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dataDir := env("DATA_DIR", "./data")
	addr := env("LISTEN_ADDR", ":8081")
	if err := os.MkdirAll(filepath.Join(dataDir, "covers"), 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	srv := &api.Server{DB: d, CoverDir: filepath.Join(dataDir, "covers")}
	apiHandler := srv.Handler()
	root := http.NewServeMux()
	root.Handle("/api/", apiHandler)
	root.Handle("/covers/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.Handle("/", web.Handler())
	hs := &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("DeltaBacklog listening on %s (data: %s)", addr, dataDir)
	log.Fatal(hs.ListenAndServe())
}
