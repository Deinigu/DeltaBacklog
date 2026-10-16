package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// gamePatch is the writable subset of a game. Pointers distinguish "absent" from empty/false.
// Text lookups (ownership, series, developer) clear with ""; numbers clear with 0; lists replace the whole set.
type gamePatch struct {
	Status      *string   `json:"status"` // status code
	Favorite    *bool     `json:"favorite"`
	VR          *bool     `json:"vr"`
	Completed   *bool     `json:"completed"`
	Title       *string   `json:"title"`
	Notes       *string   `json:"notes"`
	Ownership   *string   `json:"ownership"`
	Series      *string   `json:"series"`
	Developer   *string   `json:"developer"`
	ReleaseYear *int      `json:"release_year"`
	Platforms   *[]string `json:"platforms"`
	Genres      *[]string `json:"genres"`
	Themes      *[]string `json:"themes"`
	Players     *[]string `json:"players"`
	Years       *[]int    `json:"years"`   // calendar years played
	Periods     *[]string `json:"periods"` // "childhood", "always"
	IGDBID      *int64    `json:"igdb_id"` // links the game to IGDB; 0 clears
}

var errInvalid = errors.New("invalid value")

// validate checks everything that does not need the database.
func (in *gamePatch) validate() error {
	if in.Title != nil && (strings.TrimSpace(*in.Title) == "" || len(*in.Title) > 200) {
		return errInvalid
	}
	if in.Notes != nil && len(*in.Notes) > 20000 {
		return errInvalid
	}
	for _, p := range []*string{in.Ownership, in.Series, in.Developer} {
		if p != nil && len(*p) > 120 {
			return errInvalid
		}
	}
	if in.IGDBID != nil && *in.IGDBID < 0 {
		return errInvalid
	}
	if in.ReleaseYear != nil && *in.ReleaseYear != 0 && (*in.ReleaseYear < 1950 || *in.ReleaseYear > 2100) {
		return errInvalid
	}
	for _, l := range []*[]string{in.Platforms, in.Genres, in.Themes, in.Players} {
		if l == nil {
			continue
		}
		if len(*l) > 50 {
			return errInvalid
		}
		for _, n := range *l {
			if len(n) > 120 {
				return errInvalid
			}
		}
	}
	if in.Years != nil {
		for _, y := range *in.Years {
			if y < 1970 || y > 2100 {
				return errInvalid
			}
		}
	}
	if in.Periods != nil {
		for _, p := range *in.Periods {
			if p != "childhood" && p != "always" {
				return errInvalid
			}
		}
	}
	return nil
}

// setLinks replaces the lookup values of one game. table, junction and fk are constants from the caller.
func setLinks(tx *sql.Tx, gameID int64, table, junction, fk string, names []string) error {
	if _, err := tx.Exec("DELETE FROM "+junction+" WHERE game_id = ?", gameID); err != nil {
		return err
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		id, err := db.GetOrCreate(tx, table, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO "+junction+"(game_id, "+fk+") VALUES (?, ?)", gameID, id); err != nil {
			return err
		}
	}
	return nil
}

// nullableLookup returns the id of a lookup row, or nil when name is empty (clearing the field).
func nullableLookup(tx *sql.Tx, table, name string) (any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	return db.GetOrCreate(tx, table, name)
}

// nullInt64 stores nil and 0 as NULL.
func nullInt64(p *int64) any {
	if p == nil || *p == 0 {
		return nil
	}
	return *p
}

// foldThemes maps verbose IGDB theme names ("4X (explore, expand, ...)") to the short ones the library uses.
func foldThemes(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = db.NormalizeTheme(strings.TrimSpace(n))
	}
	return out
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// playingStatuses are the shelves that record the current year in the game's years played.
var playingStatuses = map[string]bool{"playing": true, "playing_evergreen": true, "finished": true}

