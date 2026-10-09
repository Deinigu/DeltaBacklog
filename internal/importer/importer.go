// Package importer loads the old NocoDB Games_Library export (CSV) into the normalized schema.
package importer

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// Options configures an import run.
type Options struct {
	NocoDBBase string // e.g. http://nocodb.local:8080, used to resolve uploaded cover paths
}

// Report summarises a run; Warnings lists rows or values that needed a decision.
type Report struct {
	Inserted int
	Skipped  int // already imported (same source id)
	Warnings []string
}

// CoverJob is a game whose remote cover still has to be downloaded.
type CoverJob struct {
	GameID int64
	URL    string
}

// Import reads the CSV and inserts every game not yet present. It is idempotent:
// rows whose source id was already imported are left untouched.
func Import(d *sql.DB, r io.Reader, opt Options) (Report, []CoverJob, error) {
	var rep Report
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return rep, nil, fmt.Errorf("reading header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	for _, need := range []string{"id", "Title", "Status"} {
		if _, ok := col[need]; !ok {
			return rep, nil, fmt.Errorf("missing column %q", need)
		}
	}
	rows, err := cr.ReadAll()
	if err != nil {
		return rep, nil, err
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	// Keep the old manual order as the queue order.
	sort.SliceStable(rows, func(a, b int) bool {
		x, _ := strconv.ParseFloat(get(rows[a], "nc_order"), 64)
		y, _ := strconv.ParseFloat(get(rows[b], "nc_order"), 64)
		return x < y
	})

	statusByLabel := map[string]int64{}
	sr, err := d.Query("SELECT id, label FROM status")
	if err != nil {
		return rep, nil, err
	}
	for sr.Next() {
		var id int64
		var label string
		if err := sr.Scan(&id, &label); err != nil {
			sr.Close()
			return rep, nil, err
		}
		statusByLabel[strings.ToLower(label)] = id
	}
	sr.Close()
	backlogID := statusByLabel["backlog"]

	tx, err := d.Begin()
	if err != nil {
		return rep, nil, err
	}
	defer tx.Rollback()

	var jobs []CoverJob
	for n, row := range rows {
		srcID, err := strconv.ParseInt(get(row, "id"), 10, 64)
		title := get(row, "Title")
		if err != nil || title == "" {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("row %d: skipped (no id or title)", n+2))
			continue
		}
		var exists int64
		if err := tx.QueryRow("SELECT id FROM game WHERE source_id = ?", srcID).Scan(&exists); err == nil {
			rep.Skipped++
			continue
		}

		statusID, ok := statusByLabel[strings.ToLower(get(row, "Status"))]
		if !ok {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%q: unknown status %q, set to Backlog", title, get(row, "Status")))
			statusID = backlogID
		}
		var ownershipID, seriesID any
		if v := get(row, "Ownership"); v != "" {
			id, err := db.GetOrCreate(tx, "ownership", v)
			if err != nil {
				return rep, nil, err
			}
			ownershipID = id
		}
		if v := get(row, "Series"); v != "" {
			id, err := db.GetOrCreate(tx, "series", v)
			if err != nil {
				return rep, nil, err
			}
			seriesID = id
		}
		cover, err := parseBoxart(get(row, "Boxart"), opt.NocoDBBase)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%q: %v", title, err))
		}

		res, err := tx.Exec(`INSERT INTO game
			(title, title_sort, status_id, ownership_id, series_id, vr, favorite, completed, notes, cover_url, queue_pos, source_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			title, db.TitleSort(title), statusID, ownershipID, seriesID,
			b2i(parseBool(get(row, "VR"))), b2i(parseBool(get(row, "Favorito"))), b2i(parseBool(get(row, "Completed"))),
			get(row, "Notas"), nullIfEmpty(cover), n+1, srcID)
		if err != nil {
			return rep, nil, fmt.Errorf("inserting %q: %w", title, err)
		}
		gid, _ := res.LastInsertId()

		themes := splitList(get(row, "Style"))
		for i := range themes {
			themes[i] = db.NormalizeTheme(themes[i])
		}
		for _, l := range []struct {
			table, junction, fk string
			names               []string
		}{
			{"platform", "game_platform", "platform_id", splitList(get(row, "Platform"))},
			{"genre", "game_genre", "genre_id", splitList(get(row, "Genre"))},
			{"theme", "game_theme", "theme_id", themes},
			{"player", "game_player", "player_id", splitList(get(row, "Played_with"))},
		} {
			for _, name := range l.names {
				id, err := db.GetOrCreate(tx, l.table, name)
				if err != nil {
					return rep, nil, err
				}
				if _, err := tx.Exec("INSERT OR IGNORE INTO "+l.junction+"(game_id, "+l.fk+") VALUES (?,?)", gid, id); err != nil {
					return rep, nil, err
				}
			}
		}

		plays, bad := parseYears(get(row, "Years_played"))
		for _, b := range bad {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%q: unrecognised Years_played value %q", title, b))
		}
		for _, p := range plays {
			var year, period any
			if p.Period != "" {
				period = p.Period
			} else {
				year = p.Year
			}
			if _, err := tx.Exec("INSERT INTO play(game_id, year, period) VALUES (?,?,?)", gid, year, period); err != nil {
				return rep, nil, err
			}
		}
		if err := db.IndexGame(tx, gid); err != nil {
			return rep, nil, err
		}
		if cover != "" {
			jobs = append(jobs, CoverJob{GameID: gid, URL: cover})
		}
		rep.Inserted++
	}
	if err := tx.Commit(); err != nil {
		return rep, nil, err
	}
	return rep, jobs, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
