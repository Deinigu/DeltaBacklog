// Command demo-data builds a small demo library (about 40 well-known games, no personal data) with generated
// placeholder covers, for screenshots and for trying the app out.
//
//	go run ./docs/tools/demo-data -data /tmp/demo
//	LISTEN_ADDR=127.0.0.1:18100 DATA_DIR=/tmp/demo go run ./cmd/deltabacklog
//
// The covers are plain gradients with the title, drawn here, so the repository carries no copyrighted artwork.
// It refuses to write into a data directory that already has a database.
package main

import (
	"bytes"
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

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/db"
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
	flag.Parse()
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
			g.title, db.TitleSort(g.title), statusID, own, series, dev, g.year, b2i(g.vr), b2i(g.fav), b2i(g.completed), g.notes, i+1, 400-i*8)
		must(err)
		id, _ := res.LastInsertId()
		ids[i] = id
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
	fmt.Printf("demo library with %d games written to %s\n", len(games), *dataDir)
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