func (s *Server) patchGame(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in gamePatch
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.validate() != nil {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()

	var curStatus int64
	if err := tx.QueryRow("SELECT status_id FROM game WHERE id = ?", id).Scan(&curStatus); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}

	if in.IGDBID != nil && *in.IGDBID != 0 {
		var other int64
		if err := tx.QueryRow("SELECT id FROM game WHERE igdb_id = ? AND id <> ?", *in.IGDBID, id).Scan(&other); err == nil {
			writeErr(w, http.StatusConflict, "another game in your library already has that IGDB id", map[string]any{"id": other})
			return
		}
	}

	// Field edits come first: an explicit list of years must not wipe the year the status change records below.
	if err := applyFieldEdits(tx, id, &in); err != nil {
		fail(w, err)
		return
	}

	if in.Status != nil {
		var sid int64
		if err := tx.QueryRow("SELECT id FROM status WHERE code = ?", *in.Status).Scan(&sid); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown status", nil)
			return
		}
		if sid != curStatus {
			if playingStatuses[*in.Status] {
				if err := recordCurrentYear(tx, id); err != nil {
					fail(w, err)
					return
				}
			}
			// A game that changes shelf goes to the end of the manual order.
			if _, err := tx.Exec(`UPDATE game SET status_id = ?, queue_pos = (SELECT coalesce(max(queue_pos), 0) + 1 FROM game) WHERE id = ?`, sid, id); err != nil {
				fail(w, err)
				return
			}
		}
	}
	if err := applyFlags(tx, id, &in); err != nil {
		fail(w, err)
		return
	}
	if _, err := tx.Exec(`UPDATE game SET updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`, id); err != nil {
		fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, g)
}

// applyFlags stores the favorite / VR / 100% switches that are present.
func applyFlags(tx *sql.Tx, id int64, in *gamePatch) error {
	for col, v := range map[string]*bool{"favorite": in.Favorite, "vr": in.VR, "completed": in.Completed} {
		if v == nil {
			continue
		}
		if _, err := tx.Exec("UPDATE game SET "+col+" = ? WHERE id = ?", b2i(*v), id); err != nil { // col comes from the map above, never from input
			return err
		}
	}
	return nil
}

