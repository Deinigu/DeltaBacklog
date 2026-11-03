package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// uploadSlack is room in the request for the multipart framing around a file of the largest allowed size.
const uploadSlack = 64 << 10

// readUpload reads the "file" part of a multipart request into memory, at most covers.MaxUploadBytes of it, and
// answers the client itself when that fails. Nothing is decoded here.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, covers.MaxUploadBytes+uploadSlack)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected a multipart upload with a file", nil)
		return nil, false
	}
	tooBig := func() {
		writeErr(w, http.StatusRequestEntityTooLarge, "that image is larger than 10 MB", nil)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				tooBig()
			} else {
				writeErr(w, http.StatusBadRequest, "the upload could not be read", nil)
			}
			return nil, false
		}
		if part.FormName() != "file" {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(part, covers.MaxUploadBytes+1))
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			tooBig()
			return nil, false
		case err != nil:
			writeErr(w, http.StatusBadRequest, "the upload could not be read", nil)
			return nil, false
		case len(raw) > covers.MaxUploadBytes:
			tooBig()
			return nil, false
		case len(raw) == 0:
			writeErr(w, http.StatusBadRequest, "the file is empty", nil)
			return nil, false
		}
		return raw, true
	}
	writeErr(w, http.StatusBadRequest, "no file in the upload", nil)
	return nil, false
}

// uploadFailed answers an image that was refused; false means it was some other error (the caller answers it).
func uploadFailed(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, covers.ErrUnsupportedType):
		writeErr(w, http.StatusUnsupportedMediaType, "that file is not a JPEG, PNG or WebP image", nil)
	case errors.Is(err, covers.ErrTooManyPixels):
		writeErr(w, http.StatusRequestEntityTooLarge, "that image has more than 40 megapixels", nil)
	case errors.Is(err, covers.ErrTooCostly):
		writeErr(w, http.StatusRequestEntityTooLarge, "that image is too large to process here; send a smaller version", nil)
	case errors.Is(err, covers.ErrCorrupt):
		writeErr(w, http.StatusUnprocessableEntity, "that image is damaged and could not be read", nil)
	case errors.Is(err, covers.ErrBadImage): // a poster crop that would be smaller than 16 px
		writeErr(w, http.StatusUnprocessableEntity, "that image is too small", nil)
	default:
		return false
	}
	return true
}

// uploadCover replaces the wide cover with an image from the user's device. The cover is then the user's own: nothing
// fetches covers on its own, so no automatic job replaces it.
func (s *Server) uploadCover(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.gameExists(w, id) {
		return
	}
	raw, ok := readUpload(w, r)
	if !ok {
		return
	}
	name, err := covers.SaveUpload(raw, s.CoverDir, id, s.decodeBudget())
	if err != nil {
		if !uploadFailed(w, err) {
			fail(w, err)
		}
		return
	}
	// cover_url is cleared: there is no link behind this image
	if _, err := s.DB.Exec(`UPDATE game SET cover_url = NULL, cover_file = ?, cover_updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`, name, id); err != nil {
		fail(w, err)
		return
	}
	s.writeGame(w, id)
}

// uploadPoster makes the poster from an image from the user's device and marks it as the user's (poster_user = 1), so
// the background queue and `fetch-posters --upgrade` never replace it.
func (s *Server) uploadPoster(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.gameExists(w, id) {
		return
	}
	raw, ok := readUpload(w, r)
	if !ok {
		return
	}
	err := s.posterQ().M.ChooseUpload(id, raw, s.decodeBudget())
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	case uploadFailed(w, err):
		return
	default:
		fail(w, err)
		return
	}
	s.writeGame(w, id)
}

func (s *Server) writeGame(w http.ResponseWriter, id int64) {
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, g)
}

// relinkPosterLater gives a game that was just linked to another IGDB game the poster of that game, after the response
// (a slow IGDB never holds up a link). A poster the user chose or uploaded is kept; an automatic one is replaced by the
// new game's IGDB cover, else SteamGridDB, else the crop. It only writes while the link is still igdbID.
func (s *Server) relinkPosterLater(gameID, igdbID int64) {
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, err := s.posterQ().M.Relink(ctx, gameID, igdbID)
		switch {
		case err == nil:
			s.cache.invalidate()
		case errors.Is(err, posters.ErrTemporary):
			// IGDB or SteamGridDB failed for now: the old link's poster must not stay for good. The poster worker
			// tries again (with its own pause and back-off) and still never replaces a poster the user chose.
			s.posterQ().Requeue(gameID)
		case errors.Is(err, posters.ErrUserPoster), errors.Is(err, posters.ErrStale), errors.Is(err, posters.ErrNoSource), errors.Is(err, sql.ErrNoRows):
			// the user's poster stays, or the link or the game changed meanwhile: nothing to do
		default:
			log.Printf("poster of game %d after linking IGDB game %d: %v", gameID, igdbID, err)
		}
	}()
}
