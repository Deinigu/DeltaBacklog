package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// View is a saved filter + sort + layout. Filter and Sort are opaque JSON owned by the frontend.
type View struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Icon      string          `json:"icon"`
	Filter    json.RawMessage `json:"filter"`
	Sort      json.RawMessage `json:"sort"`
	GroupBy   string          `json:"group_by"`
	Layout    string          `json:"layout"`
	Pinned    bool            `json:"pinned"`
	Pos       int             `json:"pos"`
	UpdatedAt string          `json:"updated_at"`
}

// viewInput is the writable subset; pointers distinguish "absent" from zero values on PATCH.
type viewInput struct {
	Name    *string         `json:"name"`
	Icon    *string         `json:"icon"`
	Filter  json.RawMessage `json:"filter"`
	Sort    json.RawMessage `json:"sort"`
	GroupBy *string         `json:"group_by"`
	Layout  *string         `json:"layout"`
	Pinned  *bool           `json:"pinned"`
}

var errBadView = errors.New("invalid view")

func (in *viewInput) validate(creating bool) error {
	if creating && (in.Name == nil || strings.TrimSpace(*in.Name) == "") {
		return errBadView
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errBadView
	}
	if in.Name != nil && len(*in.Name) > 80 {
		return errBadView
	}
	if in.Layout != nil && *in.Layout != "cards" && *in.Layout != "table" {
		return errBadView
	}
	if len(in.Filter) > 0 && !json.Valid(in.Filter) {
		return errBadView
	}
	if len(in.Sort) > 0 && !json.Valid(in.Sort) {
		return errBadView
	}
	if len(in.Filter) > 20000 || len(in.Sort) > 4000 {
		return errBadView
	}
	return nil
}

// LoadViews returns all saved views in navigation order.
func LoadViews(d *sql.DB) ([]View, error) {
	rows, err := d.Query(`SELECT id, name, icon, filter_json, sort_json, group_by, layout, pinned, pos, updated_at
		FROM saved_view ORDER BY pos, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		var v View
		var f, s string
		if err := rows.Scan(&v.ID, &v.Name, &v.Icon, &f, &s, &v.GroupBy, &v.Layout, &v.Pinned, &v.Pos, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Filter, v.Sort = json.RawMessage(f), json.RawMessage(s)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) listViews(w http.ResponseWriter, r *http.Request) {
	v, err := LoadViews(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) getView(id int64) (View, error) {
	all, err := LoadViews(s.DB)
	if err != nil {
		return View{}, err
	}
	for _, v := range all {
		if v.ID == id {
			return v, nil
		}
	}
	return View{}, sql.ErrNoRows
}

func decodeView(w http.ResponseWriter, r *http.Request, creating bool) (viewInput, bool) {
	var in viewInput
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.validate(creating) != nil {
		writeErr(w, http.StatusBadRequest, "invalid view", nil)
		return in, false
	}
	return in, true
}

func (s *Server) createView(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeView(w, r, true)
	if !ok {
		return
	}
	filter, sort := string(in.Filter), string(in.Sort)
	if filter == "" {
		filter = "{}"
	}
	if sort == "" {
		sort = "[]"
	}
	str := func(p *string, def string) string {
		if p == nil {
			return def
		}
		return *p
	}
	pinned := true
	if in.Pinned != nil {
		pinned = *in.Pinned
	}
	res, err := s.DB.Exec(`INSERT INTO saved_view (name, icon, filter_json, sort_json, group_by, layout, pinned, pos)
		VALUES (?, ?, ?, ?, ?, ?, ?, (SELECT coalesce(max(pos), 0) + 1 FROM saved_view))`,
		strings.TrimSpace(*in.Name), str(in.Icon, ""), filter, sort, str(in.GroupBy, ""), str(in.Layout, "cards"), pinned)
	if err != nil {
		fail(w, err)
		return
	}
	id, _ := res.LastInsertId()
	v, err := s.getView(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, v)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return 0, false
	}
	return id, true
}

func (s *Server) patchView(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decodeView(w, r, false)
	if !ok {
		return
	}
	cur, err := s.getView(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if in.Name != nil {
		cur.Name = strings.TrimSpace(*in.Name)
	}
	if in.Icon != nil {
		cur.Icon = *in.Icon
	}
	if len(in.Filter) > 0 {
		cur.Filter = in.Filter
	}
	if len(in.Sort) > 0 {
		cur.Sort = in.Sort
	}
	if in.GroupBy != nil {
		cur.GroupBy = *in.GroupBy
	}
	if in.Layout != nil {
		cur.Layout = *in.Layout
	}
	if in.Pinned != nil {
		cur.Pinned = *in.Pinned
	}
	if _, err := s.DB.Exec(`UPDATE saved_view SET name=?, icon=?, filter_json=?, sort_json=?, group_by=?, layout=?, pinned=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		cur.Name, cur.Icon, string(cur.Filter), string(cur.Sort), cur.GroupBy, cur.Layout, cur.Pinned, id); err != nil {
		fail(w, err)
		return
	}
	v, err := s.getView(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) deleteView(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.DB.Exec("DELETE FROM saved_view WHERE id = ?", id)
	if err != nil {
		fail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "not found", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reorderViews takes the full list of ids in the desired order.
func (s *Server) reorderViews(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeErr(w, http.StatusBadRequest, "expected a list of view ids", nil)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE saved_view SET pos = ? WHERE id = ?", i+1, id); err != nil {
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	v, err := LoadViews(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}
