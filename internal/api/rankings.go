package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// Rankings: any number of custom tier lists (TierMaker style). A ranking has its own tiers (a label of up to 80
// characters and a colour) and any set of games; a game is in a tier or in the ranking's pool of unranked games
// (tier_id NULL). Positions (pos) are only an order: gaps are fine, ties are broken by game id.

const (
	maxRankings       = 200
	maxRankingTiers   = 30
	maxRankingEntries = 2000
	maxNameLen        = 80 // characters, for ranking names and tier labels
	maxDescLen        = 500
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// presetTiers is the classic S to F list; the colours are the ones of the old per-year tier list.
var presetTiers = []struct{ Label, Color string }{
	{"S", "#ff7f7f"}, {"A", "#ffbf7f"}, {"B", "#ffdf7f"}, {"C", "#bfff7f"}, {"D", "#7fbfff"}, {"F", "#bf7fff"},
}

type RankingTier struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Color string `json:"color"`
	Pos   int    `json:"pos"`
}

// RankingEntry places a game; TierID nil is the pool. In a response the entries come tier by tier (in tier order),
// then the pool, each in position order.
type RankingEntry struct {
	GameID int64  `json:"game_id"`
	TierID *int64 `json:"tier_id"`
	Pos    int    `json:"pos"`
}

type Ranking struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Tiers       []RankingTier  `json:"tiers"`
	Entries     []RankingEntry `json:"entries"`
	// PostersPending is how many games of the ranking are still waiting for their poster in the background queue.
	PostersPending int `json:"posters_pending"`
}

// RankingSummary is one row of the list page.
type RankingSummary struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	Pos         int        `json:"pos"`
	UpdatedAt   string     `json:"updated_at"`
	Games       int        `json:"games"`
	Ranked      int        `json:"ranked"`
	Tiers       []tierLook `json:"tiers"`
	Preview     []int64    `json:"preview"` // the first ranked games, best tier first: the list page shows their posters
}

type tierLook struct {
	Label string `json:"label"`
	Color string `json:"color"`
}

var errBadRanking = errors.New("invalid ranking")

func cleanText(p *string, max int) (string, bool) {
	if p == nil {
		return "", false
	}
	n := strings.TrimSpace(*p)
	if n == "" || utf8.RuneCountInString(n) > max {
		return "", false
	}
	return n, true
}

// readJSON decodes a request body of at most limit bytes.
func readJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body", nil)
		return false
	}
	return true
}

func bad(w http.ResponseWriter, msg string) { writeErr(w, http.StatusBadRequest, msg, nil) }

func notFound(w http.ResponseWriter) { writeErr(w, http.StatusNotFound, "not found", nil) }

func pathInt(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// ---------------- posters ----------------

// posterQ is the background poster worker of this server, created on first use.
func (s *Server) posterQ() *posters.Queue {
	s.posterOnce.Do(func() {
		m := &posters.Maker{DB: s.DB, Dir: s.CoverDir, Allowed: s.coverHostAllowed, Fetch: s.PosterFetch}
		if s.Meta != nil {
			m.Src = s.Meta
		}
		s.posters = &posters.Queue{M: m, Pause: s.PosterPause}
	})
	return s.posters
}

// ---------------- loading ----------------

func loadRanking(d *sql.DB, id int64) (Ranking, error) {
	rk := Ranking{ID: id, Tiers: []RankingTier{}, Entries: []RankingEntry{}}
	err := d.QueryRow(`SELECT name, description, created_at, updated_at FROM ranking WHERE id = ?`, id).
		Scan(&rk.Name, &rk.Description, &rk.CreatedAt, &rk.UpdatedAt)
	if err != nil {
		return rk, err
	}
	rows, err := d.Query(`SELECT id, label, color, pos FROM ranking_tier WHERE ranking_id = ? ORDER BY pos, id`, id)
	if err != nil {
		return rk, err
	}
	for rows.Next() {
		var t RankingTier
		if err := rows.Scan(&t.ID, &t.Label, &t.Color, &t.Pos); err != nil {
			rows.Close()
			return rk, err
		}
		rk.Tiers = append(rk.Tiers, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rk, err
	}
	erows, err := d.Query(`SELECT e.game_id, e.tier_id, e.pos FROM ranking_entry e
		LEFT JOIN ranking_tier t ON t.id = e.tier_id
		WHERE e.ranking_id = ?
		ORDER BY e.tier_id IS NULL, t.pos, e.pos, e.game_id`, id)
	if err != nil {
		return rk, err
	}
	defer erows.Close()
	for erows.Next() {
		var e RankingEntry
		if err := erows.Scan(&e.GameID, &e.TierID, &e.Pos); err != nil {
			return rk, err
		}
		rk.Entries = append(rk.Entries, e)
	}
	return rk, erows.Err()
}

// withPosters queues the posters the ranking's games lack and reports how many are still on their way.
func (s *Server) withPosters(rk *Ranking) {
	q := s.posterQ()
	var missing []int64
	rows, err := s.DB.Query(`SELECT g.id FROM ranking_entry e JOIN game g ON g.id = e.game_id WHERE e.ranking_id = ? AND g.poster_file IS NULL`, rk.ID)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			missing = append(missing, id)
		}
	}
	rows.Close()
	q.Add(missing...)
	rk.PostersPending = q.Pending(missing)
}

