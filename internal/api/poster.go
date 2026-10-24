package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// unreachable tells "the service is not answering" (a network error, a block, a rate limit, a server error) from an
// answer that says no (a bad key or request): the poster picker words the first one as "can't be reached right now".
func unreachable(err error) bool {
	var he *meta.HTTPError
	if errors.As(err, &he) {
		return he.Status >= 500 || he.Status == 403 || he.Status == 429
	}
	return true
}

// setPoster changes the portrait poster of a game: {"url": ...} an image from an allowed host, {"crop": true} a centre
// crop of the wide cover, {"auto": true} back to the automatic poster. A poster set by url or crop is the user's and
// is never replaced by the background queue or `fetch-posters --upgrade`.
func (s *Server) setPoster(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		URL  string `json:"url"`
		Crop bool   `json:"crop"`
		Auto bool   `json:"auto"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request", nil)
		return
	}
	kinds := 0
	for _, set := range []bool{in.URL != "", in.Crop, in.Auto} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		writeErr(w, http.StatusBadRequest, "give one of url, crop or auto", nil)
		return
	}
	if in.URL != "" && !s.coverHostAllowed(in.URL) {
		writeErr(w, http.StatusBadRequest, "image URL not allowed", nil)
		return
	}
	var one int
	if err := s.DB.QueryRow("SELECT 1 FROM game WHERE id = ?", id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	m := s.posterQ().M
	var err error
	switch {
	case in.URL != "":
		err = m.Choose(id, in.URL)
	case in.Crop:
		err = m.ChooseCrop(id)
	default:
		_, err = m.Remake(r.Context(), id)
	}
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	case errors.Is(err, posters.ErrNoSource):
		writeErr(w, http.StatusUnprocessableEntity, "there is no image to make a poster from", nil)
		return
	case errors.Is(err, posters.ErrTemporary):
		writeErr(w, http.StatusBadGateway, "the image services did not answer; the poster was not changed", nil)
		return
	default:
		writeErr(w, http.StatusBadGateway, "could not use that image", nil)
		return
	}
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, g)
}
