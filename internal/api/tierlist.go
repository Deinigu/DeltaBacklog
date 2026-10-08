package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

// Tier is one row of a tier list (S, A, B...). The tiers are shared by every year.
type Tier struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// TierEntry places one game in a tier. In a response the entries are ordered by tier, then by position.
type TierEntry struct {
	GameID int64 `json:"game_id"`
	TierID int64 `json:"tier_id"`
}

// Tierlist is the saved ranking of one year. Games of that year that are not listed are unranked.
type Tierlist struct {
	Year    int         `json:"year"`
	Tiers   []Tier      `json:"tiers"`
	Entries []TierEntry `json:"entries"`
}

const maxTierEntries = 1000

// parseYear reads the {year} path value; the range is the one the play table enforces.
func parseYear(r *http.Request) (int, bool) {
	y, err := strconv.Atoi(r.PathValue("year"))
	return y, err == nil && y >= 1970 && y <= 2100
}

func loadTierlist(d *sql.DB, year int) (Tierlist, error) {
	tl := Tierlist{Year: year, Tiers: []Tier{}, Entries: []TierEntry{}}
	rows, err := d.Query(`SELECT id, name, color FROM tier ORDER BY sort, id`)
	if err != nil {
		return tl, err
	}
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			rows.Close()
			return tl, err
		}
		tl.Tiers = append(tl.Tiers, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return tl, err
	}
	// Only games that still have a play in this year: removing a year from a game hides its entry (and
	// restoring the year brings it back) instead of leaving a ghost in the list.
	erows, err := d.Query(`SELECT e.game_id, e.tier_id FROM tierlist_entry e
		JOIN tier t ON t.id = e.tier_id
		WHERE e.year = ? AND EXISTS (SELECT 1 FROM play p WHERE p.game_id = e.game_id AND p.year = e.year)
		ORDER BY t.sort, e.pos, e.game_id`, year)
	if err != nil {
		return tl, err
	}
	defer erows.Close()
	for erows.Next() {
		var e TierEntry
		if err := erows.Scan(&e.GameID, &e.TierID); err != nil {
			return tl, err
		}
		tl.Entries = append(tl.Entries, e)
	}
	return tl, erows.Err()
}

func (s *Server) getTierlist(w http.ResponseWriter, r *http.Request) {
	year, ok := parseYear(r)
	if !ok {
		http.Error(w, "bad year", http.StatusBadRequest)
		return
	}
	tl, err := loadTierlist(s.DB, year)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, tl)
}

// putTierlist replaces the whole list of a year. The body is {"entries": [{game_id, tier_id}, ...]}; the
// order of the entries inside each tier is the order shown. A game must have a play in that year, and the
// "entries" key is required (an explicit [] clears the list; a missing key is a mistake, not a request to clear).
// Entries of games that lost the year are hidden in GET and are kept here, so they return with the year.
func (s *Server) putTierlist(w http.ResponseWriter, r *http.Request) {
	year, ok := parseYear(r)
	if !ok {
		http.Error(w, "bad year", http.StatusBadRequest)
		return
	}
	var in struct {
		Entries *[]TierEntry `json:"entries"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&in); err != nil || in.Entries == nil || len(*in.Entries) > maxTierEntries {
		http.Error(w, "invalid tier list", http.StatusBadRequest)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tierlist_entry WHERE year = ?
		AND EXISTS (SELECT 1 FROM play p WHERE p.game_id = tierlist_entry.game_id AND p.year = tierlist_entry.year)`, year); err != nil {
		fail(w, err)
		return
	}
	seen := map[int64]bool{}
	pos := map[int64]int{}
	for _, e := range *in.Entries {
		if seen[e.GameID] {
			http.Error(w, "a game appears twice", http.StatusBadRequest)
			return
		}
		seen[e.GameID] = true
		var one int
		err := tx.QueryRow(`SELECT 1 FROM play WHERE game_id = ? AND year = ?`, e.GameID, year).Scan(&one)
		if err == sql.ErrNoRows {
			http.Error(w, "game "+strconv.FormatInt(e.GameID, 10)+" was not played in "+strconv.Itoa(year), http.StatusBadRequest)
			return
		} else if err != nil {
			fail(w, err)
			return
		}
		err = tx.QueryRow(`SELECT 1 FROM tier WHERE id = ?`, e.TierID).Scan(&one)
		if err == sql.ErrNoRows {
			http.Error(w, "unknown tier", http.StatusBadRequest)
			return
		} else if err != nil {
			fail(w, err)
			return
		}
		if _, err := tx.Exec(`INSERT INTO tierlist_entry(year, game_id, tier_id, pos) VALUES (?, ?, ?, ?)`, year, e.GameID, e.TierID, pos[e.TierID]); err != nil {
			fail(w, err)
			return
		}
		pos[e.TierID]++
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	tl, err := loadTierlist(s.DB, year)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, tl)
}
