package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSimilarFacetsMigration: a populated library at version 23 keeps its cached suggestions and their links, and the
// two new columns (platforms, genres) start empty (NULL) until the next refresh.
func TestSimilarFacetsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 23 {
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
		`PRAGMA user_version = 23`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1,'Zed','zed',(SELECT id FROM status WHERE code='finished'),100)`,
		`INSERT INTO similar_game(igdb_id, title, release_year, game_type) VALUES (200, 'Alpha', 2005, 0)`,
		`INSERT INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (100, 200)`,
		`INSERT INTO similar_hidden(igdb_id) VALUES (200)`,
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
	var version, games, links, hidden int
	var title string
	var platforms, genres sql.NullString
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	d.QueryRow(`SELECT count(*) FROM similar_link`).Scan(&links)
	d.QueryRow(`SELECT count(*) FROM similar_hidden`).Scan(&hidden)
	if err := d.QueryRow(`SELECT title, platforms, genres FROM similar_game WHERE igdb_id = 200`).Scan(&title, &platforms, &genres); err != nil {
		t.Fatal(err)
	}
	if version != 24 || games != 1 || links != 1 || hidden != 1 || title != "Alpha" || platforms.Valid || genres.Valid {
		t.Errorf("version %d games %d links %d hidden %d title %q platforms %v genres %v", version, games, links, hidden, title, platforms, genres)
	}
	if _, err := d.Exec(`UPDATE similar_game SET platforms = 'PC, PS5', genres = 'Adventure' WHERE igdb_id = 200`); err != nil {
		t.Errorf("the new columns are not writable: %v", err)
	}
}
