package igdblink

import (
	"database/sql"
	"fmt"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// ---------------- series ----------------
//
// IGDB puts almost every standalone game in a collection named after itself, while an empty series means "not part
// of a series" to the owner. So IGDB's collection is only used for a game's series when
//   (a) its folded name (meta.FoldKey) equals a series the library already has, or
//   (b) at least two library games share that collection (applySeriesProposals).
// Everything else stays empty and is only listed in the report.

func existingSeries(tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.Query("SELECT id, name FROM series")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[meta.FoldKey(name)] = id
	}
	return out, rows.Err()
}

func setSeries(tx *sql.Tx, gameID, seriesID int64) (bool, error) {
	res, err := tx.Exec("UPDATE game SET series_id = ? WHERE id = ? AND series_id IS NULL", seriesID, gameID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	return true, db.IndexGame(tx, gameID) // the search index holds the series name
}

// fillSeriesFromLibrary is rule (a): an empty series gets the library's own series that the IGDB name matches.
func fillSeriesFromLibrary(tx *sql.Tx, gameID int64, name string) (bool, error) {
	k := meta.FoldKey(name)
	if k == "" {
		return false, nil
	}
	have, err := existingSeries(tx)
	if err != nil {
		return false, err
	}
	id, ok := have[k]
	if !ok {
		return false, nil
	}
	return setSeries(tx, gameID, id)
}

// applySeriesProposals is rule (b), run after the batches over every enriched game that still has no series: a
// collection shared by two or more of them becomes a series (named as the first game spells it). The others are
// counted in the report.
func applySeriesProposals(tx *sql.Tx, rep *EnrichReport) error {
	rows, err := tx.Query(`SELECT id, title, igdb_series FROM game
		WHERE series_id IS NULL AND igdb_series IS NOT NULL AND igdb_series <> '' ORDER BY id`)
	if err != nil {
		return err
	}
	type member struct {
		id           int64
		title, label string
	}
	groups := map[string][]member{}
	var order []string
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.id, &m.title, &m.label); err != nil {
			rows.Close()
			return err
		}
		k := meta.FoldKey(m.label)
		if k == "" {
			continue
		}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	have, err := existingSeries(tx)
	if err != nil {
		return err
	}
	for _, k := range order {
		members := groups[k]
		id, exists := have[k]
		if !exists && len(members) < 2 {
			rep.SeriesProposed++
			if len(rep.SeriesExamples) < maxExamples {
				rep.SeriesExamples = append(rep.SeriesExamples, fmt.Sprintf("%s -> %s", members[0].title, members[0].label))
			}
			continue
		}
		if !exists {
			if id, err = db.GetOrCreate(tx, "series", members[0].label); err != nil {
				return err
			}
		}
		for _, m := range members {
			if ok, err := setSeries(tx, m.id, id); err != nil {
				return err
			} else if ok {
				rep.FilledSeries++
			}
		}
	}
	return nil
}

// ---------------- platforms ----------------
//
// PlatformKey (match.go) folds IGDB's names onto the library's; its alias table must stay in sync with ALIASES in
// web/static/js/platforms.js (TestPlatformAliasesMatchTheJS compares them).

// resolvePlatform finds the library's platform row for an IGDB platform name or abbreviation. It never creates one:
// a platform that is not in the library yet is the owner's decision.
func resolvePlatform(tx *sql.Tx, name string) (int64, bool, error) {
	rows, err := tx.Query("SELECT id, name FROM platform")
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	lib := map[string]int64{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return 0, false, err
		}
		lib[PlatformKey(n)] = id
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	id, ok := lib[PlatformKey(name)] // both sides fold through the same alias table
	return id, ok, nil
}
