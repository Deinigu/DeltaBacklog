package api

import (
	"encoding/json"
	"testing"
)

type hiddenResp struct {
	Deals []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Poster   string `json:"poster"`
		PosterV  string `json:"poster_v"`
		HiddenAt string `json:"hidden_at"`
	} `json:"deals"`
	Similar []struct {
		IGDBID   int64    `json:"igdb_id"`
		Title    string   `json:"title"`
		HiddenAt string   `json:"hidden_at"`
		Because  []string `json:"because"`
	} `json:"similar"`
	Series []struct {
		CollectionID int64  `json:"collection_id"`
		IGDBID       int64  `json:"igdb_id"`
		Title        string `json:"title"`
		Series       string `json:"series"`
		HiddenAt     string `json:"hidden_at"`
	} `json:"series"`
}

func (e *seriesEnv) hidden(t *testing.T) hiddenResp {
	t.Helper()
	var r hiddenResp
	if err := json.Unmarshal(e.req(t, "GET", "/api/hidden", "", 200), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// GET /api/hidden lists what the three Hide buttons hid, from the database, and the existing unhide endpoints empty it.
func TestHiddenPage(t *testing.T) {
	e := newSeriesEnv(t, true)
	r := e.hidden(t)
	if len(r.Deals)+len(r.Similar)+len(r.Series) != 0 {
		t.Fatalf("nothing is hidden yet: %+v", r)
	}
	e.refresh(t) // series cache: collection 7 "Zed Series" with Zed II (101) and Zed III (102)
	e.req(t, "POST", "/api/series/hide", `{"collection_id":7,"igdb_id":101}`, 200)
	// a deals game (backlog, not owned, no plays), hidden; and a hidden game that no longer qualifies (played): not listed
	e.db.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('Deal','deal',4), ('Played','played',4)`)
	e.db.Exec(`INSERT INTO play(game_id, year) SELECT id, 2020 FROM game WHERE title = 'Played'`)
	e.db.Exec(`UPDATE game SET poster_file = 'p2.jpg', poster_updated_at = '2025-01-01T00:00:00Z' WHERE title = 'Deal'`)
	e.req(t, "PUT", "/api/deals/hidden/2", "", 200)
	e.db.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('Older','older',4)`)
	e.db.Exec(`INSERT INTO deal_hidden(game_id, hidden_at) SELECT id, '2020-01-01T00:00:00Z' FROM game WHERE title = 'Older'`)
	e.db.Exec(`INSERT INTO deal_hidden(game_id, hidden_at) SELECT id, '2024-01-01T00:00:00Z' FROM game WHERE title = 'Played'`)
	// a hidden suggestion of the finished game Zed (IGDB 100), and one suggested by nobody
	e.db.Exec(`INSERT INTO similar_game(igdb_id, title, release_year, game_type) VALUES (201, 'Sim', 2010, 0), (202, 'Orphan', 2011, 0)`)
	// a second source, played in 2024: it comes before Zed (no year played) in "Because you played"
	e.db.Exec(`INSERT INTO game(title, title_sort, status_id, igdb_id) VALUES ('Newer','newer',7,300)`)
	e.db.Exec(`INSERT INTO play(game_id, year) SELECT id, 2024 FROM game WHERE title = 'Newer'`)
	e.db.Exec(`INSERT INTO similar_link(source_igdb_id, similar_igdb_id) VALUES (100, 201), (300, 201), (999, 202)`)
	e.db.Exec(`INSERT INTO similar_hidden(igdb_id) VALUES (201), (202)`)

	r = e.hidden(t)
	if len(r.Deals) != 2 || r.Deals[0].Title != "Deal" || r.Deals[1].Title != "Older" || r.Deals[0].HiddenAt == "" || r.Deals[0].Poster != "p2.jpg" || r.Deals[0].PosterV == "" {
		t.Errorf("deals: %+v", r.Deals)
	}
	if len(r.Similar) != 1 || r.Similar[0].Title != "Sim" || len(r.Similar[0].Because) != 2 || r.Similar[0].Because[0] != "Newer" || r.Similar[0].Because[1] != "Zed" || r.Similar[0].HiddenAt == "" {
		t.Errorf("similar: %+v", r.Similar)
	}
	if len(r.Series) != 1 || r.Series[0].IGDBID != 101 || r.Series[0].Series != "Zeds" || r.Series[0].CollectionID != 7 || r.Series[0].HiddenAt == "" {
		t.Errorf("series: %+v", r.Series)
	}

	// a hidden game that is now in the library drops out
	e.db.Exec(`INSERT INTO game(title, title_sort, status_id, igdb_id) VALUES ('Zed II','zed ii',4,101)`)
	if r := e.hidden(t); len(r.Series) != 0 {
		t.Errorf("owned game still listed: %+v", r.Series)
	}
	e.req(t, "DELETE", "/api/deals/hidden/2", "", 200)
	e.req(t, "DELETE", "/api/deals/hidden/4", "", 200)
	e.req(t, "POST", "/api/similar/unhide", `{"igdb_id":201}`, 200)
	e.req(t, "POST", "/api/series/unhide", `{"collection_id":7,"igdb_id":101}`, 200)
	r = e.hidden(t)
	if len(r.Deals)+len(r.Similar)+len(r.Series) != 0 {
		t.Errorf("after Show again: %+v", r)
	}
}