func (s *Server) respondRanking(w http.ResponseWriter, code int, id int64) {
	rk, err := loadRanking(s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.withPosters(&rk)
	writeJSONStatus(w, code, rk)
}

func (s *Server) listRankings(w http.ResponseWriter, r *http.Request) {
	list, err := loadRankingList(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, list)
}

func loadRankingList(d *sql.DB) ([]RankingSummary, error) {
	rows, err := d.Query(`SELECT r.id, r.name, r.description, r.pos, r.updated_at,
			(SELECT count(*) FROM ranking_entry e WHERE e.ranking_id = r.id),
			(SELECT count(*) FROM ranking_entry e WHERE e.ranking_id = r.id AND e.tier_id IS NOT NULL)
		FROM ranking r ORDER BY r.pos, r.id`)
	if err != nil {
		return nil, err
	}
	out := []RankingSummary{}
	for rows.Next() {
		var s RankingSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Pos, &s.UpdatedAt, &s.Games, &s.Ranked); err != nil {
			rows.Close()
			return nil, err
		}
		s.Tiers, s.Preview = []tierLook{}, []int64{}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID := map[int64]*RankingSummary{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	trows, err := d.Query(`SELECT ranking_id, label, color FROM ranking_tier ORDER BY ranking_id, pos, id`)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var id int64
		var t tierLook
		if err := trows.Scan(&id, &t.Label, &t.Color); err != nil {
			trows.Close()
			return nil, err
		}
		if s := byID[id]; s != nil {
			s.Tiers = append(s.Tiers, t)
		}
	}
	trows.Close()
	prows, err := d.Query(`SELECT e.ranking_id, e.game_id FROM ranking_entry e JOIN ranking_tier t ON t.id = e.tier_id
		ORDER BY e.ranking_id, t.pos, e.pos, e.game_id`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var rid, gid int64
		if err := prows.Scan(&rid, &gid); err != nil {
			return nil, err
		}
		if s := byID[rid]; s != nil && len(s.Preview) < 6 {
			s.Preview = append(s.Preview, gid)
		}
	}
	return out, prows.Err()
}

func (s *Server) getRanking(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	s.respondRanking(w, http.StatusOK, id)
}

// ---------------- rankings ----------------

type rankingInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Preset      string  `json:"preset"`   // create: "sabcdf" (default) or "empty"
	GameIDs     []int64 `json:"game_ids"` // create: games to put in the pool
}

func (in *rankingInput) validate(creating bool) error {
	if in.Name == nil && creating {
		return errBadRanking
	}
	if in.Name != nil {
		if _, ok := cleanText(in.Name, maxNameLen); !ok {
			return errBadRanking
		}
	}
	if in.Description != nil && utf8.RuneCountInString(*in.Description) > maxDescLen {
		return errBadRanking
	}
	if creating && in.Preset != "" && in.Preset != "sabcdf" && in.Preset != "empty" {
		return errBadRanking
	}
	if len(in.GameIDs) > maxRankingEntries {
		return errBadRanking
	}
	return nil
}

func nullDesc(p *string) any {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	return strings.TrimSpace(*p)
}

