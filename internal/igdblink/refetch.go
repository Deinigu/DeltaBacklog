package igdblink

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// ErrNotLinked is returned by Refetch for a game without an IGDB link.
var ErrNotLinked = errors.New("game is not linked to IGDB")

// ErrNotOnIGDB is returned by Refetch when IGDB no longer returns the linked game.
var ErrNotOnIGDB = errors.New("IGDB has no such game")

// ErrStale is returned by Refetch when the link changed while IGDB was being read: nothing was written.
var ErrStale = errors.New("the IGDB link changed meanwhile")

// FieldChange is one line of "what would change": a field and its old and new text ("" = empty).
type FieldChange struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

// Refetched is the answer of Refetch: what changed (or would change), and a patch that puts the owner-editable fields
// back (the drawer's undo). Game modes, perspectives, times and the release date belong to IGDB and are not in the patch.
type Refetched struct {
	Changes []FieldChange  `json:"changes"`
	Undo    map[string]any `json:"undo"`
}

// detailsSnapshot is what Refetch compares before and after.
type detailsSnapshot struct {
	genres, themes, platforms, modes, perspectives []string
	developer, series                              string
	year                                           sql.NullInt64
	ttb                                            [3]sql.NullInt64
	ttbSrc                                         sql.NullString
	release                                        sql.NullInt64
}

func namesOf(tx *sql.Tx, query string, id int64) ([]string, error) {
	rows, err := tx.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func snapshot(tx *sql.Tx, id int64) (s detailsSnapshot, err error) {
	for _, q := range []struct {
		dst   *[]string
		query string
	}{
		{&s.genres, `SELECT p.name FROM game_genre x JOIN genre p ON p.id = x.genre_id WHERE x.game_id = ? ORDER BY p.name`},
		{&s.themes, `SELECT p.name FROM game_theme x JOIN theme p ON p.id = x.theme_id WHERE x.game_id = ? ORDER BY p.name`},
		{&s.platforms, `SELECT p.name FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = ? ORDER BY p.name`},
		{&s.modes, `SELECT p.name FROM game_game_mode x JOIN game_mode p ON p.id = x.game_mode_id WHERE x.game_id = ? ORDER BY p.name`},
		{&s.perspectives, `SELECT p.name FROM game_perspective x JOIN perspective p ON p.id = x.perspective_id WHERE x.game_id = ? ORDER BY p.name`},
	} {
		if *q.dst, err = namesOf(tx, q.query, id); err != nil {
			return s, err
		}
	}
	var dev, ser sql.NullString
	err = tx.QueryRow(`SELECT c.name, se.name, g.release_year, g.time_to_beat_hastily, g.time_to_beat_normally, g.time_to_beat_completely,
			g.time_to_beat_src, g.first_release_date
		FROM game g LEFT JOIN company c ON c.id = g.developer_id LEFT JOIN series se ON se.id = g.series_id WHERE g.id = ?`, id).
		Scan(&dev, &ser, &s.year, &s.ttb[0], &s.ttb[1], &s.ttb[2], &s.ttbSrc, &s.release)
	s.developer, s.series = dev.String, ser.String
	return s, err
}

func minutesText(v sql.NullInt64) string {
	if !v.Valid {
		return ""
	}
	if v.Int64 < 60 {
		return fmt.Sprintf("%d min", v.Int64)
	}
	if v.Int64%60 == 0 {
		return fmt.Sprintf("%d h", v.Int64/60)
	}
	return fmt.Sprintf("%.1f h", float64(v.Int64)/60)
}

func intText(v sql.NullInt64) string {
	if !v.Valid {
		return ""
	}
	return fmt.Sprint(v.Int64)
}

func listText(l []string) string { return strings.Join(l, ", ") }

// diffSnapshots lists the fields that differ.
func diffSnapshots(a, b detailsSnapshot) (changes []FieldChange, undo map[string]any) {
	undo = map[string]any{}
	add := func(field, label, old, now string) {
		if old != now {
			changes = append(changes, FieldChange{Field: field, Label: label, Old: old, New: now})
		}
	}
	add("genres", "Genres", listText(a.genres), listText(b.genres))
	add("themes", "Themes", listText(a.themes), listText(b.themes))
	add("developer", "Developer", a.developer, b.developer)
	add("series", "Series", a.series, b.series)
	add("release_year", "Released", intText(a.year), intText(b.year))
	add("platforms", "Platforms", listText(a.platforms), listText(b.platforms))
	add("modes", "Game modes", listText(a.modes), listText(b.modes))
	add("perspectives", "Perspective", listText(a.perspectives), listText(b.perspectives))
	for i, l := range []string{"Time to beat, quick", "Time to beat, normal", "Time to beat, complete"} {
		add(fmt.Sprintf("ttb_%d", i), l, minutesText(a.ttb[i]), minutesText(b.ttb[i]))
	}
	for _, c := range changes {
		switch c.Field {
		case "genres":
			undo["genres"] = a.genres
		case "themes":
			undo["themes"] = a.themes
		case "developer":
			undo["developer"] = a.developer
		case "series":
			undo["series"] = a.series
		case "release_year":
			undo["release_year"] = int(a.year.Int64) // 0 clears
		case "platforms":
			undo["platforms"] = a.platforms
		}
	}
	return changes, undo
}

// clearForReplace empties the fields IGDB has a value for, so the fill-only-empty code stores IGDB's value instead.
// A series is cleared only when the library already has the series IGDB names (the fill cannot create one), and the
// platforms are never touched: a game's platform is where the owner has it.
func clearForReplace(tx *sql.Tx, gameID int64, det meta.Details) error {
	stmts := []string{}
	if len(det.Genres) > 0 {
		stmts = append(stmts, "DELETE FROM game_genre WHERE game_id = ?")
	}
	if len(det.Themes) > 0 {
		stmts = append(stmts, "DELETE FROM game_theme WHERE game_id = ?")
	}
	if det.Developer != "" {
		stmts = append(stmts, "UPDATE game SET developer_id = NULL WHERE id = ?")
	}
	if det.Year >= 1950 && det.Year <= 2100 {
		stmts = append(stmts, "UPDATE game SET release_year = NULL WHERE id = ?")
	}
	if k := meta.FoldKey(det.Series); k != "" {
		have, err := existingSeries(tx)
		if err != nil {
			return err
		}
		if _, ok := have[k]; ok {
			stmts = append(stmts, "UPDATE game SET series_id = NULL WHERE id = ?")
		}
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, gameID); err != nil {
			return err
		}
	}
	return nil
}

