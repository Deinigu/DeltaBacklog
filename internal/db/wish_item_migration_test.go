package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestWishItemMigration: a populated library with Deals data at version 20 keeps everything and gains the empty wishlist
// tables; wish_price cascades from wish_item, and the checks refuse bad values.
func TestWishItemMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 20 {
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
		`PRAGMA user_version = 20`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (1,'A','a',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO deal_match(game_id, itad_id, source) VALUES (1, 'x', 'steam')`,
		`INSERT INTO deal_price(game_id, currency, cur_price, low_price, fetched_at) VALUES (1, 'EUR', 500, 400, '2026-01-01T00:00:00Z')`,
		`INSERT INTO deal_alert(game_id, muted) VALUES (1, 1)`,
		`INSERT INTO setting(key, value) VALUES ('deals_country', 'FR')`,
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
	if count(`SELECT count(*) FROM game`) != 1 || count(`SELECT count(*) FROM deal_match`) != 1 || count(`SELECT cur_price FROM deal_price`) != 500 ||
		count(`SELECT muted FROM deal_alert`) != 1 || count(`SELECT count(*) FROM setting`) != 1 {
		t.Fatal("existing data changed")
	}
	if count(`SELECT count(*) FROM wish_item`) != 0 || count(`SELECT count(*) FROM wish_price`) != 0 {
		t.Fatal("the wishlist tables start empty")
	}
	for _, q := range []string{
		`INSERT INTO wish_item(appid, title, seen_at) VALUES (10, 'Hollow Knight', '2026-01-01T00:00:00Z')`,
		`INSERT INTO wish_price(appid, currency, cur_price, cur_url, fetched_at) VALUES (10, 'EUR', 500, 'https://x.example/', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO wish_price(appid, currency, fetched_at) VALUES (99, 'EUR', 'x')`,
		`INSERT INTO wish_item(appid, title, seen_at, muted) VALUES (11, 'x', 'x', 2)`,
		`INSERT INTO wish_item(appid, title, seen_at, last_alert_kind) VALUES (12, 'x', 'x', 'other')`,
		`INSERT INTO wish_item(appid, seen_at) VALUES (13, 'x')`,
		`UPDATE wish_price SET cur_url = 'javascript:alert(1)'`,
		`UPDATE wish_price SET low2y_price = -1`,
	} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("accepted: %s", q)
		}
	}
	d.Exec(`DELETE FROM wish_item WHERE appid = 10`)
	if count(`SELECT count(*) FROM wish_price`) != 0 {
		t.Error("deleting a wishlist game must delete its prices")
	}
}
