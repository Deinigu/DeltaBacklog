package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Meta is what quick add needs from the metadata services (satisfied by *meta.Client).
type Meta interface {
	Configured() (igdb, sgdb bool)
	SearchIGDB(ctx context.Context, title string) ([]meta.Candidate, error)
	SearchCovers(ctx context.Context, title string) ([]meta.CoverOption, error)
	ResolveLink(ctx context.Context, link string) (string, error)
}

var defaultCoverHosts = []string{"cdn2.steamgriddb.com", "cdn.steamgriddb.com", "images.igdb.com"}

func (s *Server) coverHostAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	hosts := s.CoverHosts
	if len(hosts) == 0 {
		hosts = defaultCoverHosts
	}
	for _, h := range hosts {
		if strings.EqualFold(u.Host, h) || strings.EqualFold(u.Hostname(), h) {
			return true
		}
	}
	return false
}

func writeErr(w http.ResponseWriter, code int, msg string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	out := map[string]any{"error": msg}
	for k, v := range extra {
		out[k] = v
	}
	json.NewEncoder(w).Encode(out)
}

// ---------------- lookup ----------------

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	igdb, _ := false, false
	if s.Meta != nil {
		igdb, _ = s.Meta.Configured()
	}
	if !igdb {
		writeJSON(w, map[string]any{"configured": false, "query": q, "results": []meta.Candidate{}})
		return
	}
	if len(q) < 2 {
		writeJSON(w, map[string]any{"configured": true, "query": q, "results": []meta.Candidate{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	title := q
	if meta.LooksLikeLink(q) {
		t, err := s.Meta.ResolveLink(ctx, q)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error(), nil)
			return
		}
		title = t
	}
	res, err := s.Meta.SearchIGDB(ctx, title)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "IGDB lookup failed", nil)
		return
	}
	if res == nil {
		res = []meta.Candidate{}
	}
	writeJSON(w, map[string]any{"configured": true, "query": title, "results": meta.RankByTitle(res, title)})
}

func (s *Server) searchCovers(w http.ResponseWriter, r *http.Request) {
	sgdb := false
	if s.Meta != nil {
		_, sgdb = s.Meta.Configured()
	}
	title := strings.TrimSpace(r.URL.Query().Get("q"))
	if !sgdb || title == "" {
		writeJSON(w, map[string]any{"configured": sgdb, "covers": []meta.CoverOption{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := s.Meta.SearchCovers(ctx, title)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "SteamGridDB lookup failed", nil)
		return
	}
	if c == nil {
		c = []meta.CoverOption{}
	}
	writeJSON(w, map[string]any{"configured": true, "covers": c})
}

// ---------------- create / delete / cover ----------------

type gameCreate struct {
	gamePatch
	IGDBID   *int64 `json:"igdb_id"`
	CoverURL string `json:"cover_url"`
}

func (s *Server) createGame(w http.ResponseWriter, r *http.Request) {
	var in gameCreate
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.validate() != nil {
		writeErr(w, http.StatusBadRequest, "invalid game", nil)
		return
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		writeErr(w, http.StatusBadRequest, "a title is required", nil)
		return
	}
	if in.CoverURL != "" && !s.coverHostAllowed(in.CoverURL) {
		writeErr(w, http.StatusBadRequest, "cover URL not allowed", nil)
		return
	}
	status := "backlog"
	if in.Status != nil && *in.Status != "" {
		status = *in.Status
	}
	if in.Themes != nil { // fold verbose IGDB theme names
		folded := make([]string, len(*in.Themes))
		for i, t := range *in.Themes {
			folded[i] = db.NormalizeTheme(t)
		}
		in.Themes = &folded
	}

	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()

	if in.IGDBID != nil {
		var existing int64
		if err := tx.QueryRow("SELECT id FROM game WHERE igdb_id = ?", *in.IGDBID).Scan(&existing); err == nil {
			writeErr(w, http.StatusConflict, "already in your library", map[string]any{"id": existing})
			return
		}
	}
	var sid int64
	if err := tx.QueryRow("SELECT id FROM status WHERE code = ?", status).Scan(&sid); err != nil {
		writeErr(w, http.StatusBadRequest, "unknown status", nil)
		return
	}
	title := strings.TrimSpace(*in.Title)
	res, err := tx.Exec(`INSERT INTO game (title, title_sort, status_id, igdb_id, queue_pos)
		VALUES (?, ?, ?, ?, (SELECT coalesce(max(queue_pos), 0) + 1 FROM game))`, title, db.TitleSort(title), sid, in.IGDBID)
	if err != nil {
		fail(w, err)
		return
	}
	id, _ := res.LastInsertId()
	if err := applyFieldEdits(tx, id, &in.gamePatch); err != nil {
		fail(w, err)
		return
	}
	if playingStatuses[status] {
		if err := recordCurrentYear(tx, id); err != nil {
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}

	coverErr := ""
	if in.CoverURL != "" {
		if err := s.storeCover(id, in.CoverURL); err != nil {
			coverErr = "the game was added, but its cover could not be downloaded"
		}
	}
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"game": g, "cover_error": coverErr})
}

// storeCover downloads url (already host-checked), writes the resized cover and records it on the game.
func (s *Server) storeCover(id int64, url string) error {
	name, err := covers.FetchStrict(url, s.CoverDir, id)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE game SET cover_url = ?, cover_file = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`, url, name, id)
	return err
}

func (s *Server) setCover(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !s.coverHostAllowed(in.URL) {
		writeErr(w, http.StatusBadRequest, "cover URL not allowed", nil)
		return
	}
	var one int
	if err := s.DB.QueryRow("SELECT 1 FROM game WHERE id = ?", id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if err := s.storeCover(id, in.URL); err != nil {
		writeErr(w, http.StatusBadGateway, "could not download that image", nil)
		return
	}
	g, err := LoadGame(s.DB, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, g)
}

func (s *Server) deleteGame(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	var cover sql.NullString
	if err := tx.QueryRow("SELECT cover_file FROM game WHERE id = ?", id).Scan(&cover); errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if _, err := tx.Exec("DELETE FROM game_fts WHERE rowid = ?", id); err != nil {
		fail(w, err)
		return
	}
	if _, err := tx.Exec("DELETE FROM game WHERE id = ?", id); err != nil { // links, plays and tier entries cascade
		fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	if cover.Valid && cover.String != "" {
		os.Remove(filepath.Join(s.CoverDir, filepath.Base(cover.String)))
	}
	w.WriteHeader(http.StatusNoContent)
}
