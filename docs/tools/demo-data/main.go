// Command demo-data builds a small demo library (about 40 well-known games, no personal data), for screenshots
// and for trying the app out.
//
//	go run ./docs/tools/demo-data -data /tmp/demo
//	LISTEN_ADDR=127.0.0.1:18100 DATA_DIR=/tmp/demo go run ./cmd/deltabacklog
//
// Every game gets a generated placeholder cover (a gradient with the title), so it works offline and the
// repository carries no artwork. When IGDB / SteamGridDB keys are available (in the environment, or in the file
// given with -env) the real covers are downloaded into the data directory through the same lookups the app uses,
// for illustration in screenshots; they stay in that throwaway directory and are never committed.
// It refuses to write into a data directory that already has a database.
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type game struct {
	title, status, ownership, series, dev string
	year                                  int
	platforms, genres, themes             string // comma separated
	plays                                 string // "2021,2023", "childhood", "always"
	vr, fav, completed                    bool
	notes                                 string
}

// Order matters: it is the manual queue order.
var games = []game{
	{"Outer Wilds", "playing", "Owned", "", "Mobius Digital", 2019, "PC, PS4", "Adventure, Puzzle", "Open world, Science fiction", "2025,2026", false, true, false, "Do not read anything about it first."},
	{"Hades", "playing", "Owned", "", "Supergiant Games", 2020, "PC, Switch", "Roguelike, Hack and slash", "Fantasy", "2024,2025", false, true, false, ""},
	{"Stardew Valley", "playing_evergreen", "Owned", "", "ConcernedApe", 2016, "PC, Switch", "Simulator, RPG", "Farming, Fantasy", "2019,2021,2024", false, true, false, "Cozy autumn game."},
	{"Hollow Knight", "up_next", "Owned", "", "Team Cherry", 2017, "PC, Switch", "Metroidvania, Platform", "Fantasy, Dark", "", false, false, false, ""},
	{"Disco Elysium", "up_next", "Owned", "", "ZA/UM", 2019, "PC", "RPG, Adventure", "Mystery, Drama", "", false, false, false, ""},
	{"Celeste", "up_next", "Owned", "", "Maddy Makes Games", 2018, "PC, Switch", "Platform, Indie", "Drama", "", false, false, false, ""},
	{"Return of the Obra Dinn", "up_next", "Owned", "", "Lucas Pope", 2018, "PC", "Puzzle, Adventure", "Mystery, Historical", "", false, false, false, ""},
	{"Baldur's Gate 3", "backlog", "Owned", "Baldur's Gate", "Larian Studios", 2023, "PC", "RPG, Strategy", "Fantasy", "", false, false, false, "Wait for a quiet month."},
	{"Elden Ring", "backlog", "Owned", "Souls", "FromSoftware", 2022, "PC, PS5", "RPG, Hack and slash", "Fantasy, Open world", "", false, false, false, ""},
	{"Cyberpunk 2077", "backlog", "Owned", "", "CD Projekt Red", 2020, "PC", "RPG, Shooter", "Science fiction, Open world", "", false, false, false, ""},
	{"Death Stranding", "backlog", "Free to play", "", "Kojima Productions", 2019, "PC, PS4", "Adventure", "Science fiction, Open world", "", false, false, false, ""},
	{"Subnautica", "backlog", "Owned", "Subnautica", "Unknown Worlds", 2018, "PC", "Adventure, Simulator", "Survival, Open world", "", false, false, false, ""},
	{"Pentiment", "backlog", "Family share", "", "Obsidian Entertainment", 2022, "PC", "Adventure, RPG", "Historical, Mystery", "", false, false, false, ""},
	{"Slay the Spire", "evergreen", "Owned", "", "Mega Crit", 2019, "PC, Switch", "Card game, Roguelike", "Fantasy", "2020,2022,2024", false, false, false, ""},
	{"Rocket League", "evergreen", "Free to play", "", "Psyonix", 2015, "PC", "Sport, Racing", "Sports", "2017,2018,2019", false, false, false, ""},
	{"Tetris Effect", "evergreen", "Owned", "", "Monstars", 2018, "PS4, PC", "Puzzle", "Abstract", "always", false, false, false, ""},
	{"Super Mario Odyssey", "finished", "Owned", "Super Mario", "Nintendo EPD", 2017, "Switch", "Platform, Adventure", "Fantasy", "2018", false, true, true, ""},
	{"The Legend of Zelda: Breath of the Wild", "finished", "Owned", "The Legend of Zelda", "Nintendo EPD", 2017, "Switch", "Adventure, RPG", "Fantasy, Open world", "2019,2020", false, true, false, ""},
	{"The Legend of Zelda: Ocarina of Time", "finished", "Borrowed", "The Legend of Zelda", "Nintendo EAD", 1998, "N64", "Adventure", "Fantasy", "childhood", false, true, false, ""},
	{"Portal 2", "finished", "Owned", "Portal", "Valve", 2011, "PC", "Puzzle, Platform", "Science fiction", "2012,2022", false, true, false, ""},
	{"Portal", "finished", "Owned", "Portal", "Valve", 2007, "PC", "Puzzle, Platform", "Science fiction", "2010", false, false, false, ""},
	{"Half-Life: Alyx", "finished", "Owned", "Half-Life", "Valve", 2020, "PC", "Shooter, Adventure", "Science fiction", "2021", true, false, false, ""},
	{"Beat Saber", "evergreen", "Owned", "", "Beat Games", 2019, "PC, Quest", "Rhythm, Music", "Music", "2021,2022", true, false, false, ""},
	{"Phoenix Wright: Ace Attorney", "finished", "Owned", "Ace Attorney", "Capcom", 2001, "DS, Switch", "Adventure, Visual novel", "Mystery, Legal", "2022", false, false, false, ""},
	{"Phoenix Wright: Ace Attorney - Justice for All", "finished", "Owned", "Ace Attorney", "Capcom", 2002, "DS, Switch", "Adventure, Visual novel", "Mystery, Legal", "2022", false, false, false, ""},
	{"Phoenix Wright: Ace Attorney - Trials and Tribulations", "playing", "Owned", "Ace Attorney", "Capcom", 2004, "DS, Switch", "Adventure, Visual novel", "Mystery, Legal", "2026", false, false, false, ""},
	{"Apollo Justice: Ace Attorney", "backlog", "Owned", "Ace Attorney", "Capcom", 2007, "DS, Switch", "Adventure, Visual novel", "Mystery, Legal", "", false, false, false, ""},
	{"Dark Souls", "finished", "Owned", "Souls", "FromSoftware", 2011, "PC", "RPG, Hack and slash", "Fantasy, Dark", "2016,2017", false, false, true, ""},
	{"Dark Souls III", "dropped", "Owned", "Souls", "FromSoftware", 2016, "PC", "RPG, Hack and slash", "Fantasy, Dark", "2018", false, false, false, "Stuck at the Dancer."},
	{"Bloodborne", "hold", "Owned", "Souls", "FromSoftware", 2015, "PS4", "RPG, Hack and slash", "Horror, Dark", "2021", false, false, false, ""},
	{"The Witcher 3: Wild Hunt", "finished", "Owned", "The Witcher", "CD Projekt Red", 2015, "PC", "RPG", "Fantasy, Open world", "2017,2023", false, true, false, ""},
	{"Red Dead Redemption 2", "hold", "Owned", "Red Dead", "Rockstar Games", 2018, "PC", "Adventure, Shooter", "Western, Open world", "2020", false, false, false, ""},
	{"Minecraft", "evergreen", "Owned", "", "Mojang Studios", 2011, "PC, Switch", "Sandbox, Survival", "Open world", "childhood,2013,2020", false, false, false, "Played with the family."},
	{"Among Us", "evergreen", "Free to play", "", "Innersloth", 2018, "PC, Android", "Strategy, Party", "Social deduction", "2020", false, false, false, ""},
	{"Fall Guys", "dropped", "Free to play", "", "Mediatonic", 2020, "PC", "Platform, Party", "Comedy", "2020", false, false, false, ""},
	{"Civilization VI", "hold", "Owned", "Civilization", "Firaxis Games", 2016, "PC, Switch", "Strategy, 4X", "Historical", "2018,2019", false, false, false, ""},
	{"Fortnite", "not_interested", "Free to play", "", "Epic Games", 2017, "PC", "Shooter, Battle royale", "Survival", "", false, false, false, ""},
	{"Pokémon Red", "finished", "Borrowed", "Pokémon", "Game Freak", 1996, "Game Boy", "RPG", "Fantasy", "childhood", false, true, false, ""},
	{"Pokémon Legends: Arceus", "dropped", "Owned", "Pokémon", "Game Freak", 2022, "Switch", "RPG", "Fantasy, Open world", "2022", false, false, false, ""},
	{"Undertale", "finished", "Owned", "", "Toby Fox", 2015, "PC, Switch", "RPG, Indie", "Comedy, Fantasy", "2016", false, true, true, ""},
	// The wishlist (see due): games the owner wants but does not have. Not linked to IGDB, so their release dates stay
	// the made-up ones below, relative to the day the demo is built.
	{"Phantom Blade Zero", "wishlist", "", "", "S-GAME", 2026, "PC, PS5", "Hack and slash, RPG", "Fantasy", "", false, false, false, "Day-one buy."},
	{"Professor Layton and the New World of Steam", "wishlist", "", "Professor Layton", "Level-5", 2026, "Switch", "Puzzle, Adventure", "Mystery", "", false, false, false, ""},
	{"Marvel's Wolverine", "wishlist", "", "", "Insomniac Games", 2027, "PS5", "Adventure, Hack and slash", "Action", "", false, false, false, ""},
	{"Slay the Spire II", "wishlist", "", "", "Mega Crit", 2026, "PC", "Card game, Roguelike", "Fantasy", "", false, false, false, "Out already: time to move it to the backlog."},
	{"The Elder Scrolls VI", "wishlist", "", "", "Bethesda Game Studios", 0, "PC", "RPG", "Fantasy, Open world", "", false, false, false, ""},
}

