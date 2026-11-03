package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/igdblink"
)

// igdbDetails updates a linked game's details from its IGDB game (the prompt after relinking). Body: {"mode": "empty"
// or "replace", "apply": false}. Without apply nothing is written and the answer lists what would change. See
// igdblink.Refetch for what is and is not touched.
func (s *Server) igdbDetails(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Mode  string `json:"mode"`
		Apply bool   `json:"apply"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.Mode != "empty" && in.Mode != "replace") {
		writeErr(w, http.StatusBadRequest, "mode must be empty or replace", nil)
		return
	}
	e, ok := s.Meta.(igdblink.Enricher)
	if !ok || !s.igdbConfigured() {
		writeErr(w, http.StatusServiceUnavailable, "IGDB is not configured", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := igdblink.Refetch(ctx, s.DB, e, id, in.Mode == "replace", in.Apply)
	switch {
	case err == nil:
	case errors.Is(err, igdblink.ErrNoGame):
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	case errors.Is(err, igdblink.ErrNotLinked), errors.Is(err, igdblink.ErrStale):
		writeErr(w, http.StatusConflict, "the game is not linked to that IGDB game any more", nil)
		return
	case errors.Is(err, igdblink.ErrNotOnIGDB):
		writeErr(w, http.StatusUnprocessableEntity, "IGDB has no details for this game", nil)
		return
	default:
		writeErr(w, http.StatusBadGateway, "IGDB did not answer; nothing was changed", nil)
		return
	}
	out := map[string]any{"changes": res.Changes, "undo": res.Undo}
	if in.Apply {
		s.cache.invalidate()
		g, err := LoadGame(s.DB, id)
		if err != nil {
			fail(w, err)
			return
		}
		out["game"] = g
	}
	writeJSON(w, out)
}

// attentionIgnoredKey is the setting that holds the games the owner told the Needs attention page to ignore.
const attentionIgnoredKey = "attention_ignored"

// attentionGroups are the groups of the Needs attention page (web/static/js/attention.js GROUPS): the only keys accepted.
var attentionGroups = map[string]bool{"unlinked": true, "uncertain": true, "no_cover": true, "poster": true, "no_ttb": true,
	"no_platform": true, "no_year": true, "no_ownership": true, "no_genres": true}

// attentionIgnored answers {"ignored": {"<group>": [game ids]}}: the games hidden from each group of the page. Ids of
// games that no longer exist are left out.
func (s *Server) attentionIgnored(w http.ResponseWriter, r *http.Request) {
	out := s.loadIgnored()
	exists := map[int64]bool{}
	if rows, err := s.DB.Query("SELECT id FROM game"); err == nil {
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			exists[id] = true
		}
		rows.Close()
	}
	for g, ids := range out {
		keep := ids[:0:0]
		for _, id := range ids {
			if exists[id] {
				keep = append(keep, id)
			}
		}
		if len(keep) == 0 {
			delete(out, g)
		} else {
			out[g] = keep
		}
	}
	writeJSON(w, map[string]any{"ignored": out})
}

func (s *Server) loadIgnored() map[string][]int64 {
	out := map[string][]int64{}
	var raw string
	if err := s.DB.QueryRow("SELECT value FROM setting WHERE key = ?", attentionIgnoredKey).Scan(&raw); err == nil {
		json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// attentionIgnore ignores (or stops ignoring) one game in one group: {"group": "no_poster", "game_id": 3, "ignore": true}.
func (s *Server) attentionIgnore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Group  string `json:"group"`
		GameID int64  `json:"game_id"`
		Ignore bool   `json:"ignore"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.GameID <= 0 || !attentionGroups[in.Group] {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	cur := map[string][]int64{}
	var raw string
	if err := tx.QueryRow("SELECT value FROM setting WHERE key = ?", attentionIgnoredKey).Scan(&raw); err == nil {
		json.Unmarshal([]byte(raw), &cur)
	}
	ids := cur[in.Group][:0:0]
	for _, id := range cur[in.Group] {
		if id != in.GameID {
			ids = append(ids, id)
		}
	}
	if in.Ignore {
		ids = append(ids, in.GameID)
	}
	if len(ids) == 0 {
		delete(cur, in.Group)
	} else {
		cur[in.Group] = ids
	}
	b, _ := json.Marshal(cur)
	if _, err := tx.Exec("INSERT INTO setting(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", attentionIgnoredKey, string(b)); err != nil {
		fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ignored": cur})
}
