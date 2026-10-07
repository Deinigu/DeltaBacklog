package api

import (
	"database/sql"
	"net/http"
)

// Facet is one selectable filter value with the number of games that have it.
type Facet struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
	Count int    `json:"count"`
}

func (s *Server) facets(w http.ResponseWriter, r *http.Request) {
	s.serveCached(w, r, "facets", s.buildFacets)
}

func (s *Server) buildFacets() (any, error) {
	out := map[string][]Facet{}
	queries := []struct{ name, query string }{
		{"status", `SELECT s.code, s.label, s.color, count(g.id) FROM status s LEFT JOIN game g ON g.status_id = s.id GROUP BY s.id ORDER BY s.sort`},
		{"platform", junctionFacet("platform", "game_platform", "platform_id")},
		{"genre", junctionFacet("genre", "game_genre", "genre_id")},
		{"theme", junctionFacet("theme", "game_theme", "theme_id")},
		{"player", junctionFacet("player", "game_player", "player_id")},
		{"ownership", `SELECT o.name, o.name, '', count(g.id) FROM ownership o LEFT JOIN game g ON g.ownership_id = o.id GROUP BY o.id ORDER BY count(g.id) DESC, o.name`},
		{"series", `SELECT s.name, s.name, '', count(g.id) FROM series s LEFT JOIN game g ON g.series_id = s.id GROUP BY s.id HAVING count(g.id) > 0 ORDER BY count(g.id) DESC, s.name`},
		{"year", `SELECT CAST(year AS TEXT), CAST(year AS TEXT), '', count(*) FROM play WHERE year IS NOT NULL GROUP BY year ORDER BY year DESC`},
	}
	for _, q := range queries {
		f, err := loadFacets(s.DB, q.query)
		if err != nil {
			return nil, err
		}
		out[q.name] = f
	}
	return out, nil
}

func junctionFacet(table, junction, fk string) string {
	return `SELECT t.name, t.name, '', count(j.game_id) FROM ` + table + ` t
		LEFT JOIN ` + junction + ` j ON j.` + fk + ` = t.id GROUP BY t.id ORDER BY count(j.game_id) DESC, t.name`
}

func loadFacets(d *sql.DB, query string) ([]Facet, error) {
	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Facet{}
	for rows.Next() {
		var f Facet
		if err := rows.Scan(&f.Key, &f.Label, &f.Color, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
