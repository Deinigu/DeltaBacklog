package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestDealsMigration: a populated library at version 15 keeps its games and gains the empty deals tables, which cascade
// from the game.
func TestDealsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 15 {
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
		`PRAGMA user_version = 15`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (1,'Wished','wished',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Done','done',(SELECT id FROM status WHERE code='finished'))`,
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
	var version, games, matches, prices, settings int
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	d.QueryRow(`SELECT count(*) FROM deal_match`).Scan(&matches)
	d.QueryRow(`SELECT count(*) FROM deal_price`).Scan(&prices)
	d.QueryRow(`SELECT count(*) FROM setting`).Scan(&settings)
	if version != 18 || games != 2 || matches != 0 || prices != 0 || settings != 0 {
		t.Fatalf("version %d games %d matches %d prices %d settings %d", version, games, matches, prices, settings)
	}
	for _, q := range []string{
		`INSERT INTO deal_match(game_id, itad_id, source) VALUES (1, 'abc', 'steam')`,
		`INSERT INTO deal_price(game_id, currency, cur_price, cur_cut, cur_url, fetched_at) VALUES (1, 'EUR', 500, 50, 'https://x.example/', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if _, err := d.Exec(`INSERT INTO deal_match(game_id, source) VALUES (2, 'bogus')`); err == nil {
		t.Error("an unknown match source must be refused")
	}
	if _, err := d.Exec(`INSERT INTO deal_price(game_id, currency, cur_url, fetched_at) VALUES (2, 'EUR', 'javascript:alert(1)', 'x')`); err == nil {
		t.Error("a non-http price link must be refused")
	}
	d.Exec(`DELETE FROM game WHERE id = 1`)
	d.QueryRow(`SELECT count(*) FROM deal_match`).Scan(&matches)
	d.QueryRow(`SELECT count(*) FROM deal_price`).Scan(&prices)
	if matches != 0 || prices != 0 {
		t.Errorf("deleting the game must delete its deals: %d %d", matches, prices)
	}
}
