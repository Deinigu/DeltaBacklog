package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/collections"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

type hiddenDeal struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	Cover    *string `json:"cover"`
	CoverV   string  `json:"cover_v"`
	HiddenAt string  `json:"hidden_at"`
}

// hiddenList answers everything the owner hid, by place (Deals, Similar games, Series), from the database only: no
// IGDB or IsThereAnyDeal call. Each group has what its own page's Hidden list has, plus when it was hidden.
func (s *Server) hiddenList(w http.ResponseWriter, r *http.Request) {
	dl, err := hiddenDeals(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	sim, err := collections.LoadHiddenSimilar(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	ser, err := collections.LoadHiddenSeries(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"deals": dl, "similar": sim, "series": ser})
}

// hiddenDeals lists the games hidden from Deals that Deals' own Hidden list shows (still unowned and unplayed).
func hiddenDeals(d *sql.DB) ([]hiddenDeal, error) {
	l, err := deals.Load(d)
	if err != nil {
		return nil, err
	}
	out := []hiddenDeal{}
	if len(l.Hidden) == 0 {
		return out, nil
	}
	ids := make([]any, len(l.Hidden))
	for i, id := range l.Hidden {
		ids[i] = id
	}
	rows, err := d.Query(`SELECT g.id, g.title, g.cover_file, coalesce(g.cover_updated_at, ''), h.hidden_at FROM game g JOIN deal_hidden h ON h.game_id = g.id
		WHERE g.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) ORDER BY h.hidden_at DESC, g.title_sort`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g hiddenDeal
		if err := rows.Scan(&g.ID, &g.Title, &g.Cover, &g.CoverV, &g.HiddenAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
