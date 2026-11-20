package main

// seedFakeIGDB fills the caches that normally need IGDB (the games of the series' collections that the library lacks,
// and the similar games) with made-up games, so the Series, Upcoming and Similar pages have something to filter without
// any key or network. It is only for the browser smoke test (docs/tools/smoke.mjs passes -fake-igdb); the screenshots
// read the real IGDB instead. The IGDB ids are made up, so Add/Fill from IGDB never matches anything real.

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

func seedFakeIGDB(d *sql.DB) {
	must := func(_ sql.Result, err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05Z")
	day := func(months, days int) int64 { return now.AddDate(0, months, days).Unix() }
	// every game gets a made-up IGDB id, so the finished and favourite ones can be sources
	must(d.Exec(`UPDATE game SET igdb_id = 900000 + id WHERE igdb_id IS NULL`))
	must(d.Exec(`UPDATE game SET igdb_parent_id = 0 WHERE igdb_parent_id IS NULL`))

	// 1. the series with at least two games: a collection each, with a few games the library lacks
	rows, err := d.Query(`SELECT s.name, group_concat(g.id) FROM game g JOIN series s ON s.id = g.series_id GROUP BY s.id HAVING count(*) >= 2 ORDER BY s.name`)
	if err != nil {
		log.Fatal(err)
	}
	type ser struct{ name, ids string }
	var all []ser
	for rows.Next() {
		var s ser
		if err := rows.Scan(&s.name, &s.ids); err != nil {
			log.Fatal(err)
		}
		all = append(all, s)
	}
	rows.Close()
	plats := []string{"PC", "PC, PS5", "Switch", "PC, Xbox Series X|S", "PS5", ""}
	for i, s := range all {
		cid := int64(7000 + i)
		must(d.Exec(`INSERT INTO igdb_collection(id, name, fetched_at) VALUES (?,?,?)`, cid, s.name, stamp))
		must(d.Exec(`INSERT INTO game_collection(game_id, collection_id, name) SELECT g.id, ?, ? FROM game g JOIN series x ON x.id = g.series_id WHERE x.name = ?`, cid, s.name, s.name))
		// (title, release date, date format, release year)
		type fake struct {
			title  string
			date   int64
			format int
			year   int
		}
		list := []fake{
			{s.name + ": Origins", day(-96, 0), 0, now.Year() - 8},
			{s.name + " Reborn", day(0, 3+i%20), 0, now.Year()},
			{s.name + ": Next Light", day(2, i%25), 1, now.Year()},
			{s.name + " Legacy", day(9, 0), 2, now.Year() + 1},
			{s.name + " Unbound", 0, 0, 0},
		}
		if i%3 == 0 {
			list = append(list, fake{s.name + ": Afterglow", day(-1, -10), 0, now.Year()})
		}
		for j, f := range list {
			var date, format, year any
			if f.date != 0 {
				date, format = f.date, f.format
			}
			if f.year != 0 {
				year = f.year
			}
			must(d.Exec(`INSERT INTO igdb_collection_game(collection_id, igdb_id, title, release_year, game_type, parent_igdb_id, first_release_date, release_date_format, platforms)
				VALUES (?,?,?,?,0,0,?,?,?)`, cid, 800000+int64(i)*10+int64(j), f.title, year, date, format, plats[(i+j)%len(plats)]))
		}
	}
	must(d.Exec(`INSERT INTO series_hidden(collection_id, igdb_id) SELECT 7000, 800000 + 4 WHERE EXISTS (SELECT 1 FROM igdb_collection_game WHERE collection_id = 7000 AND igdb_id = 800004)`))

	// 2. similar games of the finished and favourite games
	var sources []int64
	srows, err := d.Query(`SELECT g.igdb_id FROM game g JOIN status s ON s.id = g.status_id WHERE s.code = 'finished' OR g.favorite = 1 ORDER BY g.id`)
	if err != nil {
		log.Fatal(err)
	}
	for srows.Next() {
		var id int64
		if err := srows.Scan(&id); err != nil {
			log.Fatal(err)
		}
		sources = append(sources, id)
	}
	srows.Close()
	genres := []string{"Role-playing (RPG)", "Adventure", "Shooter", "Platform", "Strategy", "Puzzle", "Indie", "Simulator"}
	simPlats := []string{"PC", "PC, PS5", "Switch", "PC, Switch", "PS5, Xbox Series X|S", "PC, Xbox Series X|S"}
	names := []string{"Ash Kingdom", "Blue Hollow", "Crimson Tide", "Dust Runner", "Ember Gate", "Frost Signal", "Glass Harbor", "Hollow Peak", "Iron Orchard", "Jade Circuit",
		"Kite Song", "Lantern Hill", "Moss Engine", "Night Ferry", "Opal Drift", "Paper Moon", "Quiet Forge", "Rust Garden", "Silver Tower", "Tin Savannah",
		"Umber Reach", "Velvet Gear", "Wild Orbit", "Yarrow Deep"}
	if len(sources) > 0 {
		for i, n := range names {
			id := int64(850000 + i)
			g1, g2 := genres[i%len(genres)], genres[(i*3+1)%len(genres)]
			gen := g1
			if g2 != g1 {
				gen += ", " + g2
			}
			must(d.Exec(`INSERT INTO similar_game(igdb_id, title, release_year, game_type, parent_igdb_id, platforms, genres) VALUES (?,?,?,0,0,?,?)`,
				id, n, 2008+(i*5)%17, simPlats[i%len(simPlats)], gen))
			for k := 0; k <= i%3; k++ { // one to three sources
				must(d.Exec(`INSERT OR IGNORE INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (?,?)`, sources[(i+k*2)%len(sources)], id))
			}
		}
		for _, s := range sources {
			must(d.Exec(`INSERT OR IGNORE INTO similar_fetch(source_igdb_id, fetched_at) VALUES (?,?)`, s, stamp))
		}
		must(d.Exec(`INSERT INTO similar_hidden(igdb_id) VALUES (850003)`))
	}
	fmt.Println("made-up IGDB caches written (series collections and similar games)")
}

// seedExpansions gives the demo library IGDB-style expansions (tracked and untracked, owned and not, released and upcoming), so the
// drawer, the Expansions page, Upcoming, Stats and the screenshots have data without any key. Every game is marked as read, so no
// page asks a real IGDB for them. Made-up IGDB ids (960000 and up).
func seedExpansions(d *sql.DB) {
	must := func(_ sql.Result, err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05Z")
	must(d.Exec(`INSERT OR IGNORE INTO ownership(name) VALUES ('Owned')`))
	must(d.Exec(`INSERT INTO expansion_fetch(game_id, fetched_at) SELECT id, ? FROM game`, stamp)) // every game counts as read, so no drawer asks a real IGDB for them
	erows, err := d.Query(`SELECT g.id, g.title, g.release_year, s.code FROM game g JOIN status s ON s.id = g.status_id ORDER BY (s.code = 'finished') DESC, g.id LIMIT 18`)
	if err != nil {
		log.Fatal(err)
	}
	type base struct {
		id    int64
		title string
		year  sql.NullInt64
		code  string
	}
	var bases []base
	for erows.Next() {
		var b base
		if err := erows.Scan(&b.id, &b.title, &b.year, &b.code); err != nil {
			log.Fatal(err)
		}
		bases = append(bases, b)
	}
	erows.Close()
	suffixes := []string{"The Lost Chapter", "Frozen Shores", "Night of the Hollow", "Reckoning"}
	statuses := []string{"", "finished", "playing", "backlog", "", "dropped"}
	for i, b := range bases {
		for k := 0; k <= i%3; k++ {
			year := now.Year() - 1 - (i+k)%6
			var date, ry any
			switch {
			case i == 2 && k == 0: // coming up
				date, ry = now.AddDate(0, 2, 5).Format("2006-01-02"), now.Year()
			case i == 5 && k == 0: // coming up later
				date, ry = now.AddDate(1, 0, 0).Format("2006-01-02"), now.Year()+1
			case i == 7 && k == 0: // announced, no date
			default:
				date, ry = fmt.Sprintf("%d-%02d-%02d", year, 1+(i*3+k)%12, 1+(i+k*7)%27), year
			}
			title := b.title + ": " + suffixes[(i+k)%len(suffixes)]
			var status, own any
			if st := statuses[(i+k*2)%len(statuses)]; st != "" {
				status = st
			}
			if (i+k)%2 == 0 {
				own = "Owned"
			}
			if b.code == "finished" && k == 0 { // a finished game with an owned expansion not played yet: Needs attention lists it
				status, own = nil, "Owned"
			}
			res, err := d.Exec(`INSERT INTO expansion(game_id, igdb_id, title, release_date, release_year, steam_app_id, status_id, ownership_id)
				VALUES (?,?,?,?,?,?, (SELECT id FROM status WHERE code = ?), (SELECT id FROM ownership WHERE name = ?))`,
				b.id, 960000+int64(i)*10+int64(k), title, date, ry, 1500000+int64(i)*10+int64(k), status, own)
			if err != nil {
				log.Fatal(err)
			}
			if status == "finished" || status == "playing" {
				xid, _ := res.LastInsertId()
				must(d.Exec(`INSERT INTO expansion_play(expansion_id, year) VALUES (?, ?)`, xid, now.Year()-(i+k)%3))
			}
		}
	}
	// IGDB's DLC (game_type 1) of a few games: none added, some owned on Steam (the drawer offers those first)
	dlcs := []string{"Hearthstone Hamlet", "Vampire Hunters", "Costume Pack"}
	for i, b := range bases {
		if i >= 6 {
			break
		}
		must(d.Exec(`INSERT INTO expansion(game_id, igdb_id, title, release_date, release_year, steam_app_id, kind, steam_owned) VALUES (?,?,?,?,?,?, 'dlc', ?)`,
			b.id, 969000+int64(i), b.title+": "+dlcs[i%len(dlcs)], fmt.Sprintf("%d-06-01", now.Year()-2), now.Year()-2, 1590000+int64(i), (i+1)%2))
	}
	fmt.Println("made-up expansions written")
}