// due is the made-up IGDB release date of a wishlist game, relative to today: an exact day (n days ahead), a month, a
// quarter or a year, as precise as IGDB would give it. A negative n is a release in the past.
var due = map[string]struct {
	kind string
	n    int
}{
	"Phantom Blade Zero": {"day", 9}, "Professor Layton and the New World of Steam": {"month", 1},
	"Marvel's Wolverine": {"quarter", 5}, "Slay the Spire II": {"day", -18},
}

// dueDate turns a due entry into IGDB's shape: unix seconds (UTC) and date_format (0 day, 1 month, 2 year, 3-6 quarter).
func dueDate(now time.Time, kind string, n int) (int64, int) {
	t := now.UTC()
	switch kind {
	case "month":
		return time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC).Unix(), 1
	case "quarter":
		x := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
		q := (int(x.Month()) - 1) / 3
		return time.Date(x.Year(), time.Month(q*3+4), 0, 0, 0, 0, 0, time.UTC).Unix(), 3 + q
	case "year":
		return time.Date(t.Year()+n, 12, 31, 0, 0, 0, 0, time.UTC).Unix(), 2
	}
	return time.Date(t.Year(), t.Month(), t.Day()+n, 0, 0, 0, 0, time.UTC).Unix(), 0
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	dataDir := flag.String("data", "./demo-data", "data directory to create (app.db and covers/)")
	envFile := flag.String("env", "", "a .env file with IGDB_CLIENT_ID, IGDB_CLIENT_SECRET and STEAMGRIDDB_API_KEY, to download real covers")
	flag.Parse()
	if *envFile != "" {
		loadEnv(*envFile)
	}
	dbPath := filepath.Join(*dataDir, "app.db")
	if _, err := os.Stat(dbPath); err == nil {
		log.Fatalf("%s already exists; refusing to touch it (choose an empty directory)", dbPath)
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	tx, err := d.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	ids := make([]int64, len(games))
	for i, g := range games {
		var statusID int64
		must(tx.QueryRow("SELECT id FROM status WHERE code = ?", g.status).Scan(&statusID))
		var own, series, dev any
		if g.ownership != "" {
			own, err = db.GetOrCreate(tx, "ownership", g.ownership)
			must(err)
		}
		if g.series != "" {
			series, err = db.GetOrCreate(tx, "series", g.series)
			must(err)
		}
		if g.dev != "" {
			dev, err = db.GetOrCreate(tx, "company", g.dev)
			must(err)
		}
		res, err := tx.Exec(`INSERT INTO game (title, title_sort, status_id, ownership_id, series_id, developer_id, release_year,
			vr, favorite, completed, notes, queue_pos, added_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?, strftime('%Y-%m-%dT%H:%M:%SZ','now','-'||?||' days'), strftime('%Y-%m-%dT%H:%M:%SZ','now'))`,
			g.title, db.TitleSort(g.title), statusID, own, series, dev, nullYear(g.year), b2i(g.vr), b2i(g.fav), b2i(g.completed), g.notes, i+1, 400-i*8)
		must(err)
		id, _ := res.LastInsertId()
		ids[i] = id
		if w, ok := due[g.title]; ok {
			date, format := dueDate(time.Now(), w.kind, w.n)
			_, err = tx.Exec("UPDATE game SET first_release_date = ?, release_date_format = ? WHERE id = ?", date, format, id)
			must(err)
		}
		for _, l := range []struct{ table, junction, fk, list string }{
			{"platform", "game_platform", "platform_id", g.platforms},
			{"genre", "game_genre", "genre_id", g.genres},
			{"theme", "game_theme", "theme_id", g.themes},
		} {
			for _, name := range split(l.list) {
				if l.table == "theme" {
					name = db.NormalizeTheme(name)
				}
				nid, err := db.GetOrCreate(tx, l.table, name)
				must(err)
				_, err = tx.Exec("INSERT OR IGNORE INTO "+l.junction+"(game_id, "+l.fk+") VALUES (?,?)", id, nid)
				must(err)
			}
		}
		for _, p := range split(g.plays) {
			switch p {
			case "childhood", "always":
				_, err = tx.Exec("INSERT INTO play(game_id, period) VALUES (?,?)", id, p)
			default:
				var y int
				fmt.Sscanf(p, "%d", &y)
				_, err = tx.Exec("INSERT INTO play(game_id, year) VALUES (?,?)", id, y)
			}
			must(err)
		}
		must(db.IndexGame(tx, id))
	}
	// Times to beat (minutes) for some games, from the three possible sources, so the drawer's block, the Short game
	// filter and the backlog hours have something to show. The numbers are rounded, made up for the demo.
	for _, t := range []struct {
		game    string
		h, n, c int
		source  string
	}{
		{"Hollow Knight", 1620, 2500, 3940, "hltb"},
		{"Celeste", 480, 600, 1500, "hltb"},
		{"Return of the Obra Dinn", 480, 600, 720, "igdb"},
		{"Disco Elysium", 1800, 2400, 3000, "igdb"},
		{"Baldur's Gate 3", 4200, 5100, 7500, "igdb"},
		{"Elden Ring", 3600, 5700, 8100, "igdb"},
		{"Portal", 180, 320, 630, "hltb"},
		{"Outer Wilds", 900, 1100, 1500, "manual"},
		{"Slay the Spire", 0, 1200, 0, "manual"},
		{"Pentiment", 960, 1200, 0, "igdb"},
	} {
		_, err := tx.Exec(`UPDATE game SET time_to_beat_hastily = nullif(?, 0), time_to_beat_normally = nullif(?, 0),
			time_to_beat_completely = nullif(?, 0), time_to_beat_src = ? WHERE title = ?`, t.h, t.n, t.c, t.source, t.game)
		must(err)
	}
	// Two games waiting in the "Link IGDB" review screen (made-up ids), so that screen has something to show.
	for _, c := range []struct {
		game     string
		igdb     int
		name     string
		year     int
		score    int
		platform string
	}{
		{"Pentiment", 900001, "Pentiment", 2022, 82, "PC, Xbox Series X|S, PlayStation 5"},
		{"Subnautica", 900002, "Subnautica: Below Zero", 2021, 64, "PC, Nintendo Switch"},
	} {
		_, err := tx.Exec(`INSERT INTO sync_candidate(game_id, igdb_id, name, year, score, platforms, slug)
			SELECT id, ?, ?, ?, ?, ?, ? FROM game WHERE title = ?`, c.igdb, c.name, c.year, c.score, c.platform,
			strings.ToLower(strings.ReplaceAll(c.name, " ", "-")), c.game)
		must(err)
		// The review queue lists games the sync has already searched.
		_, err = tx.Exec("UPDATE game SET igdb_checked_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE title = ?", c.game)
		must(err)
	}
	must(tx.Commit())

	for i, g := range games {
		name, err := covers.Save(bytes.NewReader(cover(g.title)), filepath.Join(*dataDir, "covers"), ids[i])
		must(err)
		_, err = d.Exec("UPDATE game SET cover_file = ?, cover_updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?", name, ids[i])
		must(err)
	}
	real := 0
	mc := meta.New(meta.Config{
		IGDBClientID:     os.Getenv("IGDB_CLIENT_ID"),
		IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET"),
		SteamGridDBKey:   os.Getenv("STEAMGRIDDB_API_KEY"),
	})
	if igdb, sgdb := mc.Configured(); igdb || sgdb {
		for i, g := range games {
			url := realCover(mc, g.title, igdb, sgdb)
			if url == "" {
				log.Printf("no real cover found for %s; keeping the placeholder", g.title)
				continue
			}
			// The same download (size guard, resize to JPEG) as the app's own cover change.
			if _, err := covers.FetchStrict(url, filepath.Join(*dataDir, "covers"), ids[i]); err != nil {
				log.Printf("cover for %s: %v; keeping the placeholder", g.title, err)
				continue
			}
			real++
		}
	}
	// With IGDB keys, link the games whose title IGDB knows exactly (the two games of the review queue above stay
	// unlinked). The rankings' posters then come from the right IGDB cover instead of a fuzzy SteamGridDB match
	// ("Baldur's Gate 3" found "Baldur's Gate Toolkit").
	igdbName := map[string]string{"Baldur's Gate 3": "Baldur's Gate III"} // IGDB's own spelling
	if igdb, _ := mc.Configured(); igdb {
		for i, g := range games {
			if _, wish := due[g.title]; wish || g.status == "wishlist" || g.title == "Pentiment" || g.title == "Subnautica" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cands, err := mc.SearchIGDB(ctx, g.title)
			cancel()
			if err != nil || len(cands) == 0 {
				continue
			}
			same := func(a, b string) bool { // IGDB writes the typographic apostrophe
				return strings.EqualFold(strings.ReplaceAll(a, "’", "'"), strings.ReplaceAll(b, "’", "'"))
			}
			for _, c := range meta.RankByTitle(cands, g.title) { // exact title AND release year: IGDB has several "Hades"
				if (same(c.Name, g.title) || same(c.Name, igdbName[g.title])) && c.Year == g.year {
					d.Exec("UPDATE game SET igdb_id = ? WHERE id = ? AND igdb_id IS NULL", c.IGDBID, ids[i]) // an id used twice is skipped
					break
				}
			}
		}
	}
	fmt.Printf("demo library with %d games written to %s (%d real covers, %d placeholders)\n", len(games), *dataDir, real, len(games)-real)
}

