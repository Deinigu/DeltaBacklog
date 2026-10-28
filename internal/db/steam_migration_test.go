package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSteamMigration: a populated library at version 16 keeps its games, which gain empty hours, and gets the empty
// steam_app table, whose game link is cleared (not the app) when the game is deleted.
func TestSteamMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 16 {
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
		`PRAGMA user_version = 16`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (1,'Hades','hades',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Done','done',(SELECT id FROM status WHERE code='finished'))`,
		`INSERT INTO game_link(game_id, kind, url) VALUES (1, 'steam', 'https://store.steampowered.com/app/1145360/')`,
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
	var version, games, hours, apps int
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	d.QueryRow(`SELECT count(*) FROM game WHERE playtime_min IS NULL AND steam_last_played IS NULL`).Scan(&hours)
	d.QueryRow(`SELECT count(*) FROM steam_app`).Scan(&apps)
	if version != 17 || games != 2 || hours != 2 || apps != 0 {
		t.Fatalf("version %d games %d without hours %d apps %d", version, games, hours, apps)
	}
	for _, q := range []string{
		`INSERT INTO steam_app(app_id, name, playtime_min, last_played, game_id, source, seen_at) VALUES (1145360, 'Hades', 600, 1700000000, 1, 'link', '2026-01-01T00:00:00Z')`,
		`INSERT INTO steam_app(app_id, name, seen_at) VALUES (10, 'Unmatched', '2026-01-01T00:00:00Z')`,
		`UPDATE game SET playtime_min = 600, steam_last_played = 1700000000 WHERE id = 1`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO steam_app(app_id, name, source, seen_at) VALUES (11, 'x', 'bogus', 'x')`,
		`INSERT INTO steam_app(app_id, name, playtime_min, seen_at) VALUES (12, 'x', -1, 'x')`,
		`INSERT INTO steam_app(app_id, name, game_id, seen_at) VALUES (13, 'x', 999, 'x')`,
		`UPDATE game SET playtime_min = -5 WHERE id = 2`,
	} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("must be refused: %s", q)
		}
	}
	d.Exec(`DELETE FROM game WHERE id = 1`)
	var linked sql.NullInt64
	if err := d.QueryRow(`SELECT game_id FROM steam_app WHERE app_id = 1145360`).Scan(&linked); err != nil || linked.Valid {
		t.Errorf("deleting the game must keep the app and clear its link: %v %v", linked, err)
	}
}
