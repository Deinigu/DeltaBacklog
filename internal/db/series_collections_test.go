package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSeriesCollectionsMigration: a populated library at version 12 keeps its games, series and IGDB links, and gets
// the (empty) collection cache, the hidden list and the new column.
func TestSeriesCollectionsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 12 {
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
		`PRAGMA user_version = 12`,
		`INSERT INTO series(id, name) VALUES (1, 'Zeds')`,
		`INSERT INTO game(id, title, title_sort, status_id, series_id, igdb_id, igdb_series) VALUES (1,'Zed','zed',7,1,100,'Zed Series')`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Plain','plain',7)`,
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
	var version, games int
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	var collection sql.NullInt64
	var igdbSeries string
	if err := d.QueryRow(`SELECT igdb_collection_id, igdb_series FROM game WHERE id = 1`).Scan(&collection, &igdbSeries); err != nil {
		t.Fatal(err)
	}
	if version < 13 || games != 2 || collection.Valid || igdbSeries != "Zed Series" {
		t.Errorf("version %d, games %d, collection %v, igdb_series %q", version, games, collection, igdbSeries)
	}
	for _, q := range []string{
		`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (7, 'Zed Series', '2026-01-01T00:00:00Z')`,
		`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, release_year, game_type) VALUES (7, 100, 'Zed', 1997, 0)`,
		`INSERT INTO series_hidden(collection_id, igdb_id) VALUES (7, 100)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	// a collection game needs its collection; deleting the collection takes its games along
	if _, err := d.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type) VALUES (8, 1, 'x', 0)`); err == nil {
		t.Error("a game of an unknown collection was accepted")
	}
	d.Exec(`DELETE FROM igdb_collection WHERE id = 7`)
	var n int
	d.QueryRow(`SELECT count(*) FROM igdb_collection_game`).Scan(&n)
	if n != 0 {
		t.Errorf("%d collection games left after their collection was deleted", n)
	}
}
