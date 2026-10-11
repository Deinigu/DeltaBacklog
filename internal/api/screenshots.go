package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/shots"
)

const (
	defaultShotMaxFile    = 25 << 20
	defaultShotMaxRequest = 200 << 20
	maxCaption            = 500
)

// errStorage is what an upload says when the screenshot folder is missing or not writable (for example the
// external drive is not mounted). It is shown to the user as it is.
const errStorage = "screenshot storage not available"

// Screenshot is one picture of a game as the API shows it.
type Screenshot struct {
	ID      int64  `json:"id"`
	GameID  int64  `json:"game_id"`
	Src     string `json:"src"`   // the original, e.g. /screenshots/12/<hex>.png
	Thumb   string `json:"thumb"` // the ~480px JPEG
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bytes   int64  `json:"bytes"`
	Taken   string `json:"taken_at,omitempty"`
	Caption string `json:"caption,omitempty"`
	Pos     int    `json:"pos"`
	Added   string `json:"added_at"`
}

const shotCols = `id, game_id, file, thumb, width, height, bytes, coalesce(taken_at, ''), coalesce(caption, ''), pos, added_at`

type rowScanner interface{ Scan(...any) error }

func scanShot(r rowScanner) (Screenshot, error) {
	var s Screenshot
	var file, thumb string
	err := r.Scan(&s.ID, &s.GameID, &file, &thumb, &s.Width, &s.Height, &s.Bytes, &s.Taken, &s.Caption, &s.Pos, &s.Added)
	s.Src = fmt.Sprintf("/screenshots/%d/%s", s.GameID, file)
	s.Thumb = fmt.Sprintf("/screenshots/%d/%s", s.GameID, thumb)
	return s, err
}

func (s *Server) shotLimits() (perFile, perRequest int64) {
	perFile, perRequest = s.ShotMaxFile, s.ShotMaxRequest
	if perFile <= 0 {
		perFile = defaultShotMaxFile
	}
	if perRequest <= 0 {
		perRequest = defaultShotMaxRequest
	}
	return
}

// shotStorageOK reports whether the screenshot folder exists and can be written. The server never creates the folder
// itself (main does that only for the default location): a configured folder on an unmounted drive must not turn into
// an ordinary folder on the SD card.
func (s *Server) ShotStorageOK() bool { return s.shotStorageOK() }

