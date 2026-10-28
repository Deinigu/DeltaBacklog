package db

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestRemoveWishlistSteamMigration: a populated library at version 17, with Steam rows and hours and a wishlist game,
// loses the Steam tables and columns and the Wishlist status; every game, play, view, ranking, series cache and deal row
// survives, the wishlist game is Backlog and the statuses after it get their old sort numbers back.
func TestRemoveWishlistSteamMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", FileURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	var names []string
	for _, e := range entries {
		if n, _ := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0]); n <= 17 {
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
		`PRAGMA user_version = 17`,
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id, playtime_min, steam_last_played, poster_file, first_release_date, release_date_format) VALUES (1,'Hades','hades',(SELECT id FROM status WHERE code='finished'),100,600,1700000000,'p1.jpg',1600000000,0)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2,'Wished','wished',(SELECT id FROM status WHERE code='wishlist'))`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (3,'Held','held',(SELECT id FROM status WHERE code='hold'))`,
		`INSERT INTO play(game_id, year) VALUES (1, 2024)`,
		`INSERT INTO play(game_id, year) VALUES (3, 2023)`,
		`INSERT INTO ranking(id, name) VALUES (1, 'Best')`,
		`INSERT INTO ranking_tier(id, ranking_id, label, color, pos) VALUES (1, 1, 'S', '#fff', 1)`,
		`INSERT INTO ranking_entry(ranking_id, game_id, tier_id, pos) VALUES (1, 1, 1, 1)`,
		`INSERT INTO similar_game(igdb_id, title, game_type) VALUES (500, 'Similar', 0)`,
		`INSERT INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (100, 500)`,
		`INSERT INTO deal_price(game_id, currency, fetched_at) VALUES (1, 'EUR', '2026-01-01T00:00:00Z')`,
		`INSERT INTO setting(key, value) VALUES ('steam_synced_at', '2026-01-01T00:00:00Z')`,
		`INSERT INTO setting(key, value) VALUES ('deals_country', 'ES')`,
		`INSERT INTO steam_app(app_id, name, playtime_min, game_id, source, seen_at) VALUES (1, 'Hades', 600, 1, 'link', '2026-01-01T00:00:00Z')`,
		`INSERT INTO steam_app(app_id, name, seen_at) VALUES (2, 'Unmatched', '2026-01-01T00:00:00Z')`,
		`INSERT INTO deal_match(game_id, itad_id, source) VALUES (1, 'abc', 'title')`,
		`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (7, 'Zed Series', '2026-01-01T00:00:00Z')`,
		`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, game_type, first_release_date, release_date_format) VALUES (7, 101, 'Hades II', 0, 1900000000, 0)`,
		`INSERT INTO saved_view(name, icon, filter_json, sort_json, pinned, pos) VALUES ('Wanted', 'x', '{"in":{"status":["wishlist","backlog"]},"ex":{"status":["wishlist"]}}', '[{"field":"playtime","dir":"desc"},{"field":"title","dir":"asc"}]', 0, 90)`,
		`INSERT INTO saved_view(name, icon, filter_json, sort_json, pinned, pos) VALUES ('Only', 'x', '{"in":{"status":["wishlist"]},"flags":["vr"]}', '[{"field":"playtime","dir":"asc"}]', 0, 91)`,
		`INSERT INTO saved_view(name, icon, filter_json, sort_json, pinned, pos) VALUES ('Search', 'x', '{"q":"wishlist","in":{"genre":["wishlist"]}}', '[{"field":"title","dir":"asc"}]', 0, 92)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	var views, rankings int
	old.QueryRow(`SELECT count(*) FROM saved_view`).Scan(&views)
	old.QueryRow(`SELECT count(*) FROM ranking`).Scan(&rankings)
	old.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	count := func(q string) (n int) {
		d.QueryRow(q).Scan(&n)
		return
	}
	if v := count(`PRAGMA user_version`); v != 18 {
		t.Fatalf("version %d", v)
	}
	if n := count(`SELECT count(*) FROM game`); n != 3 {
		t.Errorf("games %d", n)
	}
	if n := count(`SELECT count(*) FROM play`); n != 2 {
		t.Errorf("plays %d", n)
	}
	if n := count(`SELECT count(*) FROM saved_view`); n != views {
		t.Errorf("views %d, want %d", n, views)
	}
	if n := count(`SELECT count(*) FROM ranking`); n != rankings {
		t.Errorf("rankings %d, want %d", n, rankings)
	}
	for table, want := range map[string]int{"igdb_collection": 1, "igdb_collection_game": 1, "similar_game": 1, "similar_link": 1,
		"deal_match": 1, "deal_price": 1, "ranking": 1, "ranking_tier": 1, "ranking_entry": 1} {
		if n := count(`SELECT count(*) FROM ` + table); n != want {
			t.Errorf("%s: %d rows, want %d", table, n, want)
		}
	}
	if n := count(`SELECT count(*) FROM game WHERE id = 1 AND poster_file = 'p1.jpg' AND first_release_date = 1600000000 AND release_date_format = 0`); n != 1 {
		t.Error("poster or release date of game 1 changed")
	}
	if n := count(`SELECT count(*) FROM igdb_collection_game WHERE first_release_date = 1900000000`); n != 1 {
		t.Error("collection game release date changed")
	}
	if n := count(`SELECT count(*) FROM setting WHERE key = 'steam_synced_at'`); n != 0 {
		t.Error("steam_synced_at left")
	}
	if n := count(`SELECT count(*) FROM setting WHERE key = 'deals_country'`); n != 1 {
		t.Error("deals_country lost")
	}
	if n := count(`SELECT count(*) FROM sqlite_master WHERE name LIKE '%steam%'`); n != 0 {
		t.Errorf("%d steam objects left", n)
	}
	if n := count(`SELECT count(*) FROM pragma_table_info('game') WHERE name IN ('playtime_min', 'steam_last_played')`); n != 0 {
		t.Errorf("%d hours columns left", n)
	}
	if n := count(`SELECT count(*) FROM status WHERE code = 'wishlist'`); n != 0 {
		t.Errorf("wishlist status left")
	}
	var status string
	d.QueryRow(`SELECT s.code FROM game g JOIN status s ON s.id = g.status_id WHERE g.id = 2`).Scan(&status)
	if status != "backlog" {
		t.Errorf("the wishlist game is %q", status)
	}
	rows, _ := d.Query(`SELECT code, sort FROM status ORDER BY sort`)
	var order []string
	for rows.Next() {
		var c string
		var s int
		rows.Scan(&c, &s)
		order = append(order, c+"="+strconv.Itoa(s))
	}
	rows.Close()
	if got := strings.Join(order, " "); got != "playing=1 playing_evergreen=2 up_next=3 backlog=4 hold=5 evergreen=6 finished=7 dropped=8 not_interested=9" {
		t.Errorf("status order: %s", got)
	}
	var wanted, wantedSort, only, onlySort, search, searchSort string
	d.QueryRow(`SELECT filter_json, sort_json FROM saved_view WHERE name = 'Wanted'`).Scan(&wanted, &wantedSort)
	d.QueryRow(`SELECT filter_json, sort_json FROM saved_view WHERE name = 'Only'`).Scan(&only, &onlySort)
	d.QueryRow(`SELECT filter_json, sort_json FROM saved_view WHERE name = 'Search'`).Scan(&search, &searchSort)
	if wanted != `{"in":{"status":["backlog"]},"ex":{"status":[]}}` || wantedSort != `[{"field":"title","dir":"asc"}]` {
		t.Errorf("view Wanted: %s | %s", wanted, wantedSort)
	}
	if only != `{"in":{"status":[]},"flags":["vr"]}` || onlySort != `[]` {
		t.Errorf("view Only: %s | %s", only, onlySort)
	}
	if search != `{"q":"wishlist","in":{"genre":["wishlist"]}}` || searchSort != `[{"field":"title","dir":"asc"}]` {
		t.Errorf("a search or genre named wishlist was touched: %s | %s", search, searchSort)
	}
	if n := count(`SELECT count(*) FROM saved_view WHERE NOT json_valid(filter_json) OR NOT json_valid(sort_json)`); n != 0 {
		t.Errorf("%d views with invalid JSON", n)
	}
	if n := count(`SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("%d foreign key violations", n)
	}
	var integrity string
	d.QueryRow(`PRAGMA integrity_check`).Scan(&integrity)
	if integrity != "ok" {
		t.Errorf("integrity %s", integrity)
	}
}
