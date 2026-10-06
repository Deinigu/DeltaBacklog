package api

import (
	"database/sql"
	"net/http"
)

// Game is the full read model of one game, with multi-valued fields as arrays.
type Game struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"` // status code
	Ownership   *string  `json:"ownership"`
	Series      *string  `json:"series"`
	SeriesOrder *int     `json:"series_order"`
	Developer   *string  `json:"developer"`
	ReleaseYear *int     `json:"release_year"`
	VR          bool     `json:"vr"`
	Favorite    bool     `json:"favorite"`
	Completed   bool     `json:"completed"`
	Notes       string   `json:"notes"`
	Cover       *string  `json:"cover"` // file name under /covers/
	QueuePos    *int     `json:"queue_pos"`
	UpdatedAt   string   `json:"updated_at"`
	Platforms   []string `json:"platforms"`
	Genres      []string `json:"genres"`
	Themes      []string `json:"themes"`
	Players     []string `json:"players"`
	Years       []int    `json:"years"`   // calendar years played
	Periods     []string `json:"periods"` // "childhood", "always"
}

// loadLinks fetches (game_id, value) pairs from one junction query and groups them by game.
func loadLinks(d *sql.DB, query string) (map[int64][]string, error) {
	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = append(out[id], v)
	}
	return out, rows.Err()
}

func (s *Server) listGames(w http.ResponseWriter, r *http.Request) {
	games, err := LoadGames(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, games)
}

// LoadGames returns every game ordered by status then queue position.
func LoadGames(d *sql.DB) ([]Game, error) {
	rows, err := d.Query(`SELECT g.id, g.title, s.code, o.name, se.name, g.series_order, c.name, g.release_year,
			g.vr, g.favorite, g.completed, g.notes, g.cover_file, g.queue_pos, g.updated_at
		FROM game g
		JOIN status s ON s.id = g.status_id
		LEFT JOIN ownership o ON o.id = g.ownership_id
		LEFT JOIN series se ON se.id = g.series_id
		LEFT JOIN company c ON c.id = g.developer_id
		ORDER BY s.sort, g.queue_pos, g.title_sort`)
	if err != nil {
		return nil, err
	}
	games := []Game{}
	for rows.Next() {
		var g Game
		if err := rows.Scan(&g.ID, &g.Title, &g.Status, &g.Ownership, &g.Series, &g.SeriesOrder, &g.Developer,
			&g.ReleaseYear, &g.VR, &g.Favorite, &g.Completed, &g.Notes, &g.Cover, &g.QueuePos, &g.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		games = append(games, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	links := []struct {
		query string
		set   func(*Game, []string)
	}{
		{`SELECT x.game_id, p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id ORDER BY p.name`, func(g *Game, v []string) { g.Platforms = v }},
		{`SELECT x.game_id, p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id ORDER BY p.name`, func(g *Game, v []string) { g.Genres = v }},
		{`SELECT x.game_id, p.name FROM game_theme x JOIN theme p ON p.id = x.theme_id ORDER BY p.name`, func(g *Game, v []string) { g.Themes = v }},
		{`SELECT x.game_id, p.name FROM game_player x JOIN player p ON p.id = x.player_id ORDER BY p.name`, func(g *Game, v []string) { g.Players = v }},
		{`SELECT game_id, period FROM play WHERE period IS NOT NULL ORDER BY period`, func(g *Game, v []string) { g.Periods = v }},
	}
	for _, l := range links {
		m, err := loadLinks(d, l.query)
		if err != nil {
			return nil, err
		}
		for i := range games {
			v := m[games[i].ID]
			if v == nil {
				v = []string{}
			}
			l.set(&games[i], v)
		}
	}

	yrows, err := d.Query(`SELECT game_id, year FROM play WHERE year IS NOT NULL ORDER BY year`)
	if err != nil {
		return nil, err
	}
	defer yrows.Close()
	years := map[int64][]int{}
	for yrows.Next() {
		var id int64
		var y int
		if err := yrows.Scan(&id, &y); err != nil {
			return nil, err
		}
		years[id] = append(years[id], y)
	}
	for i := range games {
		if games[i].Years = years[games[i].ID]; games[i].Years == nil {
			games[i].Years = []int{}
		}
	}
	return games, yrows.Err()
}
