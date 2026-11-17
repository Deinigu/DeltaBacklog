package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/expansions"
)

// Expansion is the compact read model of one IGDB expansion of a game, sent inside the game (GET /api/games), so the
// Library badge and filters, Stats, Upcoming and Needs attention work in the browser without extra calls. Status and
// ownership are null while the owner has not set them ("Not tracked"). Empty optional values are left out. An expansion
// that is also a game of the library (its own card) is not sent.
type Expansion struct {
	ID        int64    `json:"id"`
	GameID    int64    `json:"game_id"`
	Title     string   `json:"title"`
	Year      *int     `json:"year"`
	Date      *int64   `json:"date,omitempty"`    // release date, unix seconds (UTC); null = TBA
	Cover     string   `json:"cover,omitempty"`   // poster file under /covers/ (x<igdb id>.jpg) once downloaded
	CoverV    string   `json:"cover_v,omitempty"` // IGDB's image id: the cache key of the poster
	Status    *string  `json:"status"`            // status code, the same list as games
	Ownership *string  `json:"ownership"`
	Years     []int    `json:"years"`
	Periods   []string `json:"periods"`
	Notes     string   `json:"notes,omitempty"`
	Hidden    bool     `json:"hidden,omitempty"`
	UpdatedAt string   `json:"updated_at"`

	TTBHastily    *int    `json:"ttb_hastily,omitempty"`
	TTBNormally   *int    `json:"ttb_normally,omitempty"`
	TTBCompletely *int    `json:"ttb_completely,omitempty"`
	TTBSource     *string `json:"ttb_source,omitempty"`
}

