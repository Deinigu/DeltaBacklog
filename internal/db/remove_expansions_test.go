package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestRemoveExpansionsMigration: a populated version-26 library, with an expansion that has plays and a fetch row, loses the
// three expansion tables; every game, play, view and setting row survives and the foreign keys stay consistent.
func TestRemoveExpansionsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 26 {
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
		`PRAGMA user_version = 26`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1,'Witcher','witcher',(SELECT id FROM status WHERE code='finished'),1942)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Other','other',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO play(game_id, year) VALUES (1, 2016)`,
		`INSERT INTO play(game_id, period) VALUES (2, 'childhood')`,
		`INSERT INTO saved_view(name, icon, filter_json, sort_json, pinned, pos) VALUES ('Mine', 'x', '{"in":{"status":["backlog"]}}', '[{"field":"title","dir":"asc"}]', 0, 90)`,
		`INSERT INTO setting(key, value) VALUES ('deals_country', 'ES')`,
		`INSERT INTO expansion(id, game_id, igdb_id, title, kind, steam_owned) VALUES (1, 1, 100, 'Hearts of Stone', 'expansion', 1)`,
		`INSERT INTO expansion_play(expansion_id, year) VALUES (1, 2016)`,
		`INSERT INTO expansion_fetch(game_id, fetched_at) VALUES (1, '2026-01-01T00:00:00Z')`,
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
	if v := count(`PRAGMA user_version`); v != 27 {
		t.Fatalf("version %d", v)
	}
	if n := count(`SELECT count(*) FROM sqlite_master WHERE name LIKE '%expansion%'`); n != 0 {
		t.Errorf("%d expansion objects left", n)
	}
	if count(`SELECT count(*) FROM game`) != 2 || count(`SELECT count(*) FROM play`) != 2 ||
		count(`SELECT count(*) FROM saved_view WHERE name = 'Mine'`) != 1 ||
		count(`SELECT count(*) FROM setting WHERE key = 'deals_country'`) != 1 {
		t.Error("existing data changed")
	}
	if n := count(`SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("foreign_key_check reports %d problems", n)
	}
}
