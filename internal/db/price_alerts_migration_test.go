package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestPriceAlertsMigration: a populated library with cached prices at version 19 keeps them, gains the two-year low
// columns (empty) and the alert table, which cascades from the game.
func TestPriceAlertsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 19 {
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
		`PRAGMA user_version = 19`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (1,'A','a',(SELECT id FROM status WHERE code='backlog'))`,
		`INSERT INTO deal_price(game_id, currency, cur_price, low_price, fetched_at) VALUES (1, 'EUR', 500, 400, '2026-01-01T00:00:00Z')`,
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
	var version, price int
	var low2y, checked sql.NullString
	d.QueryRow(`PRAGMA user_version`).Scan(&version)
	d.QueryRow(`SELECT cur_price, low2y_price, low2y_checked_at FROM deal_price WHERE game_id = 1`).Scan(&price, &low2y, &checked)
	if version != 23 || price != 500 || low2y.Valid || checked.Valid {
		t.Fatalf("version %d price %d low2y %v checked %v", version, price, low2y, checked)
	}
	for _, q := range []string{
		`INSERT INTO deal_alert(game_id, muted) VALUES (1, 1)`,
		`UPDATE deal_alert SET last_alert_price = 450, last_alert_kind = 'two_year', last_alert_at = '2026-01-02T00:00:00Z', seen = 0`,
		`UPDATE deal_price SET low2y_price = 450, low2y_checked_at = '2026-01-02T00:00:00Z'`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO deal_alert(game_id) VALUES (9)`,
		`UPDATE deal_alert SET last_alert_kind = 'other'`,
		`UPDATE deal_alert SET muted = 2`,
		`UPDATE deal_price SET low2y_price = -1`,
	} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("accepted: %s", q)
		}
	}
	d.Exec(`DELETE FROM game WHERE id = 1`)
	var n int
	d.QueryRow(`SELECT count(*) FROM deal_alert`).Scan(&n)
	if n != 0 {
		t.Error("deleting the game must delete its alert row")
	}
}
