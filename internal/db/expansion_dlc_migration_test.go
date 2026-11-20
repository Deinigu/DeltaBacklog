package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestExpansionDLCMigration: a version-25 library with tracked expansions keeps every row and the owner's data; the rows become
// kind 'expansion', not owned on Steam, and every game is due to be read again (to get its DLC).
func TestExpansionDLCMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 25 {
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
		`PRAGMA user_version = 25`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1,'Skyrim','skyrim',(SELECT id FROM status WHERE code='finished'),72)`,
		`INSERT INTO expansion(id, game_id, igdb_id, title, steam_app_id, status_id, notes) VALUES (1, 1, 6069, 'Dragonborn', 211722, (SELECT id FROM status WHERE code='finished'), 'great')`,
		`INSERT INTO expansion_play(expansion_id, year) VALUES (1, 2013)`,
		`INSERT INTO expansion_fetch(game_id, fetched_at) VALUES (1, '2026-11-17T00:00:00Z')`,
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
	var v, plays, fetched int
	var kind, notes string
	var steam int
	d.QueryRow(`PRAGMA user_version`).Scan(&v)
	d.QueryRow(`SELECT kind, steam_owned, notes FROM expansion WHERE id = 1`).Scan(&kind, &steam, &notes)
	d.QueryRow(`SELECT count(*) FROM expansion_play`).Scan(&plays)
	d.QueryRow(`SELECT count(*) FROM expansion_fetch`).Scan(&fetched)
	if v != 26 || kind != "expansion" || steam != 0 || notes != "great" || plays != 1 || fetched != 0 {
		t.Errorf("version %d kind %q steam %d notes %q plays %d fetched %d", v, kind, steam, notes, plays, fetched)
	}
	if _, err := d.Exec(`UPDATE expansion SET kind = 'pack' WHERE id = 1`); err == nil {
		t.Error("kind accepts only expansion or dlc")
	}
	if _, err := d.Exec(`UPDATE expansion SET steam_owned = 2 WHERE id = 1`); err == nil {
		t.Error("steam_owned is 0 or 1")
	}
}