func (s *Server) shotStorageOK() bool {
	if s.ShotDir == "" {
		return false
	}
	if fi, err := os.Stat(s.ShotDir); err != nil || !fi.IsDir() {
		return false
	}
	f, err := os.CreateTemp(s.ShotDir, ".probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

func (s *Server) gameShotDir(gameID int64) string {
	return filepath.Join(s.ShotDir, strconv.FormatInt(gameID, 10))
}

func (s *Server) gameExists(w http.ResponseWriter, id int64) bool {
	var one int
	if err := s.DB.QueryRow("SELECT 1 FROM game WHERE id = ?", id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return false
	} else if err != nil {
		fail(w, err)
		return false
	}
	return true
}

func (s *Server) listShots(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.gameExists(w, id) {
		return
	}
	list, err := s.loadShots(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"screenshots": list, "available": s.shotStorageOK()})
}

func (s *Server) loadShots(gameID int64) ([]Screenshot, error) {
	rows, err := s.DB.Query("SELECT "+shotCols+" FROM screenshot WHERE game_id = ? ORDER BY pos, id", gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Screenshot{}
	for rows.Next() {
		sc, err := scanShot(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sc)
	}
	return list, rows.Err()
}

// shotResult is the outcome of one uploaded file.
type shotResult struct {
	Name       string      `json:"name"`
	Screenshot *Screenshot `json:"screenshot,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// uploadShots takes a multipart body with one or more "file" parts. Each part is streamed to disk (never held in
// memory), checked by content, thumbnailed and recorded; a bad file does not stop the others. An optional
// "modified" part (milliseconds since 1970, the file's last-modified time) before a file is used as its date
// when the picture has no EXIF date.
func (s *Server) uploadShots(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.gameExists(w, id) {
		return
	}
	if !s.shotStorageOK() {
		writeErr(w, http.StatusServiceUnavailable, errStorage, nil)
		return
	}
	perFile, perRequest := s.shotLimits()
	r.Body = http.MaxBytesReader(w, r.Body, perRequest)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected a multipart upload", nil)
		return
	}
	dir := s.gameShotDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("screenshots: %v", err)
		writeErr(w, http.StatusServiceUnavailable, errStorage, nil)
		return
	}
	results := []shotResult{}
	modified := ""
	status := http.StatusOK
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				status = http.StatusRequestEntityTooLarge
				results = append(results, shotResult{Error: fmt.Sprintf("the upload is larger than %d MB", perRequest>>20)})
			} else {
				status = http.StatusBadRequest
			}
			break
		}
		switch part.FormName() {
		case "modified":
			b, _ := io.ReadAll(io.LimitReader(part, 32))
			modified = modifiedToTime(string(b))
		case "file":
			res := s.storeShot(id, dir, part, perFile, modified)
			modified = ""
			results = append(results, res)
		}
	}
	writeJSONStatus(w, status, map[string]any{"results": results})
}

// modifiedToTime turns the client's File.lastModified (milliseconds) into an ISO UTC time, or "".
func modifiedToTime(ms string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(ms), 10, 64)
	if err != nil || n < 631152000000 || n > time.Now().Add(48*time.Hour).UnixMilli() { // before 1990 or in the future: unknown
		return ""
	}
	return time.UnixMilli(n).UTC().Format("2006-01-02T15:04:05Z")
}

// cleanName is the client's file name for messages only; it is never used on disk.
func cleanName(n string) string {
	n = filepath.Base(strings.ReplaceAll(n, `\`, "/"))
	if len(n) > 100 {
		n = n[:100]
	}
	return n
}

func (s *Server) storeShot(gameID int64, dir string, part *multipart.Part, maxFile int64, modified string) shotResult {
	res := shotResult{Name: cleanName(part.FileName())}
	fail := func(msg string) shotResult { res.Error = msg; return res }

	head := make([]byte, shots.SniffLen)
	if _, err := io.ReadFull(part, head); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return fail("not a JPEG, PNG or WebP image")
		}
		return fail("the upload was interrupted")
	}
	ext, ok := shots.Sniff(head)
	if !ok {
		return fail(shots.ErrUnsupported.Error())
	}
	base := shots.NewName()
	final := filepath.Join(dir, base+"."+ext)
	thumbFinal := filepath.Join(dir, shots.ThumbName(base))
	tmp, err := os.CreateTemp(dir, "up-*.tmp")
	if err != nil {
		log.Printf("screenshots: %v", err)
		return fail(errStorage)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	if _, err := tmp.Write(head); err != nil {
		cleanup()
		return fail(errStorage)
	}
	n, err := io.Copy(tmp, io.LimitReader(part, maxFile-int64(len(head))+1))
	n += int64(len(head))
	if err != nil {
		cleanup()
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return fail("the upload is larger than the request limit")
		}
		return fail("the upload was interrupted")
	}
	if n > maxFile {
		cleanup()
		// the rest of this part is skipped by the multipart reader when it moves on
		return fail(fmt.Sprintf("larger than %d MB", maxFile>>20))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fail(errStorage)
	}
	info, err := shots.Inspect(tmpName)
	if err != nil {
		os.Remove(tmpName)
		switch {
		case errors.Is(err, shots.ErrTooLarge):
			return fail("too many pixels (limit 40 megapixels for JPEG, 12 for PNG and WebP)")
		case errors.Is(err, shots.ErrUnsupported):
			return fail(err.Error())
		}
		return fail("could not read that image")
	}
	if err := shots.WriteThumb(tmpName, thumbFinal, info.Orientation); err != nil {
		os.Remove(tmpName)
		return fail("could not read that image")
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		os.Remove(thumbFinal)
		return fail(errStorage)
	}
	taken := info.Taken
	if taken == "" {
		taken = modified
	}
	var takenArg any
	if taken != "" {
		takenArg = taken
	}
	row := s.DB.QueryRow(`INSERT INTO screenshot(game_id, file, thumb, width, height, bytes, taken_at, pos)
		VALUES (?, ?, ?, ?, ?, ?, ?, (SELECT coalesce(max(pos), 0) + 1 FROM screenshot WHERE game_id = ?))
		RETURNING `+shotCols, gameID, base+"."+ext, shots.ThumbName(base), info.Width, info.Height, n, takenArg, gameID)
	sc, err := scanShot(row)
	if err != nil { // for example the game was deleted meanwhile
		os.Remove(final)
		os.Remove(thumbFinal)
		log.Printf("screenshots: insert: %v", err)
		return fail("could not save the screenshot")
	}
	res.Screenshot = &sc
	return res
}

func (s *Server) patchShot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Caption *string `json:"caption"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Caption == nil || len([]rune(*in.Caption)) > maxCaption {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	c := strings.TrimSpace(*in.Caption)
	var capArg any
	if c != "" {
		capArg = c
	}
	row := s.DB.QueryRow("UPDATE screenshot SET caption = ? WHERE id = ? RETURNING "+shotCols, capArg, id)
	sc, err := scanShot(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, sc)
}

// orderShots takes the ids of a game's screenshots in the wanted order; it must name every one of them.
func (s *Server) orderShots(w http.ResponseWriter, r *http.Request) {
	gid, ok := pathID(w, r)
	if !ok || !s.gameExists(w, gid) {
		return
	}
	var ids []int64
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeErr(w, http.StatusBadRequest, "expected a list of screenshot ids", nil)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	have := map[int64]bool{}
	rows, err := tx.Query("SELECT id FROM screenshot WHERE game_id = ?", gid)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		have[id] = true
	}
	rows.Close()
	if len(ids) != len(have) {
		writeErr(w, http.StatusBadRequest, "the list must name every screenshot of the game once", nil)
		return
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !have[id] || seen[id] {
			writeErr(w, http.StatusBadRequest, "the list must name every screenshot of the game once", nil)
			return
		}
		seen[id] = true
	}
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE screenshot SET pos = ? WHERE id = ?", i+1, id); err != nil {
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	list, err := s.loadShots(gid)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"screenshots": list})
}

func (s *Server) deleteShot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var gid int64
	var file, thumb string
	err := s.DB.QueryRow("DELETE FROM screenshot WHERE id = ? RETURNING game_id, file, thumb", id).Scan(&gid, &file, &thumb)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if s.ShotDir != "" {
		dir := s.gameShotDir(gid)
		os.Remove(filepath.Join(dir, filepath.Base(file)))
		os.Remove(filepath.Join(dir, filepath.Base(thumb)))
	}
	w.WriteHeader(http.StatusNoContent)
}

var shotTypes = map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}

// serveShot serves /screenshots/<game id>/<file>. Only names this server generated are served (a strict pattern),
// so no path trick reaches anything else. ?download=1 asks the browser to save the file.
func (s *Server) serveShot(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	name := r.PathValue("name")
	if err != nil || gid <= 0 || !shots.NameRE.MatchString(name) || s.ShotDir == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.gameShotDir(gid), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", shotTypes[filepath.Ext(name)])
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=31536000, immutable") // the name is random and the content never changes
	if r.URL.Query().Get("download") != "" {
		h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	http.ServeContent(w, r, name, fi.ModTime(), f)
}