func countRows(x interface {
	QueryRow(string, ...any) *sql.Row
}, q string, args ...any) (int, error) {
	var n int
	err := x.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (s *Server) createRanking(w http.ResponseWriter, r *http.Request) {
	var in rankingInput
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	if in.validate(true) != nil {
		bad(w, "invalid ranking")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if n, err := countRows(tx, `SELECT count(*) FROM ranking`); err != nil {
		fail(w, err)
		return
	} else if n >= maxRankings {
		bad(w, "too many rankings")
		return
	}
	name, _ := cleanText(in.Name, maxNameLen)
	res, err := tx.Exec(`INSERT INTO ranking(name, description, pos) VALUES (?, ?, (SELECT coalesce(max(pos), 0) + 1 FROM ranking))`, name, nullDesc(in.Description))
	if err != nil {
		fail(w, err)
		return
	}
	id, _ := res.LastInsertId()
	if in.Preset != "empty" {
		for i, t := range presetTiers {
			if _, err := tx.Exec(`INSERT INTO ranking_tier(ranking_id, label, color, pos) VALUES (?, ?, ?, ?)`, id, t.Label, t.Color, i); err != nil {
				fail(w, err)
				return
			}
		}
	}
	if _, bad := addEntries(tx, id, in.GameIDs, nil); bad != "" {
		writeErr(w, http.StatusBadRequest, bad, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.respondRanking(w, http.StatusCreated, id)
}

func (s *Server) patchRanking(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in rankingInput
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	if in.validate(false) != nil || (in.Name == nil && in.Description == nil) {
		bad(w, "invalid ranking")
		return
	}
	var one int
	if err := s.DB.QueryRow(`SELECT 1 FROM ranking WHERE id = ?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		notFound(w)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if in.Name != nil {
		name, _ := cleanText(in.Name, maxNameLen)
		if _, err := s.DB.Exec(`UPDATE ranking SET name = ? WHERE id = ?`, name, id); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Description != nil {
		if _, err := s.DB.Exec(`UPDATE ranking SET description = ? WHERE id = ?`, nullDesc(in.Description), id); err != nil {
			fail(w, err)
			return
		}
	}
	touch(s.DB, id)
	s.respondRanking(w, http.StatusOK, id)
}

func touch(x interface {
	Exec(string, ...any) (sql.Result, error)
}, id int64) {
	x.Exec(`UPDATE ranking SET updated_at = ? WHERE id = ?`, now(), id)
}

func (s *Server) deleteRanking(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	res, err := s.DB.Exec(`DELETE FROM ranking WHERE id = ?`, id)
	if err != nil {
		fail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		notFound(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) duplicateRanking(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in rankingInput
	if r.ContentLength != 0 && !readJSON(w, r, &in, 4<<10) {
		return
	}
	if in.validate(false) != nil {
		bad(w, "invalid ranking")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	var name string
	var desc *string
	if err := tx.QueryRow(`SELECT name, description FROM ranking WHERE id = ?`, id).Scan(&name, &desc); errors.Is(err, sql.ErrNoRows) {
		notFound(w)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if n, err := countRows(tx, `SELECT count(*) FROM ranking`); err != nil {
		fail(w, err)
		return
	} else if n >= maxRankings {
		bad(w, "too many rankings")
		return
	}
	newName := name + " (copy)"
	if utf8.RuneCountInString(newName) > maxNameLen {
		newName = string([]rune(name)[:maxNameLen-7]) + " (copy)"
	}
	if in.Name != nil {
		newName, _ = cleanText(in.Name, maxNameLen)
	}
	res, err := tx.Exec(`INSERT INTO ranking(name, description, pos) VALUES (?, ?, (SELECT coalesce(max(pos), 0) + 1 FROM ranking))`, newName, desc)
	if err != nil {
		fail(w, err)
		return
	}
	nid, _ := res.LastInsertId()
	trows, err := tx.Query(`SELECT id, label, color, pos FROM ranking_tier WHERE ranking_id = ? ORDER BY pos, id`, id)
	if err != nil {
		fail(w, err)
		return
	}
	var tiers []RankingTier
	for trows.Next() {
		var t RankingTier
		if err := trows.Scan(&t.ID, &t.Label, &t.Color, &t.Pos); err != nil {
			trows.Close()
			fail(w, err)
			return
		}
		tiers = append(tiers, t)
	}
	trows.Close()
	for _, t := range tiers {
		res, err := tx.Exec(`INSERT INTO ranking_tier(ranking_id, label, color, pos) VALUES (?, ?, ?, ?)`, nid, t.Label, t.Color, t.Pos)
		if err != nil {
			fail(w, err)
			return
		}
		newTier, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO ranking_entry(ranking_id, game_id, tier_id, pos) SELECT ?, game_id, ?, pos FROM ranking_entry WHERE ranking_id = ? AND tier_id = ?`, nid, newTier, id, t.ID); err != nil {
			fail(w, err)
			return
		}
	}
	if _, err := tx.Exec(`INSERT INTO ranking_entry(ranking_id, game_id, tier_id, pos) SELECT ?, game_id, NULL, pos FROM ranking_entry WHERE ranking_id = ? AND tier_id IS NULL`, nid, id); err != nil {
		fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.respondRanking(w, http.StatusCreated, nid)
}

func (s *Server) reorderRankings(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	if !readJSON(w, r, &ids, 16<<10) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !samePermutation(tx, `SELECT id FROM ranking`, ids) {
		bad(w, "the list must name every ranking once")
		return
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE ranking SET pos = ? WHERE id = ?`, i+1, id); err != nil {
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.listRankings(w, r)
}

// samePermutation reports whether ids names exactly the ids the query returns, each once.
func samePermutation(tx *sql.Tx, query string, ids []int64, args ...any) bool {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return false
	}
	have := map[int64]bool{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			have[id] = true
		}
	}
	rows.Close()
	if len(ids) != len(have) {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !have[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// ---------------- tiers ----------------

type tierInput struct {
	Label *string `json:"label"`
	Color *string `json:"color"`
	Index *int    `json:"index"` // create: where the tier goes (default: last)
}

func (in *tierInput) validate(creating bool) error {
	if creating && (in.Label == nil || in.Color == nil) {
		return errBadRanking
	}
	if in.Label != nil {
		if _, ok := cleanText(in.Label, maxNameLen); !ok {
			return errBadRanking
		}
	}
	if in.Color != nil && !hexColor.MatchString(*in.Color) {
		return errBadRanking
	}
	return nil
}

// tierOf reports whether the tier belongs to the ranking.
func tierOf(x interface {
	QueryRow(string, ...any) *sql.Row
}, rid, tid int64) bool {
	var one int
	return x.QueryRow(`SELECT 1 FROM ranking_tier WHERE id = ? AND ranking_id = ?`, tid, rid).Scan(&one) == nil
}

func rankingExists(x interface {
	QueryRow(string, ...any) *sql.Row
}, id int64) bool {
	var one int
	return x.QueryRow(`SELECT 1 FROM ranking WHERE id = ?`, id).Scan(&one) == nil
}

func loadTier(d *sql.DB, tid int64) (RankingTier, error) {
	var t RankingTier
	err := d.QueryRow(`SELECT id, label, color, pos FROM ranking_tier WHERE id = ?`, tid).Scan(&t.ID, &t.Label, &t.Color, &t.Pos)
	return t, err
}

// renumberTiers rewrites the positions of a ranking's tiers as 0, 1, 2... in the order given.
func renumberTiers(tx *sql.Tx, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE ranking_tier SET pos = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

func tierIDs(tx *sql.Tx, rid int64) ([]int64, error) {
	rows, err := tx.Query(`SELECT id FROM ranking_tier WHERE ranking_id = ? ORDER BY pos, id`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Server) addTier(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in tierInput
	if !readJSON(w, r, &in, 4<<10) {
		return
	}
	if in.validate(true) != nil {
		bad(w, "invalid tier")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !rankingExists(tx, rid) {
		notFound(w)
		return
	}
	ids, err := tierIDs(tx, rid)
	if err != nil {
		fail(w, err)
		return
	}
	if len(ids) >= maxRankingTiers {
		bad(w, "too many tiers")
		return
	}
	label, _ := cleanText(in.Label, maxNameLen)
	res, err := tx.Exec(`INSERT INTO ranking_tier(ranking_id, label, color, pos) VALUES (?, ?, ?, ?)`, rid, label, strings.ToLower(*in.Color), len(ids))
	if err != nil {
		fail(w, err)
		return
	}
	tid, _ := res.LastInsertId()
	at := len(ids)
	if in.Index != nil && *in.Index >= 0 && *in.Index < len(ids) {
		at = *in.Index
	}
	order := append(append(append([]int64{}, ids[:at]...), tid), ids[at:]...)
	if err := renumberTiers(tx, order); err != nil {
		fail(w, err)
		return
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	t, err := loadTier(s.DB, tid)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, t)
}

func (s *Server) patchTier(w http.ResponseWriter, r *http.Request) {
	rid, ok1 := pathID(w, r)
	tid, ok2 := pathInt(r, "tid")
	if !ok1 || !ok2 {
		notFound(w)
		return
	}
	var in tierInput
	if !readJSON(w, r, &in, 4<<10) {
		return
	}
	if in.validate(false) != nil || (in.Label == nil && in.Color == nil) {
		bad(w, "invalid tier")
		return
	}
	if !tierOf(s.DB, rid, tid) {
		notFound(w)
		return
	}
	if in.Label != nil {
		label, _ := cleanText(in.Label, maxNameLen)
		if _, err := s.DB.Exec(`UPDATE ranking_tier SET label = ? WHERE id = ?`, label, tid); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Color != nil {
		if _, err := s.DB.Exec(`UPDATE ranking_tier SET color = ? WHERE id = ?`, strings.ToLower(*in.Color), tid); err != nil {
			fail(w, err)
			return
		}
	}
	touch(s.DB, rid)
	t, err := loadTier(s.DB, tid)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, t)
}

// deleteTier removes a tier; its games go back to the end of the pool, in the order they had.
func (s *Server) deleteTier(w http.ResponseWriter, r *http.Request) {
	rid, ok1 := pathID(w, r)
	tid, ok2 := pathInt(r, "tid")
	if !ok1 || !ok2 {
		notFound(w)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !tierOf(tx, rid, tid) {
		notFound(w)
		return
	}
	if _, err := tx.Exec(`UPDATE ranking_entry SET pos = pos + (SELECT coalesce(max(pos), -1) + 1 FROM ranking_entry WHERE ranking_id = ? AND tier_id IS NULL), tier_id = NULL
		WHERE ranking_id = ? AND tier_id = ?`, rid, rid, tid); err != nil {
		fail(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM ranking_tier WHERE id = ?`, tid); err != nil {
		fail(w, err)
		return
	}
	ids, err := tierIDs(tx, rid)
	if err == nil {
		err = renumberTiers(tx, ids)
	}
	if err != nil {
		fail(w, err)
		return
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderTiers(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var ids []int64
	if !readJSON(w, r, &ids, 4<<10) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !rankingExists(tx, rid) {
		notFound(w)
		return
	}
	if !samePermutation(tx, `SELECT id FROM ranking_tier WHERE ranking_id = ?`, ids, rid) {
		bad(w, "the list must name every tier of the ranking once")
		return
	}
	if err := renumberTiers(tx, ids); err != nil {
		fail(w, err)
		return
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.respondRanking(w, http.StatusOK, rid)
}

// ---------------- entries ----------------

// listOf returns the game ids of one tier (nil: the pool) in order.
func listOf(tx *sql.Tx, rid int64, tier *int64) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if tier == nil {
		rows, err = tx.Query(`SELECT game_id FROM ranking_entry WHERE ranking_id = ? AND tier_id IS NULL ORDER BY pos, game_id`, rid)
	} else {
		rows, err = tx.Query(`SELECT game_id FROM ranking_entry WHERE ranking_id = ? AND tier_id = ? ORDER BY pos, game_id`, rid, *tier)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// setList writes the games of a tier (nil: the pool) as that tier's content, in the given order.
func setList(tx *sql.Tx, rid int64, tier *int64, ids []int64) error {
	var t any
	if tier != nil {
		t = *tier
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE ranking_entry SET tier_id = ?, pos = ? WHERE ranking_id = ? AND game_id = ?`, t, i, rid, id); err != nil {
			return err
		}
	}
	return nil
}

// addEntries puts games in a ranking at the end of a tier (nil: the pool), skipping the ones already in it. It returns
// the ids that were new, or a reason when a game does not exist.
func addEntries(tx *sql.Tx, rid int64, gameIDs []int64, tier *int64) (added []int64, reason string) {
	var t any
	if tier != nil {
		t = *tier
	}
	seen := map[int64]bool{}
	for _, gid := range gameIDs {
		if seen[gid] {
			continue
		}
		seen[gid] = true
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM game WHERE id = ?`, gid).Scan(&one); err != nil {
			return nil, "game " + strconv.FormatInt(gid, 10) + " does not exist"
		}
		if err := tx.QueryRow(`SELECT 1 FROM ranking_entry WHERE ranking_id = ? AND game_id = ?`, rid, gid).Scan(&one); err == nil {
			continue
		}
		pos := 0
		if tier == nil {
			tx.QueryRow(`SELECT coalesce(max(pos) + 1, 0) FROM ranking_entry WHERE ranking_id = ? AND tier_id IS NULL`, rid).Scan(&pos)
		} else {
			tx.QueryRow(`SELECT coalesce(max(pos) + 1, 0) FROM ranking_entry WHERE ranking_id = ? AND tier_id = ?`, rid, *tier).Scan(&pos)
		}
		if _, err := tx.Exec(`INSERT INTO ranking_entry(ranking_id, game_id, tier_id, pos) VALUES (?, ?, ?, ?)`, rid, gid, t, pos); err != nil {
			return nil, "could not add game"
		}
		added = append(added, gid)
	}
	return added, ""
}

func (s *Server) addRankingEntries(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		GameIDs []int64 `json:"game_ids"`
		TierID  *int64  `json:"tier_id"`
	}
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	if len(in.GameIDs) == 0 || len(in.GameIDs) > maxRankingEntries {
		bad(w, "invalid game list")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !rankingExists(tx, rid) {
		notFound(w)
		return
	}
	if in.TierID != nil && !tierOf(tx, rid, *in.TierID) {
		bad(w, "unknown tier")
		return
	}
	added, reason := addEntries(tx, rid, in.GameIDs, in.TierID)
	if reason != "" {
		bad(w, reason)
		return
	}
	if n, _ := countRows(tx, `SELECT count(*) FROM ranking_entry WHERE ranking_id = ?`, rid); n > maxRankingEntries {
		bad(w, "too many games in this ranking")
		return
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.posterQ().Add(added...)
	if added == nil {
		added = []int64{}
	}
	writeJSON(w, map[string]any{"added": added})
}

func (s *Server) removeRankingEntry(w http.ResponseWriter, r *http.Request) {
	rid, ok1 := pathID(w, r)
	gid, ok2 := pathInt(r, "gid")
	if !ok1 || !ok2 {
		notFound(w)
		return
	}
	res, err := s.DB.Exec(`DELETE FROM ranking_entry WHERE ranking_id = ? AND game_id = ?`, rid, gid)
	if err != nil {
		fail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		notFound(w)
		return
	}
	touch(s.DB, rid)
	w.WriteHeader(http.StatusNoContent)
}

// moveEntries moves games that are already in the ranking to a tier (null: the pool) at an index (default: the end).
// The moved games keep their relative order; the games around them keep theirs.
func (s *Server) moveRankingEntries(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		GameIDs []int64 `json:"game_ids"`
		TierID  *int64  `json:"tier_id"`
		Index   *int    `json:"index"`
	}
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	if len(in.GameIDs) == 0 || len(in.GameIDs) > maxRankingEntries || (in.Index != nil && *in.Index < 0) {
		bad(w, "invalid move")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !rankingExists(tx, rid) {
		notFound(w)
		return
	}
	if in.TierID != nil && !tierOf(tx, rid, *in.TierID) {
		bad(w, "unknown tier")
		return
	}
	moving := map[int64]bool{}
	sources := map[int64]*int64{} // tier key (0 = pool) -> tier
	for _, gid := range in.GameIDs {
		if moving[gid] {
			bad(w, "a game appears twice")
			return
		}
		moving[gid] = true
		var tier *int64
		if err := tx.QueryRow(`SELECT tier_id FROM ranking_entry WHERE ranking_id = ? AND game_id = ?`, rid, gid).Scan(&tier); err != nil {
			bad(w, "game "+strconv.FormatInt(gid, 10)+" is not in this ranking")
			return
		}
		key := int64(0)
		if tier != nil {
			key = *tier
		}
		sources[key] = tier
	}
	without := func(ids []int64) []int64 {
		out := make([]int64, 0, len(ids))
		for _, id := range ids {
			if !moving[id] {
				out = append(out, id)
			}
		}
		return out
	}
	target, err := listOf(tx, rid, in.TierID)
	if err != nil {
		fail(w, err)
		return
	}
	target = without(target)
	at := len(target)
	if in.Index != nil && *in.Index < at {
		at = *in.Index
	}
	merged := append(append(append([]int64{}, target[:at]...), in.GameIDs...), target[at:]...)
	if err := setList(tx, rid, in.TierID, merged); err != nil {
		fail(w, err)
		return
	}
	for _, src := range sources { // close the gaps the move left behind
		if (src == nil && in.TierID == nil) || (src != nil && in.TierID != nil && *src == *in.TierID) {
			continue
		}
		rest, err := listOf(tx, rid, src)
		if err == nil {
			err = setList(tx, rid, src, rest)
		}
		if err != nil {
			fail(w, err)
			return
		}
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.respondRanking(w, http.StatusOK, rid)
}

// ---------------- board (what the editor saves) ----------------

type boardTier struct {
	ID    int64   `json:"id"`
	Label string  `json:"label"`
	Color string  `json:"color"`
	Games []int64 `json:"games"`
}

// putBoard saves the board the editor shows: the tiers in display order with their labels, colours and games, and the
// pool. It only touches what it lists: a tier or a game it does not mention keeps its place (a tier added or a game
// removed by a request that overtook this one is not undone); ids that are not in the ranking are ignored for games
// and rejected for tiers.
func (s *Server) putBoard(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r)
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Tiers *[]boardTier `json:"tiers"`
		Pool  *[]int64     `json:"pool"`
	}
	if !readJSON(w, r, &in, 256<<10) {
		return
	}
	if in.Tiers == nil || in.Pool == nil || len(*in.Tiers) > maxRankingTiers || len(*in.Pool) > maxRankingEntries {
		bad(w, "invalid board")
		return
	}
	for _, t := range *in.Tiers {
		if _, ok := cleanText(&t.Label, maxNameLen); !ok || !hexColor.MatchString(t.Color) {
			bad(w, "invalid tier")
			return
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if !rankingExists(tx, rid) {
		notFound(w)
		return
	}
	listed := map[int64]bool{} // games named by the body
	tierSeen := map[int64]bool{}
	var order []int64
	for _, t := range *in.Tiers {
		if tierSeen[t.ID] || !tierOf(tx, rid, t.ID) {
			bad(w, "unknown tier")
			return
		}
		tierSeen[t.ID] = true
		order = append(order, t.ID)
		for _, gid := range t.Games {
			if listed[gid] {
				bad(w, "a game appears twice")
				return
			}
			listed[gid] = true
		}
	}
	for _, gid := range *in.Pool {
		if listed[gid] {
			bad(w, "a game appears twice")
			return
		}
		listed[gid] = true
	}
	member := map[int64]bool{}
	mrows, err := tx.Query(`SELECT game_id FROM ranking_entry WHERE ranking_id = ?`, rid)
	if err != nil {
		fail(w, err)
		return
	}
	for mrows.Next() {
		var id int64
		if mrows.Scan(&id) == nil {
			member[id] = true
		}
	}
	mrows.Close()

	place := func(tier *int64, games []int64) error {
		cur, err := listOf(tx, rid, tier)
		if err != nil {
			return err
		}
		var final []int64
		for _, gid := range games {
			if member[gid] {
				final = append(final, gid)
			}
		}
		for _, gid := range cur { // games the body does not mention stay in the tier, after the listed ones
			if !listed[gid] {
				final = append(final, gid)
			}
		}
		return setList(tx, rid, tier, final)
	}
	for _, t := range *in.Tiers {
		tid := t.ID
		if _, err := tx.Exec(`UPDATE ranking_tier SET label = ?, color = ? WHERE id = ?`, strings.TrimSpace(t.Label), strings.ToLower(t.Color), tid); err != nil {
			fail(w, err)
			return
		}
		if err := place(&tid, t.Games); err != nil {
			fail(w, err)
			return
		}
	}
	if err := place(nil, *in.Pool); err != nil {
		fail(w, err)
		return
	}
	// A game listed in a tier but currently in another tier that the body did not mention is already moved by setList.
	// Tier order: the listed tiers in the given order, the others after them.
	rest, err := tierIDs(tx, rid)
	if err != nil {
		fail(w, err)
		return
	}
	for _, id := range rest {
		if !tierSeen[id] {
			order = append(order, id)
		}
	}
	if err := renumberTiers(tx, order); err != nil {
		fail(w, err)
		return
	}
	touch(tx, rid)
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.respondRanking(w, http.StatusOK, rid)
}
