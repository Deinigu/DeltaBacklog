package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// versionNine writes a database at schema version 9 (the last one with the per-year tier list) holding tier lists.
func versionNine(t *testing.T, path string) {
	t.Helper()
	d, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 9 {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, _ := migrationFiles.ReadFile("migrations/" + name)
		if _, err := d.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 9`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (1,'A','a',7), (2,'B','b',7), (3,'C','c',7), (4,'D','d',7)`,
		// 2024: A and B played; C played 2024 and 2025. 2025: C and D.
		`INSERT INTO play(game_id, year) VALUES (1,2024), (2,2024), (3,2024), (3,2025), (4,2025)`,
		`UPDATE tier SET name = 'Me ha cambiado la vida' WHERE name = 'S'`,
		// 2024: B in S (first), A in S (second). 2025: C in B. D is not ranked. Game 4 also has a ghost entry in 2023
		// (it no longer has that year).
		`INSERT INTO tierlist_entry(year, game_id, tier_id, pos) VALUES (2024,2,1,0), (2024,1,1,1), (2025,3,3,0), (2023,4,2,0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
}

func TestRankingsMigrationKeepsTierLists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	versionNine(t, path)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var v int
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 10 {
		t.Fatalf("version %d", v)
	}
	names := func() (out []string) {
		rows, _ := d.Query(`SELECT name FROM ranking ORDER BY pos`)
		defer rows.Close()
		for rows.Next() {
			var n string
			rows.Scan(&n)
			out = append(out, n)
		}
		return
	}()
	if strings.Join(names, ",") != "Tier list 2025,Tier list 2024,Tier list 2023" {
		t.Fatalf("rankings = %v", names)
	}
	placement := func(ranking string) string {
		rows, err := d.Query(`SELECT e.game_id || ':' || coalesce(t.label, '-') FROM ranking_entry e JOIN ranking r ON r.id = e.ranking_id
			LEFT JOIN ranking_tier t ON t.id = e.tier_id WHERE r.name = ?
			ORDER BY coalesce(t.pos, 99), e.pos, e.game_id`, ranking)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			rows.Scan(&s)
			out = append(out, s)
		}
		return strings.Join(out, " ")
	}
	for ranking, want := range map[string]string{
		"Tier list 2024": "2:Me ha cambiado la vida 1:Me ha cambiado la vida 3:-", // C is the unranked rest of the year
		"Tier list 2025": "3:B 4:-",
		"Tier list 2023": "4:A", // the ghost entry of a game that lost the year is kept
	} {
		if got := placement(ranking); got != want {
			t.Errorf("%s = %q, want %q", ranking, got, want)
		}
	}
	// Every ranking got its own copy of the six tiers, in order, with the colours.
	var tiers, distinct int
	d.QueryRow(`SELECT count(*), count(DISTINCT ranking_id) FROM ranking_tier`).Scan(&tiers, &distinct)
	if tiers != 18 || distinct != 3 {
		t.Errorf("tiers=%d rankings=%d", tiers, distinct)
	}
	var first, color string
	d.QueryRow(`SELECT label, color FROM ranking_tier WHERE ranking_id = (SELECT id FROM ranking WHERE name = 'Tier list 2024') ORDER BY pos LIMIT 1`).Scan(&first, &color)
	if first != "Me ha cambiado la vida" || color != "#ff7f7f" {
		t.Errorf("first tier %q %q", first, color)
	}
	// The conversion records the year, so renaming a ranking does not lose the link with Year in review.
	var years string
	d.QueryRow(`SELECT group_concat(year) FROM (SELECT year FROM ranking ORDER BY pos)`).Scan(&years)
	if years != "2025,2024,2023" {
		t.Errorf("years = %q", years)
	}
	// The old tables are untouched.
	var old int
	d.QueryRow(`SELECT count(*) FROM tierlist_entry`).Scan(&old)
	if old != 4 {
		t.Errorf("old entries = %d", old)
	}
	// Cascades: a deleted game leaves every ranking; a deleted tier sends its games to the pool.
	d.Exec(`DELETE FROM game WHERE id = 3`)
	var n int
	d.QueryRow(`SELECT count(*) FROM ranking_entry WHERE game_id = 3`).Scan(&n)
	if n != 0 {
		t.Errorf("entries of a deleted game = %d", n)
	}
	d.Exec(`DELETE FROM ranking_tier WHERE label = 'Me ha cambiado la vida'`)
	if got := placement("Tier list 2024"); got != "2:- 1:-" {
		t.Errorf("after deleting the tier: %q", got)
	}
}
