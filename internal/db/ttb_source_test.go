package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestTimeToBeatSourceMigration: a library at version 11 keeps its IGDB times and they are marked as IGDB's.
func TestTimeToBeatSourceMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 11 {
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
		`PRAGMA user_version = 11`,
		`INSERT INTO game(id, title, title_sort, status_id, time_to_beat_normally) VALUES (1,'Timed','timed',7,600)`,
		`INSERT INTO game(id, title, title_sort, status_id, time_to_beat_completely) VALUES (2,'Complete only','complete only',7,900)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (3,'No time','no time',7)`,
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
	got := map[int64]sql.NullString{}
	rows, _ := d.Query(`SELECT id, time_to_beat_src FROM game`)
	for rows.Next() {
		var id int64
		var s sql.NullString
		rows.Scan(&id, &s)
		got[id] = s
	}
	rows.Close()
	if got[1].String != "igdb" || got[2].String != "igdb" || got[3].Valid {
		t.Errorf("sources: %v", got)
	}
	if _, err := d.Exec(`UPDATE game SET time_to_beat_src = 'guess' WHERE id = 1`); err == nil {
		t.Error("an unknown source was accepted")
	}
}