// recordCurrentYear counts a game as played this year (starting or finishing it).
func recordCurrentYear(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO play(game_id, year) VALUES (?, CAST(strftime('%Y', 'now') AS INTEGER))`, id)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SlotOrder assigns the given positions to ids in order. The positions are the ids' own current
// positions sorted ascending, so games not being moved keep theirs: reordering a 5-game view never
// disturbs where the other 580 games sit in the global order.
func SlotOrder(ids []int64, current map[int64]int) map[int64]int {
	slots := make([]int, 0, len(ids))
	for _, id := range ids {
		slots = append(slots, current[id])
	}
	sort.Ints(slots)
	out := make(map[int64]int, len(ids))
	for i, id := range ids {
		out[id] = slots[i]
	}
	return out
}

// reorderGames takes the ids of the games a view shows, in the desired order.
func (s *Server) reorderGames(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil || len(ids) == 0 {
		writeErr(w, http.StatusBadRequest, "expected a non-empty list of game ids", nil)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()

	current := make(map[int64]int, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			writeErr(w, http.StatusBadRequest, "duplicate id", nil)
			return
		}
		seen[id] = true
		var pos sql.NullInt64
		if err := tx.QueryRow("SELECT queue_pos FROM game WHERE id = ?", id).Scan(&pos); errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusBadRequest, "unknown game id", nil)
			return
		} else if err != nil {
			fail(w, err)
			return
		}
		current[id] = int(pos.Int64)
	}
	// Games without a position (never ordered) get consecutive ones after the current maximum.
	var max int
	tx.QueryRow("SELECT coalesce(max(queue_pos), 0) FROM game").Scan(&max)
	for _, id := range ids {
		if current[id] == 0 {
			max++
			current[id] = max
		}
	}
	next := SlotOrder(ids, current)
	for id, pos := range next {
		if _, err := tx.Exec("UPDATE game SET queue_pos = ? WHERE id = ?", pos, id); err != nil {
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, next)
}

// applyFieldEdits writes the non-status fields of a patch inside the caller's transaction.
func applyFieldEdits(tx *sql.Tx, id int64, in *gamePatch) error {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if _, err := tx.Exec("UPDATE game SET title = ?, title_sort = ? WHERE id = ?", t, db.TitleSort(t), id); err != nil {
			return err
		}
	}
	if in.Notes != nil {
		if _, err := tx.Exec("UPDATE game SET notes = ? WHERE id = ?", *in.Notes, id); err != nil {
			return err
		}
	}
	for _, f := range []struct {
		val           *string
		table, column string
	}{{in.Ownership, "ownership", "ownership_id"}, {in.Series, "series", "series_id"}, {in.Developer, "company", "developer_id"}} {
		if f.val == nil {
			continue
		}
		ref, err := nullableLookup(tx, f.table, *f.val)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE game SET "+f.column+" = ? WHERE id = ?", ref, id); err != nil { // column is a constant above
			return err
		}
	}
	if in.Series != nil {
		// Whatever the owner sets (or clears) is their decision: the enrichment must never fill this series again.
		if _, err := tx.Exec("UPDATE game SET igdb_series = NULL WHERE id = ?", id); err != nil {
			return err
		}
	}
	if in.IGDBID != nil {
		var current sql.NullInt64
		if err := tx.QueryRow("SELECT igdb_id FROM game WHERE id = ?", id).Scan(&current); err != nil {
			return err
		}
		next := nullInt64(in.IGDBID)
		if (next == nil) != !current.Valid || (next != nil && next.(int64) != current.Int64) {
			// A different (or no) IGDB game: what the enrichment stored for the old one is not this game's. All of it is
			// IGDB's data, never owner-entered, so it goes; `sync-igdb --enrich` reads the new link.
			for _, q := range []string{
				"UPDATE game SET igdb_enriched_at = NULL, igdb_series = NULL, time_to_beat_hastily = NULL, time_to_beat_normally = NULL, time_to_beat_completely = NULL WHERE id = ?",
				"DELETE FROM game_game_mode WHERE game_id = ?",
				"DELETE FROM game_perspective WHERE game_id = ?",
				"DELETE FROM game_link WHERE game_id = ?",
			} {
				if _, err := tx.Exec(q, id); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec("UPDATE game SET igdb_id = ? WHERE id = ?", next, id); err != nil {
			return err
		}
	}
	if in.ReleaseYear != nil {
		if _, err := tx.Exec("UPDATE game SET release_year = ? WHERE id = ?", nullInt(*in.ReleaseYear), id); err != nil {
			return err
		}
	}
	for _, l := range []struct {
		names               *[]string
		table, junction, fk string
	}{
		{in.Platforms, "platform", "game_platform", "platform_id"},
		{in.Genres, "genre", "game_genre", "genre_id"},
		{in.Themes, "theme", "game_theme", "theme_id"},
		{in.Players, "player", "game_player", "player_id"},
	} {
		if l.names == nil {
			continue
		}
		names := *l.names
		if l.table == "theme" {
			names = foldThemes(names) // the same folding as the importer and quick add
		}
		if err := setLinks(tx, id, l.table, l.junction, l.fk, names); err != nil {
			return err
		}
	}
	if in.Years != nil {
		if _, err := tx.Exec("DELETE FROM play WHERE game_id = ? AND year IS NOT NULL", id); err != nil {
			return err
		}
		for _, y := range *in.Years {
			if _, err := tx.Exec("INSERT OR IGNORE INTO play(game_id, year) VALUES (?, ?)", id, y); err != nil {
				return err
			}
		}
	}
	if in.Periods != nil {
		if _, err := tx.Exec("DELETE FROM play WHERE game_id = ? AND period IS NOT NULL", id); err != nil {
			return err
		}
		for _, p := range *in.Periods {
			if _, err := tx.Exec("INSERT OR IGNORE INTO play(game_id, period) VALUES (?, ?)", id, p); err != nil {
				return err
			}
		}
	}
	return db.IndexGame(tx, id)
}
