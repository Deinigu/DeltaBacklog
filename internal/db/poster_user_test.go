package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestPosterUserMigration: a library at version 10 with automatic posters keeps them, none counts as user-chosen.
func TestPosterUserMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 10 {
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
		`PRAGMA user_version = 10`,
		`INSERT INTO game(id, title, title_sort, status_id, poster_file, poster_src) VALUES (1,'A','a',7,'p1.jpg','igdb'), (2,'B','b',7,NULL,NULL)`,
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
	var v, user int
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 11 {
		t.Fatalf("version %d", v)
	}
	if err := d.QueryRow(`SELECT count(*) FROM game WHERE poster_user = 0`).Scan(&user); err != nil || user != 2 {
		t.Errorf("poster_user = 0 for %d games (err %v), want 2", user, err)
	}
	if _, err := d.Exec(`UPDATE game SET poster_user = 2 WHERE id = 1`); err == nil {
		t.Error("poster_user = 2 accepted")
	}
}
