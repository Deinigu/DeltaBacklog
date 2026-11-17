package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestExpansionsMigration: a populated library at version 24 keeps everything and gains the three expansion tables, with
// the same constraints as play, a unique IGDB id, cascades from the game, and no status or ownership until the owner sets one.
func TestExpansionsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 24 {
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
		`PRAGMA user_version = 24`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1,'Witcher','witcher',(SELECT id FROM status WHERE code='finished'),1942)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Other','other',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO play(game_id, year) VALUES (1, 2016)`,
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
	count := func(q string, args ...any) int {
		var n int
		if err := d.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(q, err)
		}
		return n
	}
	if v := count(`PRAGMA user_version`); v != 25 {
		t.Fatalf("version %d", v)
	}
	if count(`SELECT count(*) FROM game`) != 2 || count(`SELECT count(*) FROM play`) != 1 {
		t.Fatal("existing data changed")
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(q, err)
		}
	}
	refused := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err == nil {
			t.Fatalf("should be refused: %s", q)
		}
	}
	mustExec(`INSERT INTO expansion(id, game_id, igdb_id, title, release_date, release_year, steam_app_id) VALUES (1, 1, 100, 'Hearts of Stone', '2015-05-13', 2015, 378648)`)
	if count(`SELECT count(*) FROM expansion WHERE id = 1 AND status_id IS NULL AND ownership_id IS NULL AND notes = '' AND hidden = 0`) != 1 {
		t.Error("a new expansion has no status, no ownership and is not hidden")
	}
	refused(`INSERT INTO expansion(game_id, igdb_id, title) VALUES (2, 100, 'Same IGDB id')`)
	refused(`INSERT INTO expansion(game_id, igdb_id, title) VALUES (99, 101, 'No such game')`)
	refused(`INSERT INTO expansion(game_id, igdb_id, title) VALUES (1, 102, '  ')`)
	refused(`UPDATE expansion SET hidden = 2 WHERE id = 1`)
	refused(`UPDATE expansion SET time_to_beat_src = 'guess' WHERE id = 1`)
	mustExec(`UPDATE expansion SET status_id = (SELECT id FROM status WHERE code = 'playing'), time_to_beat_normally = 600, time_to_beat_src = 'manual' WHERE id = 1`)

	// the years played follow play's rules
	mustExec(`INSERT INTO expansion_play(expansion_id, year) VALUES (1, 2016)`)
	mustExec(`INSERT INTO expansion_play(expansion_id, period) VALUES (1, 'childhood')`)
	refused(`INSERT INTO expansion_play(expansion_id, year) VALUES (1, 2016)`)
	refused(`INSERT INTO expansion_play(expansion_id, period) VALUES (1, 'childhood')`)
	refused(`INSERT INTO expansion_play(expansion_id, year, period) VALUES (1, 2017, 'always')`)
	refused(`INSERT INTO expansion_play(expansion_id) VALUES (1)`)
	refused(`INSERT INTO expansion_play(expansion_id, year) VALUES (1, 1969)`)
	refused(`INSERT INTO expansion_play(expansion_id, period) VALUES (1, 'sometimes')`)
	refused(`INSERT INTO expansion_play(expansion_id, year) VALUES (99, 2018)`)

	mustExec(`INSERT INTO expansion_fetch(game_id, fetched_at) VALUES (1, '2026-01-01T00:00:00Z')`)
	// deleting the game takes its expansions, their years and the fetch mark with it
	mustExec(`DELETE FROM game WHERE id = 1`)
	for _, table := range []string{"expansion", "expansion_play", "expansion_fetch"} {
		if n := count(`SELECT count(*) FROM ` + table); n != 0 {
			t.Errorf("%s keeps %d rows of a deleted game", table, n)
		}
	}
}