// realCover returns the URL of a cover for the title: a wide SteamGridDB grid (the shape the library's cards use)
// when possible, otherwise an IGDB picture. Empty when nothing is found.
func realCover(mc *meta.Client, title string, igdb, sgdb bool) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if sgdb {
		if opts, err := mc.SearchCovers(ctx, title); err == nil && len(opts) > 0 {
			return opts[0].URL
		}
	}
	if igdb {
		if opts, err := mc.IGDBImages(ctx, 0, title); err == nil && len(opts) > 0 {
			return opts[0].URL
		}
	}
	return ""
}

// loadEnv reads KEY=VALUE lines into the environment (variables that are already set win).
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if ok && k != "" && os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cover draws a 300x450 gradient from two colours derived from the title, with the title in large letters
// (drawn small with a bitmap font, then enlarged 2x, centred, because cards crop covers to a wide strip).
func cover(title string) []byte {
	h := fnv.New32a()
	h.Write([]byte(title))
	s := h.Sum32()
	c1 := color.RGBA{uint8(40 + s&0x7f), uint8(40 + (s>>8)&0x7f), uint8(60 + (s>>16)&0x7f), 255}
	c2 := color.RGBA{uint8(20 + (s>>24)&0x5f), uint8(30 + (s>>4)&0x6f), uint8(40 + (s>>12)&0x7f), 255}
	const w, hgt = 300, 450
	img := image.NewRGBA(image.Rect(0, 0, w, hgt))
	for y := 0; y < hgt; y++ {
		t := float64(y) / hgt
		row := color.RGBA{lerp(c1.R, c2.R, t), lerp(c1.G, c2.G, t), lerp(c1.B, c2.B, t), 255}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, row)
		}
	}
	small := image.NewRGBA(image.Rect(0, 0, w/2, hgt/2))
	var lines []string
	cur := ""
	for _, wd := range strings.Fields(title) {
		switch {
		case cur == "":
			cur = wd
		case len(cur)+1+len(wd) > 17:
			lines = append(lines, cur)
			cur = wd
		default:
			cur += " " + wd
		}
	}
	lines = append(lines, cur)
	if len(lines) > 4 {
		lines = lines[:4]
	}
	dr := &font.Drawer{Dst: small, Src: image.White, Face: basicfont.Face7x13}
	y := hgt/4 - len(lines)*15/2 + 12
	for _, ln := range lines {
		r := []rune(ln)
		if len(r) > 20 {
			r = r[:20]
		}
		dr.Dot = fixed.P((w/2-len(r)*7)/2, y)
		dr.DrawString(string(r))
		y += 15
	}
	for yy := 0; yy < hgt; yy++ {
		for xx := 0; xx < w; xx++ {
			if _, _, _, a := small.At(xx/2, yy/2).RGBA(); a > 0 {
				img.SetRGBA(xx, yy, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func lerp(a, b uint8, t float64) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }

// nullYear stores an unknown release year (0) as NULL.
func nullYear(y int) any {
	if y == 0 {
		return nil
	}
	return y
}