// attachExpansions fills Expansions and ExpansionsFetched of the games (only one game's when `only` is not 0).
func attachExpansions(d *sql.DB, games []Game, only int64) error {
	// an expansion the library also has as its own game is left out: its card is its record (it would be counted twice)
	where := " WHERE NOT " + expansions.IsGameSQL
	var args []any
	if only != 0 {
		args = []any{only}
		where += " AND e.game_id = ?"
	}
	rows, err := d.Query(`SELECT e.id, e.game_id, e.title, e.release_year, e.release_date, e.cover_file, e.cover_image_id, s.code, o.name, e.notes, e.hidden,
			e.time_to_beat_hastily, e.time_to_beat_normally, e.time_to_beat_completely, e.time_to_beat_src, e.updated_at
		FROM expansion e LEFT JOIN status s ON s.id = e.status_id LEFT JOIN ownership o ON o.id = e.ownership_id`+where+`
		ORDER BY e.release_date IS NULL, e.release_date, e.title`, args...)
	if err != nil {
		return err
	}
	byGame := map[int64][]*Expansion{}
	byID := map[int64]*Expansion{}
	for rows.Next() {
		var x Expansion
		var date, cover, coverV sql.NullString
		if err := rows.Scan(&x.ID, &x.GameID, &x.Title, &x.Year, &date, &cover, &coverV, &x.Status, &x.Ownership, &x.Notes, &x.Hidden,
			&x.TTBHastily, &x.TTBNormally, &x.TTBCompletely, &x.TTBSource, &x.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		if t, err := time.Parse("2006-01-02", date.String); date.Valid && err == nil {
			u := t.Unix()
			x.Date = &u
		}
		x.Cover, x.CoverV = cover.String, coverV.String
		x.Years, x.Periods = []int{}, []string{}
		byGame[x.GameID] = append(byGame[x.GameID], &x)
		byID[x.ID] = &x
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	prows, err := d.Query(`SELECT p.expansion_id, p.year, p.period FROM expansion_play p JOIN expansion e ON e.id = p.expansion_id`+where+` ORDER BY p.year, p.period`, args...)
	if err != nil {
		return err
	}
	for prows.Next() {
		var id int64
		var year sql.NullInt64
		var period sql.NullString
		if err := prows.Scan(&id, &year, &period); err != nil {
			prows.Close()
			return err
		}
		if x := byID[id]; x != nil {
			if year.Valid {
				x.Years = append(x.Years, int(year.Int64))
			} else {
				x.Periods = append(x.Periods, period.String)
			}
		}
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return err
	}
	fq, fargs := `SELECT game_id, fetched_at FROM expansion_fetch`, []any(nil)
	if only != 0 {
		fq, fargs = fq+` WHERE game_id = ?`, args
	}
	frows, err := d.Query(fq, fargs...)
	if err != nil {
		return err
	}
	defer frows.Close()
	fetched := map[int64]string{}
	for frows.Next() {
		var id int64
		var at string
		if err := frows.Scan(&id, &at); err != nil {
			return err
		}
		fetched[id] = at
	}
	if err := frows.Err(); err != nil {
		return err
	}
	for i := range games {
		games[i].Expansions = []Expansion{}
		for _, x := range byGame[games[i].ID] {
			games[i].Expansions = append(games[i].Expansions, *x)
		}
		games[i].ExpansionsFetched = fetched[games[i].ID]
	}
	return nil
}

// ---------------- PATCH /api/expansions/{id} ----------------

// expansionPatch is the writable subset of an expansion; the same conventions as gamePatch (absent = unchanged). Status
// and ownership clear with "" (back to "Not tracked" / no ownership).
type expansionPatch struct {
	Status        *string    `json:"status"`
	Ownership     *string    `json:"ownership"`
	Notes         *string    `json:"notes"`
	Years         *[]int     `json:"years"`
	Periods       *[]string  `json:"periods"`
	Hidden        *bool      `json:"hidden"`
	TTBHastily    optMinutes `json:"ttb_hastily"`
	TTBNormally   optMinutes `json:"ttb_normally"`
	TTBCompletely optMinutes `json:"ttb_completely"`
	TTBSource     *string    `json:"ttb_source"`
}

func (in *expansionPatch) validate() error {
	// the checks shared with games: lengths, years, periods, minutes, the time source
	g := gamePatch{Notes: in.Notes, Ownership: in.Ownership, Years: in.Years, Periods: in.Periods,
		TTBHastily: in.TTBHastily, TTBNormally: in.TTBNormally, TTBCompletely: in.TTBCompletely, TTBSource: in.TTBSource}
	return g.validate()
}

func (s *Server) patchExpansion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in expansionPatch
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
	var gameID int64
	var curStatus sql.NullInt64
	// an expansion that is also a game of the library is not listed (see attachExpansions): 404 like an unknown id
	if err := tx.QueryRow("SELECT e.game_id, e.status_id FROM expansion e WHERE e.id = ? AND NOT "+expansions.IsGameSQL, id).Scan(&gameID, &curStatus); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	var statusID any // what the patch sets, when it sets a status
	setStatus := false
	if in.Status != nil {
		setStatus = true
		if *in.Status != "" {
			var sid int64
			if err := tx.QueryRow("SELECT id FROM status WHERE code = ?", *in.Status).Scan(&sid); err != nil {
				writeErr(w, http.StatusBadRequest, "unknown status", nil)
				return
			}
			statusID = sid
		}
	}
	// Field edits come first: an explicit list of years must not wipe the year the status change records below.
	if err := applyExpansionEdits(tx, id, &in); err != nil {
		fail(w, err)
		return
	}
	if setStatus {
		changed := (statusID == nil) == curStatus.Valid || (statusID != nil && statusID.(int64) != curStatus.Int64)
		if changed {
			if statusID != nil && playingStatuses[*in.Status] { // the game rule: starting or finishing it counts as played this year
				if _, err := tx.Exec(`INSERT OR IGNORE INTO expansion_play(expansion_id, year) VALUES (?, CAST(strftime('%Y', 'now') AS INTEGER))`, id); err != nil {
					fail(w, err)
					return
				}
			}
			if _, err := tx.Exec("UPDATE expansion SET status_id = ? WHERE id = ?", statusID, id); err != nil {
				fail(w, err)
				return
			}
		}
	}
	if _, err := tx.Exec(`UPDATE expansion SET updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`, id); err != nil {
		fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.cache.invalidate()
	g, err := LoadGame(s.DB, gameID)
	if err != nil {
		fail(w, err)
		return
	}
	for _, x := range g.Expansions {
		if x.ID == id {
			writeJSON(w, x)
			return
		}
	}
	fail(w, errors.New("expansion vanished after its update"))
}

func applyExpansionEdits(tx *sql.Tx, id int64, in *expansionPatch) error {
	if in.Notes != nil {
		if _, err := tx.Exec("UPDATE expansion SET notes = ? WHERE id = ?", *in.Notes, id); err != nil {
			return err
		}
	}
	if in.Ownership != nil {
		ref, err := nullableLookup(tx, "ownership", *in.Ownership)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE expansion SET ownership_id = ? WHERE id = ?", ref, id); err != nil {
			return err
		}
	}
	if in.Hidden != nil {
		if _, err := tx.Exec("UPDATE expansion SET hidden = ? WHERE id = ?", b2i(*in.Hidden), id); err != nil {
			return err
		}
	}
	if err := applyExpansionTTB(tx, id, in); err != nil {
		return err
	}
	if in.Years != nil {
		if _, err := tx.Exec("DELETE FROM expansion_play WHERE expansion_id = ? AND year IS NOT NULL", id); err != nil {
			return err
		}
		for _, y := range *in.Years {
			if _, err := tx.Exec("INSERT OR IGNORE INTO expansion_play(expansion_id, year) VALUES (?, ?)", id, y); err != nil {
				return err
			}
		}
	}
	if in.Periods != nil {
		if _, err := tx.Exec("DELETE FROM expansion_play WHERE expansion_id = ? AND period IS NOT NULL", id); err != nil {
			return err
		}
		for _, p := range *in.Periods {
			if _, err := tx.Exec("INSERT OR IGNORE INTO expansion_play(expansion_id, period) VALUES (?, ?)", id, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyExpansionTTB is applyTTB for an expansion: the three times share one source.
func applyExpansionTTB(tx *sql.Tx, id int64, in *expansionPatch) error {
	if !in.TTBHastily.Set && !in.TTBNormally.Set && !in.TTBCompletely.Set {
		return nil
	}
	var cur [3]sql.NullInt64
	if err := tx.QueryRow(`SELECT time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely FROM expansion WHERE id = ?`, id).
		Scan(&cur[0], &cur[1], &cur[2]); err != nil {
		return err
	}
	var vals [3]any
	any3 := false
	for i, o := range []optMinutes{in.TTBHastily, in.TTBNormally, in.TTBCompletely} {
		switch {
		case o.Set && o.V != nil:
			vals[i] = *o.V
		case !o.Set && cur[i].Valid:
			vals[i] = cur[i].Int64
		}
		any3 = any3 || vals[i] != nil
	}
	var src any
	if any3 {
		src = "manual"
		if in.TTBSource != nil {
			src = *in.TTBSource
		}
	}
	_, err := tx.Exec(`UPDATE expansion SET time_to_beat_hastily = ?, time_to_beat_normally = ?, time_to_beat_completely = ?, time_to_beat_src = ? WHERE id = ?`,
		vals[0], vals[1], vals[2], src, id)
	return err
}

// ---------------- reading from IGDB ----------------

// expansionSource is the IGDB client used to read expansions: the server's Expansions field (tests) or Meta.
func (s *Server) expansionSource() expansions.Source {
	if s.Expansions != nil {
		return s.Expansions
	}
	if src, ok := s.Meta.(expansions.Source); ok {
		return src
	}
	return nil
}

func (s *Server) expansionOptions() expansions.Options {
	return expansions.Options{CoverDir: s.CoverDir, Fetch: s.ExpansionPosterFetch}
}

// refreshExpansions (POST /api/games/{id}/expansions/refresh) reads one game's expansions from IGDB now and answers the
// updated game. Body (optional): {"only_if_stale": true} skips IGDB when the game was read within 30 days (the drawer
// sends it when it opens). 404 unknown game, 409 not linked to IGDB, 503 IGDB not set up, 502 IGDB failed.
func (s *Server) refreshExpansions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		OnlyIfStale bool `json:"only_if_stale"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	var igdb sql.NullInt64
	if err := s.DB.QueryRow("SELECT igdb_id FROM game WHERE id = ?", id).Scan(&igdb); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if !igdb.Valid {
		writeErr(w, http.StatusConflict, "the game is not linked to IGDB", nil)
		return
	}
	if !(in.OnlyIfStale && expansions.IsFresh(s.DB, id, time.Now())) {
		src := s.expansionSource()
		if src == nil || (s.Expansions == nil && !s.igdbConfigured()) {
			writeErr(w, http.StatusServiceUnavailable, "IGDB is not set up (keys are missing from .env)", nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := expansions.Fetch(ctx, s.DB, src, []expansions.Target{{GameID: id, IGDBID: igdb.Int64}}, s.expansionOptions()); err != nil {
			log.Printf("expansions of game %d: %v", id, err)
			writeErr(w, http.StatusBadGateway, "IGDB could not be reached", nil)
			return
		}
		s.cache.invalidate()
	}
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, g)
}

// expJob is the state of the one background run that reads expansions for many games (the Expansions page polls it). The
// Steam import uses it too: it first reads the games never read, then marks the owned ones.
type expJob struct {
	seriesJob
	cancel context.CancelFunc
	kind   string // "find" or "steam"
	found  int
	games  int
}

type expStatus struct {
	seriesStatus
	Kind  string `json:"kind,omitempty"`
	Found int    `json:"found"`
	Games int    `json:"games"`
}

func (j *expJob) expStatus() expStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return expStatus{seriesStatus{Running: j.running, Done: j.done, Total: j.total, Finished: j.finished, Report: j.report, Failed: j.failed}, j.kind, j.found, j.games}
}

func (s *Server) expansionSyncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.expJob.expStatus())
}

func (s *Server) expansionSyncCancel(w http.ResponseWriter, r *http.Request) {
	s.expJob.mu.Lock()
	if s.expJob.cancel != nil {
		s.expJob.cancel()
	}
	s.expJob.mu.Unlock()
	writeJSON(w, s.expJob.expStatus())
}

// startExpansionJob begins the background run (one at a time) over the games that are due; `after` (may be nil) runs when the
// reading ended without being stopped, and returns the sentence to add to the report. It answers 202 with the status, or
// the current status when a run is already going.
func (s *Server) startExpansionJob(w http.ResponseWriter, kind string, neverOnly bool, after func() (string, error)) {
	j := &s.expJob
	src := s.expansionSource()
	configured := src != nil && (s.Expansions != nil || s.igdbConfigured())
	if !configured && after == nil {
		writeErr(w, http.StatusServiceUnavailable, "IGDB is not set up (keys are missing from .env)", nil)
		return
	}
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSONStatus(w, http.StatusConflict, j.expStatus())
		return
	}
	var todo []expansions.Target
	if configured {
		var err error
		if todo, err = expansions.Due(s.DB, time.Now(), neverOnly); err != nil {
			j.mu.Unlock()
			fail(w, err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	j.running, j.done, j.total, j.report, j.failed = true, 0, len(todo), "", false
	j.kind, j.found, j.games, j.cancel = kind, 0, 0, cancel
	j.mu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		defer cancel()
		s.runExpansionJob(ctx, src, todo, after)
	}()
	writeJSONStatus(w, http.StatusAccepted, j.expStatus())
}

// nothingDue is the report of a "Find expansions" run with no game to read (instead of "Found 0 expansions for 0 games.").
const nothingDue = "Nothing to read: every game linked to IGDB was read in the last 30 days."

func (s *Server) runExpansionJob(ctx context.Context, src expansions.Source, todo []expansions.Target, after func() (string, error)) {
	j := &s.expJob
	o := s.expansionOptions()
	o.AllCovers = true // the background run catches up on every missing poster (a drawer's refresh only gets its game's)
	o.Progress = func(done, total int) { j.mu.Lock(); j.done, j.total = done, total; j.mu.Unlock() }
	var rep expansions.Report
	var err error
	if len(todo) > 0 {
		rep, err = expansions.Fetch(ctx, s.DB, src, todo, o)
	}
	if e := expansions.SweepCovers(s.DB, s.CoverDir); e != nil {
		log.Printf("expansions: sweep covers: %v", e)
	}
	text, failed := rep.Text(), false
	if len(todo) == 0 {
		text = nothingDue
	}
	switch {
	case ctx.Err() != nil:
		text = "Stopped. " + text
	case err != nil:
		log.Printf("expansions sync: %v", err)
		text, failed = "IGDB could not be reached. Stopped. "+text, true
	}
	if after != nil {
		if ctx.Err() != nil || err != nil {
			text += " Nothing was marked as owned."
		} else if more, aerr := after(); aerr != nil {
			log.Printf("expansions import: %v", aerr)
			text, failed = "The import failed.", true
		} else {
			text = more
			if rep.Games > 0 {
				text = rep.Text() + " " + more
			}
		}
	}
	s.cache.invalidate()
	j.mu.Lock()
	j.running, j.cancel = false, nil
	j.found, j.games = rep.Found, rep.WithExpansion
	j.finished = time.Now().UTC().Format(time.RFC3339)
	j.report, j.failed = text, failed
	j.mu.Unlock()
}

// expansionSyncStart (POST /api/expansions/sync) reads the expansions of every IGDB-linked game that was not read within 30
// days, in the background. 202 with the job status; 409 (with the status) while a run is going; 503 without IGDB keys.
func (s *Server) expansionSyncStart(w http.ResponseWriter, r *http.Request) {
	s.startExpansionJob(w, "find", false, nil)
}

// steamImport (POST /api/expansions/steam-import, multipart form field "file") takes Steam's userdata JSON, reads only its
// rgOwnedApps, and in the background first reads the expansions of the games never read, then gives the ownership "Owned"
// to the expansions whose Steam app is in the list and that have none. The file is never stored or logged. 400 not a Steam
// file, 413 over 5 MB, 409 a run is going; otherwise the same 202 and status as the sync (the report holds the result).
func (s *Server) steamImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, expansions.MaxSteamFile+(256<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected a file upload", nil)
		return
	}
	var owned []int64
	found := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.steamUploadError(w, err)
			return
		}
		if part.FormName() != "file" || found {
			part.Close()
			continue
		}
		found = true
		owned, err = readOwned(part)
		if err != nil {
			s.steamUploadError(w, err)
			return
		}
	}
	if !found {
		writeErr(w, http.StatusBadRequest, "no file was sent", nil)
		return
	}
	s.startExpansionJob(w, "steam", true, func() (string, error) {
		res, err := expansions.ImportOwned(s.DB, owned)
		return res.Text(), err
	})
}

func readOwned(part *multipart.Part) ([]int64, error) {
	defer part.Close()
	return expansions.ParseOwned(part)
}

func (s *Server) steamUploadError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, expansions.ErrTooBig) || errors.As(err, &tooBig):
		writeErr(w, http.StatusRequestEntityTooLarge, "the file is over 5 MB: it is not a Steam userdata file", nil)
	case errors.Is(err, expansions.ErrNotSteamFile):
		writeErr(w, http.StatusBadRequest, "this is not the Steam userdata file (it must have an rgOwnedApps list)", nil)
	default:
		writeErr(w, http.StatusBadRequest, "the upload could not be read", nil)
	}
}

// dropUntrackedExpansions is called inside the transaction that changes a game's IGDB link.
func dropUntrackedExpansions(tx *sql.Tx, gameID int64) error {
	return expansions.DropUntracked(tx, gameID)
}
