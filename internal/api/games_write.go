package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
)

// gamePatch is the writable subset for quick edits. Pointers distinguish "absent" from false.
type gamePatch struct {
	Status    *string `json:"status"` // status code
	Favorite  *bool   `json:"favorite"`
	VR        *bool   `json:"vr"`
	Completed *bool   `json:"completed"`
}

func (s *Server) patchGame(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in gamePatch
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
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
		http.NotFound(w, r)
		return
	} else if err != nil {
		fail(w, err)
		return
	}

	if in.Status != nil {
		var sid int64
		if err := tx.QueryRow("SELECT id FROM status WHERE code = ?", *in.Status).Scan(&sid); err != nil {
			http.Error(w, "unknown status", http.StatusBadRequest)
			return
		}
		if sid != curStatus {
			// A game that changes shelf goes to the end of the manual order.
			if _, err := tx.Exec(`UPDATE game SET status_id = ?, queue_pos = (SELECT coalesce(max(queue_pos), 0) + 1 FROM game) WHERE id = ?`, sid, id); err != nil {
				fail(w, err)
				return
			}
		}
	}
	for col, v := range map[string]*bool{"favorite": in.Favorite, "vr": in.VR, "completed": in.Completed} {
		if v == nil {
			continue
		}
		if _, err := tx.Exec("UPDATE game SET "+col+" = ? WHERE id = ?", b2i(*v), id); err != nil { // col comes from the map above, never from input
			fail(w, err)
			return
		}
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
		http.Error(w, "expected a non-empty list of game ids", http.StatusBadRequest)
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
			http.Error(w, "duplicate id", http.StatusBadRequest)
			return
		}
		seen[id] = true
		var pos sql.NullInt64
		if err := tx.QueryRow("SELECT queue_pos FROM game WHERE id = ?", id).Scan(&pos); errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "unknown game id", http.StatusBadRequest)
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
