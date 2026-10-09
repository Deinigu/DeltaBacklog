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
	Developer   *string  `json:"developer"`
	ReleaseYear *int     `json:"release_year"`
	VR          bool     `json:"vr"`
	Favorite    bool     `json:"favorite"`
	Completed   bool     `json:"completed"`
	Notes       string   `json:"notes"`
	Cover       *string  `json:"cover"` // file name under /covers/
	QueuePos    *int     `json:"queue_pos"`
	IGDBID      *int64   `json:"igdb_id"`
	AddedAt     string   `json:"added_at"`
	UpdatedAt   string   `json:"updated_at"`
	CoverV      string   `json:"cover_v"` // changes only when the cover image does: the browser cache key for /covers/
	Platforms   []string `json:"platforms"`
	Genres      []string `json:"genres"`
	Themes      []string `json:"themes"`
	Players     []string `json:"players"`
	Years       []int    `json:"years"`   // calendar years played
	Periods     []string `json:"periods"` // "childhood", "always"

	// IGDB enrichment (empty until `deltabacklog sync-igdb --enrich` has run). Times are minutes.
	TTBHastily    *int        `json:"ttb_hastily"`
	TTBNormally   *int        `json:"ttb_normally"`
	TTBCompletely *int        `json:"ttb_completely"`
	Modes         []string    `json:"modes"`
	Perspectives  []string    `json:"perspectives"`
	Links         []StoreLink `json:"links"`
}

// StoreLink is a store or official-site page of a game: Kind is "steam", "gog", "epic" or "official".
type StoreLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// loadLinks fetches (game_id, value) pairs from one junction query and groups them by game.
func loadLinks(d *sql.DB, query string, args ...any) (map[int64][]string, error) {
	rows, err := d.Query(query, args...)
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
	s.serveCached(w, r, "games", func() (any, error) { return LoadGames(s.DB) })
}

// LoadGames returns every game ordered by status then queue position.
func LoadGames(d *sql.DB) ([]Game, error) { return loadGames(d, 0) }

// LoadGame returns one game with its links, reading only that game's rows (an edit must not cost a full reload).
func LoadGame(d *sql.DB, id int64) (Game, error) {
	games, err := loadGames(d, id)
	if err != nil {
		return Game{}, err
	}
	if len(games) == 0 {
		return Game{}, sql.ErrNoRows
	}
	return games[0], nil
}

// loadGames reads all games, or only the game with id `only` when it is not 0.
func loadGames(d *sql.DB, only int64) ([]Game, error) {
	var args []any
	where, linkWhere, playAnd := "", "", ""
	if only != 0 {
		args = []any{only}
		where, linkWhere, playAnd = " WHERE g.id = ?", " WHERE x.game_id = ?", " AND game_id = ?"
	}
	rows, err := d.Query(`SELECT g.id, g.title, s.code, o.name, se.name, c.name, g.release_year,
			g.vr, g.favorite, g.completed, g.notes, g.cover_file, g.queue_pos, g.igdb_id, g.added_at, g.updated_at, g.cover_updated_at,
			g.time_to_beat_hastily, g.time_to_beat_normally, g.time_to_beat_completely
		FROM game g
		JOIN status s ON s.id = g.status_id
		LEFT JOIN ownership o ON o.id = g.ownership_id
		LEFT JOIN series se ON se.id = g.series_id
		LEFT JOIN company c ON c.id = g.developer_id`+where+`
		ORDER BY s.sort, g.queue_pos, g.title_sort`, args...)
	if err != nil {
		return nil, err
	}
	games := []Game{}
	for rows.Next() {
		var g Game
		if err := rows.Scan(&g.ID, &g.Title, &g.Status, &g.Ownership, &g.Series, &g.Developer,
			&g.ReleaseYear, &g.VR, &g.Favorite, &g.Completed, &g.Notes, &g.Cover, &g.QueuePos, &g.IGDBID, &g.AddedAt, &g.UpdatedAt, &g.CoverV,
			&g.TTBHastily, &g.TTBNormally, &g.TTBCompletely); err != nil {
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
		{`SELECT x.game_id, p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Platforms = v }},
		{`SELECT x.game_id, p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Genres = v }},
		{`SELECT x.game_id, p.name FROM game_theme x JOIN theme p ON p.id = x.theme_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Themes = v }},
		{`SELECT x.game_id, p.name FROM game_player x JOIN player p ON p.id = x.player_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Players = v }},
		{`SELECT x.game_id, p.name FROM game_game_mode x JOIN game_mode p ON p.id = x.game_mode_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Modes = v }},
		{`SELECT x.game_id, p.name FROM game_perspective x JOIN perspective p ON p.id = x.perspective_id` + linkWhere + ` ORDER BY p.name`, func(g *Game, v []string) { g.Perspectives = v }},
		{`SELECT game_id, period FROM play WHERE period IS NOT NULL` + playAnd + ` ORDER BY period`, func(g *Game, v []string) { g.Periods = v }},
	}
	for _, l := range links {
		m, err := loadLinks(d, l.query, args...)
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

	lrows, err := d.Query(`SELECT x.game_id, x.kind, x.url FROM game_link x`+linkWhere+` ORDER BY x.game_id, x.kind`, args...)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	storeLinks := map[int64][]StoreLink{}
	for lrows.Next() {
		var id int64
		var l StoreLink
		if err := lrows.Scan(&id, &l.Kind, &l.URL); err != nil {
			return nil, err
		}
		storeLinks[id] = append(storeLinks[id], l)
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}
	for i := range games {
		if games[i].Links = storeLinks[games[i].ID]; games[i].Links == nil {
			games[i].Links = []StoreLink{}
		}
	}

	yrows, err := d.Query(`SELECT game_id, year FROM play WHERE year IS NOT NULL`+playAnd+` ORDER BY year`, args...)
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
