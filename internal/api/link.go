package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Deinigu/DeltaBacklog/internal/igdblink"
)

// linkQueue lists the games the IGDB sync could not link on its own (the "Link with IGDB" screen).
func (s *Server) linkQueue(w http.ResponseWriter, r *http.Request) {
	items, err := igdblink.Queue(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

type linkBody struct {
	GameID int64 `json:"game_id"`
	IGDBID int64 `json:"igdb_id"`
}

func decodeLink(w http.ResponseWriter, r *http.Request, needIGDB bool) (linkBody, bool) {
	var in linkBody
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.GameID <= 0 || (needIGDB && in.IGDBID <= 0) {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return in, false
	}
	return in, true
}

func (s *Server) linkResult(w http.ResponseWriter, err error) {
	var taken *igdblink.ErrTaken
	switch {
	case err == nil:
		s.cache.invalidate()
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, igdblink.ErrNoGame):
		http.Error(w, "no such game", http.StatusNotFound)
	case errors.As(err, &taken):
		writeErr(w, http.StatusConflict, "that IGDB game is already linked to “"+taken.Title+"”", map[string]any{"id": taken.GameID})
	default:
		fail(w, err)
	}
}

// linkAccept links a game to an IGDB game: one of the proposals, or one chosen with the lookup.
func (s *Server) linkAccept(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeLink(w, r, true)
	if !ok {
		return
	}
	s.linkResult(w, igdblink.Accept(s.DB, in.GameID, in.IGDBID))
}

// linkNone records that a game is not on IGDB, so the sync leaves it alone.
func (s *Server) linkNone(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeLink(w, r, false)
	if !ok {
		return
	}
	s.linkResult(w, igdblink.NotOnIGDB(s.DB, in.GameID))
}
