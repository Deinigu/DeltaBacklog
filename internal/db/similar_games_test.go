package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSimilarGamesMigration: a populated library at version 14 keeps its games, collection cache and hidden list, and
// gains the (empty) similar-games cache, which cascades from the suggestion to its links.
func TestSimilarGamesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 14 {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, _ := migrationFiles.ReadFile("migrations/" + name)
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 14`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id, favorite) VALUES (1,'Zed','zed',(SELECT id FROM status WHERE code='finished'),100,1)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Wished','wished',(SELECT id FROM status WHERE code='wishlist'))`,
		`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (7, 'Zed Series', '2026-01-01T00:00:00Z')`,
		`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type, cover_file) VALUES (7, 101, 'Zed II', 0, 'i101.jpg')`,
		`INSERT INTO series_hidden(collection_id, igdb_id) VALUES (7, 101)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	old.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var version, games, cached, hidden int
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	d.QueryRow(`SELECT count(*) FROM igdb_collection_game`).Scan(&cached)
	d.QueryRow(`SELECT count(*) FROM series_hidden`).Scan(&hidden)
	if version != 17 || games != 2 || cached != 1 || hidden != 1 {
		t.Errorf("version %d games %d cached %d hidden %d", version, games, cached, hidden)
	}
	for _, table := range []string{"similar_game", "similar_link", "similar_fetch", "similar_hidden"} {
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d rows, %v", table, n, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO similar_game(igdb_id, title, release_year, game_type, cover_image, first_release_date, release_date_format) VALUES (200, 'Alpha', 2005, 0, 'ca', 1104537600, 0)`,
		`INSERT INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (100, 200)`,
		`INSERT INTO similar_fetch(source_igdb_id, fetched_at) VALUES (100, '2026-01-01T00:00:00Z')`,
		`INSERT INTO similar_hidden(igdb_id) VALUES (200)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if _, err := d.Exec(`INSERT INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (100, 999)`); err == nil {
		t.Error("a link to an unknown suggestion was accepted")
	}
	d.Exec(`DELETE FROM similar_game WHERE igdb_id = 200`)
	var links int
	d.QueryRow(`SELECT count(*) FROM similar_link`).Scan(&links)
	if links != 0 {
		t.Errorf("%d links left after their suggestion was deleted", links)
	}
	d.QueryRow(`SELECT count(*) FROM similar_hidden`).Scan(&hidden) // the hide list outlives the cache
	if hidden != 1 {
		t.Errorf("hide list lost: %d", hidden)
	}
}
