package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

func TestLinkQueueAcceptAndNone(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`INSERT INTO game (id, title, title_sort, status_id, igdb_checked_at) VALUES (1, 'Prey', 'prey', 4, '2026-01-01T00:00:00Z'), (2, 'Celeste', 'celeste', 4, '2026-01-01T00:00:00Z'), (3, 'Taken', 'taken', 4, NULL)`,
		`UPDATE game SET igdb_id = 11 WHERE id = 3`,
		`INSERT INTO sync_candidate (game_id, igdb_id, name, year, score) VALUES (1, 10, 'Prey', 2006, 100), (1, 11, 'Prey', 2017, 100)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir()}).Handler())
	defer srv.Close()
	post := func(path, body string) int {
		res, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	queue := func() []struct {
		GameID     int64 `json:"game_id"`
		Candidates []struct {
			IGDBID int64 `json:"igdb_id"`
		} `json:"candidates"`
	} {
		res, err := http.Get(srv.URL + "/api/link/queue")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			Items []struct {
				GameID     int64 `json:"game_id"`
				Candidates []struct {
					IGDBID int64 `json:"igdb_id"`
				} `json:"candidates"`
			} `json:"items"`
		}
		json.NewDecoder(res.Body).Decode(&out)
		return out.Items
	}
	if q := queue(); len(q) != 2 || q[0].GameID != 1 || len(q[0].Candidates) != 2 || len(q[1].Candidates) != 0 {
		t.Fatalf("queue = %+v", q)
	}
	if c := post("/api/link/accept", `{"game_id":1,"igdb_id":11}`); c != http.StatusConflict { // game 3 owns 11
		t.Errorf("taken id: status %d, want 409", c)
	}
	if c := post("/api/link/accept", `{"game_id":1,"igdb_id":10}`); c != http.StatusNoContent {
		t.Errorf("accept: status %d", c)
	}
	if c := post("/api/link/accept", `{"game_id":1,"igdb_id":10}`); c != http.StatusNoContent { // same id again
		t.Errorf("repeat accept: status %d", c)
	}
	if c := post("/api/link/accept", `{"game_id":1,"igdb_id":99}`); c != http.StatusConflict { // never replaces a link
		t.Errorf("different id: status %d, want 409", c)
	}
	if c := post("/api/link/accept", `{"game_id":1}`); c != http.StatusBadRequest {
		t.Errorf("missing igdb_id: status %d", c)
	}
	if c := post("/api/link/accept", `{"game_id":404,"igdb_id":5}`); c != http.StatusNotFound {
		t.Errorf("unknown game: status %d", c)
	}
	if c := post("/api/link/none", `{"game_id":2}`); c != http.StatusNoContent {
		t.Errorf("none: status %d", c)
	}
	if q := queue(); len(q) != 0 {
		t.Errorf("queue after decisions = %+v", q)
	}
	res, _ := http.Get(srv.URL + "/api/games")
	var games []Game
	json.NewDecoder(res.Body).Decode(&games)
	res.Body.Close()
	for _, g := range games {
		if g.ID == 1 && (g.IGDBID == nil || *g.IGDBID != 10) {
			t.Errorf("game 1 igdb_id = %v after accept (cache not refreshed?)", g.IGDBID)
		}
	}
}
