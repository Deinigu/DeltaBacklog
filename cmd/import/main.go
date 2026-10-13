// Command import loads a Games_Library CSV export from NocoDB into the DeltaBacklog database.
//
// Export on the Pi:
//
//	docker exec root_db psql -U postgres -d root_db -c "\copy (select ... ) to stdout with csv header" > games.csv
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

func main() {
	dataDir := flag.String("data", "./data", "data directory (app.db and covers/)")
	csvPath := flag.String("csv", "", "path to the exported Games_Library CSV")
	nocodb := flag.String("nocodb", "", "NocoDB base URL (for example http://nocodb.local:8080), needed only to download covers that were uploaded to NocoDB")
	fetch := flag.Bool("covers", true, "download and resize covers after importing")
	flag.Parse()
	if *csvPath == "" {
		log.Fatal("-csv is required")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(filepath.Join(*dataDir, "app.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	f, err := os.Open(*csvPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	rep, jobs, err := importer.Import(d, f, importer.Options{NocoDBBase: *nocodb})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("imported %d games, skipped %d already present, %d warnings\n", rep.Inserted, rep.Skipped, len(rep.Warnings))
	for _, w := range rep.Warnings {
		fmt.Println("  warning:", w)
	}

	if !*fetch {
		return
	}
	// Also retry covers that failed on a previous run.
	rows, err := d.Query("SELECT id, cover_url FROM game WHERE cover_url IS NOT NULL AND cover_file IS NULL")
	if err != nil {
		log.Fatal(err)
	}
	jobs = jobs[:0]
	for rows.Next() {
		var j importer.CoverJob
		rows.Scan(&j.GameID, &j.URL)
		jobs = append(jobs, j)
	}
	rows.Close()

	type result struct {
		id   int64
		name string
		err  error
	}
	in := make(chan importer.CoverJob)
	out := make(chan result)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range in {
				name, err := covers.Fetch(j.URL, filepath.Join(*dataDir, "covers"), j.GameID)
				out <- result{j.GameID, name, err}
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			in <- j
		}
		close(in)
		wg.Wait()
		close(out)
	}()
	ok, failed := 0, 0
	for r := range out {
		if r.err != nil {
			failed++
			fmt.Printf("  cover %d: %v\n", r.id, r.err)
			continue
		}
		ok++
		if _, err := d.Exec("UPDATE game SET cover_file = ?, cover_updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?", r.name, r.id); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("covers: %d downloaded, %d failed (re-run to retry)\n", ok, failed)
}
