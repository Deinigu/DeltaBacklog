package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestTTBNotFoundMigration: a populated library at version 22 keeps its games (and their times) and gains the mark, off
// everywhere; the check refuses anything but 0 and 1.
func TestTTBNotFoundMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 22 {
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
		`PRAGMA user_version = 22`,
		`INSERT INTO game(id, title, title_sort, status_id, time_to_beat_normally, time_to_beat_src) VALUES (1,'Timed','timed',(SELECT id FROM status WHERE code='finished'),600,'manual')`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'No time','no time',(SELECT id FROM status WHERE code='backlog'))`,
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
	count := func(q string) int {
		var n int
		if err := d.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(q, err)
		}
		return n
	}
	if v := count(`PRAGMA user_version`); v != 23 {
		t.Fatalf("version %d", v)
	}
	if count(`SELECT count(*) FROM game`) != 2 || count(`SELECT time_to_beat_normally FROM game WHERE id = 1`) != 600 {
		t.Fatal("existing data changed")
	}
	if count(`SELECT count(*) FROM game WHERE ttb_not_found = 0`) != 2 {
		t.Fatal("the mark must start off")
	}
	if _, err := d.Exec(`UPDATE game SET ttb_not_found = 1 WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE game SET ttb_not_found = 2 WHERE id = 2`); err == nil {
		t.Fatal("ttb_not_found = 2 should be refused")
	}
}
