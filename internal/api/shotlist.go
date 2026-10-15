package api

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultShotPage = 120
	maxShotPage     = 500
)

// GalleryShot is a screenshot in the all-games list: the usual fields plus the game's title and the date it is
// filed under (when it was taken, else when it was added).
type GalleryShot struct {
	Screenshot
	GameTitle string `json:"game_title"`
	Date      string `json:"date"`
}

// galleryDate is the date a screenshot is sorted and filtered by. taken_at is an ISO UTC string, as is added_at.
const galleryDate = `coalesce(screenshot.taken_at, screenshot.added_at)`

// listAllShots answers GET /api/screenshots: every screenshot of the library, one page at a time. Only database
// rows are read, so it costs the same whether or not the drive is mounted (`available` tells the client).
//
// Query: sort=newest (default; the newest date first) or sort=game (by game title, then the game's own order);
// q (part of the game title or the caption), year (of the date), game_id, limit (default 120, at most 500), offset.
// Paging is by offset: a screenshot added or deleted between two pages (from another device) shifts the rest by
// one, so one picture can show twice (the client drops the repeat by id) or be skipped until the page is reloaded.
// That is fine for a personal library; keyset paging (date, id) would fix it if it ever matters.
//
// The answer has `total` (all matches, not just this page); the first page (offset 0) also has `years` (every year with a
// screenshot, newest first, for the year filter) and `available`.
func (s *Server) listAllShots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := defaultShotPage, 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid limit", nil)
			return
		}
		limit = min(n, maxShotPage)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid offset", nil)
			return
		}
		offset = n
	}
	order := galleryDate + " DESC, screenshot.id DESC"
	switch q.Get("sort") {
	case "", "newest":
	case "game":
		order = "game.title COLLATE NOCASE, game.id, screenshot.pos, screenshot.id"
	default:
		writeErr(w, http.StatusBadRequest, "invalid sort", nil)
		return
	}

	where, args := []string{"1"}, []any{}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v) + "%"
		where = append(where, `(game.title LIKE ? ESCAPE '\' OR screenshot.caption LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if v := q.Get("year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < 1000 || y > 9999 {
			writeErr(w, http.StatusBadRequest, "invalid year", nil)
			return
		}
		where = append(where, `substr(`+galleryDate+`, 1, 4) = ?`)
		args = append(args, strconv.Itoa(y))
	}
	if v := q.Get("game_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			writeErr(w, http.StatusBadRequest, "invalid game_id", nil)
			return
		}
		where = append(where, `screenshot.game_id = ?`)
		args = append(args, id)
	}
	cond := strings.Join(where, " AND ")
	const from = ` FROM screenshot JOIN game ON game.id = screenshot.game_id WHERE `

	var total int
	if err := s.DB.QueryRow("SELECT count(*)"+from+cond, args...).Scan(&total); err != nil {
		fail(w, err)
		return
	}
	rows, err := s.DB.Query(`SELECT screenshot.id, screenshot.game_id, screenshot.file, screenshot.thumb, screenshot.width, screenshot.height,
		screenshot.bytes, coalesce(screenshot.taken_at, ''), coalesce(screenshot.caption, ''), screenshot.pos, screenshot.added_at,
		game.title, `+galleryDate+from+cond+" ORDER BY "+order+" LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	list := []GalleryShot{}
	for rows.Next() {
		var g GalleryShot
		var file, thumb string
		if err := rows.Scan(&g.ID, &g.GameID, &file, &thumb, &g.Width, &g.Height, &g.Bytes, &g.Taken, &g.Caption, &g.Pos, &g.Added, &g.GameTitle, &g.Date); err != nil {
			fail(w, err)
			return
		}
		g.Src = "/screenshots/" + strconv.FormatInt(g.GameID, 10) + "/" + file
		g.Thumb = "/screenshots/" + strconv.FormatInt(g.GameID, 10) + "/" + thumb
		list = append(list, g)
	}
	if err := rows.Err(); err != nil {
		fail(w, err)
		return
	}

	out := map[string]any{"screenshots": list, "total": total, "offset": offset, "limit": limit}
	if offset == 0 { // the years and the drive do not change from page to page: only the first page pays for them
		years := []int{}
		yr, err := s.DB.Query(`SELECT DISTINCT CAST(substr(` + galleryDate + `, 1, 4) AS INTEGER) y FROM screenshot ORDER BY y DESC`)
		if err != nil {
			fail(w, err)
			return
		}
		defer yr.Close()
		for yr.Next() {
			var y int
			if err := yr.Scan(&y); err != nil {
				fail(w, err)
				return
			}
			if y > 0 {
				years = append(years, y)
			}
		}
		if err := yr.Err(); err != nil {
			fail(w, err)
			return
		}
		out["years"] = years
		out["available"] = s.shotStorageReadable()
	}
	writeJSON(w, out)
}