// Refetch reads the game's current IGDB link and updates its details from it, with the code of `sync-igdb --enrich`.
// replace false fills only the empty fields; true first empties the ones IGDB has a value for. Status, ownership, notes,
// years played, covers and posters are never touched, and times set by hand or taken from HowLongToBeat are kept.
// With apply false nothing is written: the changes are computed in a transaction that is rolled back (the preview).
func Refetch(ctx context.Context, d *sql.DB, e Enricher, gameID int64, replace, apply bool) (*Refetched, error) {
	var igdbID sql.NullInt64
	if err := d.QueryRow("SELECT igdb_id FROM game WHERE id = ?", gameID).Scan(&igdbID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoGame
	} else if err != nil {
		return nil, err
	}
	if !igdbID.Valid {
		return nil, ErrNotLinked
	}
	details, err := e.GameDetails(ctx, []int64{igdbID.Int64})
	if err != nil {
		return nil, err
	}
	var det *meta.Details
	for i := range details {
		if details[i].IGDBID == igdbID.Int64 {
			det = &details[i]
		}
	}
	if det == nil {
		return nil, ErrNotOnIGDB
	}
	var ttb meta.TimeToBeat
	if m, err := e.TimeToBeat(ctx, []int64{igdbID.Int64}); err == nil { // times are a bonus: the rest is still worth storing
		ttb = m[igdbID.Int64]
	}

	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var still sql.NullInt64
	if err := tx.QueryRow("SELECT igdb_id FROM game WHERE id = ?", gameID).Scan(&still); err != nil {
		return nil, err
	}
	if !still.Valid || still.Int64 != igdbID.Int64 {
		return nil, ErrStale
	}
	before, err := snapshot(tx, gameID)
	if err != nil {
		return nil, err
	}
	if replace {
		if err := clearForReplace(tx, gameID, *det); err != nil {
			return nil, err
		}
	}
	if err := enrichGame(tx, gameID, *det, true, false, ttb, &EnrichReport{}); err != nil {
		return nil, err
	}
	after, err := snapshot(tx, gameID)
	if err != nil {
		return nil, err
	}
	changes, undo := diffSnapshots(before, after)
	if apply {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	if changes == nil {
		changes = []FieldChange{}
	}
	return &Refetched{Changes: changes, Undo: undo}, nil
}
