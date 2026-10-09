package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestMigrateIsIdempotentAndSeeds(t *testing.T) {
	x := open(t)
	if err := Migrate(x); err != nil {
		t.Fatal(err)
	}
	var v, statuses, tiers int
	x.QueryRow("PRAGMA user_version").Scan(&v)
	x.QueryRow("SELECT count(*) FROM status").Scan(&statuses)
	x.QueryRow("SELECT count(*) FROM tier").Scan(&tiers)
	if v != 7 || statuses != 9 || tiers != 6 {
		t.Errorf("version=%d statuses=%d tiers=%d", v, statuses, tiers)
	}
}

func TestConstraints(t *testing.T) {
	x := open(t)
	bad := []string{
		`INSERT INTO game(title, title_sort, status_id) VALUES ('', '', 1)`,                    // empty title
		`INSERT INTO game(title, title_sort, status_id) VALUES ('x', 'x', 999)`,                // unknown status
		`INSERT INTO game(title, title_sort, status_id, release_year) VALUES ('x','x',1,1800)`, // year range
		`INSERT INTO platform(name) VALUES ('PC'), ('pc')`,                                     // case-insensitive unique
	}
	for _, q := range bad {
		if _, err := x.Exec(q); err == nil {
			t.Errorf("expected constraint failure: %s", q)
		}
	}
	if _, err := x.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('ok','ok',1)`); err != nil {
		t.Fatal(err)
	}
	// play: exactly one of year/period, no duplicates, cascade on delete.
	for _, q := range []string{
		`INSERT INTO play(game_id, year, period) VALUES (1, NULL, NULL)`,
		`INSERT INTO play(game_id, year, period) VALUES (1, 2020, 'always')`,
	} {
		if _, err := x.Exec(q); err == nil {
			t.Errorf("expected play failure: %s", q)
		}
	}
	x.Exec(`INSERT INTO play(game_id, year) VALUES (1, 2020)`)
	if _, err := x.Exec(`INSERT INTO play(game_id, year) VALUES (1, 2020)`); err == nil {
		t.Error("duplicate year should fail")
	}
	x.Exec(`DELETE FROM game WHERE id = 1`)
	var n int
	x.QueryRow(`SELECT count(*) FROM play`).Scan(&n)
	if n != 0 {
		t.Error("play rows should cascade-delete")
	}
}

func TestGetOrCreate(t *testing.T) {
	x := open(t)
	a, err := GetOrCreate(x, "platform", "PC")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GetOrCreate(x, "platform", " pc ")
	if a != b {
		t.Errorf("case-insensitive lookup created a duplicate: %d vs %d", a, b)
	}
	if _, err := GetOrCreate(x, "game; DROP TABLE game", "x"); err == nil {
		t.Error("non-lookup table must be rejected")
	}
}

func TestSavedViewConstraintsAndStarters(t *testing.T) {
	x := open(t)
	var n int
	x.QueryRow(`SELECT count(*) FROM saved_view WHERE pinned = 1`).Scan(&n)
	if n != 3 {
		t.Errorf("pinned starter views = %d, want 3", n)
	}
	for _, q := range []string{
		`INSERT INTO saved_view(name) VALUES ('  ')`,
		`INSERT INTO saved_view(name, layout) VALUES ('x', 'list')`,
		`INSERT INTO saved_view(name, filter_json) VALUES ('x', '{nope')`,
	} {
		if _, err := x.Exec(q); err == nil {
			t.Errorf("expected failure: %s", q)
		}
	}
}

func TestTitleSort(t *testing.T) {
	if got := TitleSort("The Witcher 3"); got != "witcher 3" {
		t.Errorf("got %q", got)
	}
	if got := TitleSort("  ¡Hola!"); got != "hola!" {
		t.Errorf("got %q", got)
	}
}

func TestPathsWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b%20c")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec("INSERT INTO game(title, title_sort, status_id) VALUES ('x','x',1)")
	d.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database is not at the requested path: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "a*")); len(m) != 1 {
		t.Errorf("stray files: %v", m)
	}
	r, err := Check(path)
	if err != nil || r.Games != 1 {
		t.Errorf("check: %+v %v", r, err)
	}
}
