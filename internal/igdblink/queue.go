package igdblink

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// QueueCandidate is one proposed IGDB match for a game in the review queue.
type QueueCandidate struct {
	IGDBID int64  `json:"igdb_id"`
	Name   string `json:"name"`
	Year   *int   `json:"year"`
	Score  int    `json:"score"`
	// Platforms IGDB lists for the game, and its igdb.com page name; they tell same-named games apart.
	Platforms []string `json:"platforms"`
	Slug      string   `json:"slug"`
}

// QueueItem is a game waiting for a decision. No candidates means the sync found nothing: search by hand.
type QueueItem struct {
	GameID     int64            `json:"game_id"`
	Title      string           `json:"title"`
	Year       *int             `json:"year"`
	Platforms  []string         `json:"platforms"`
	Status     string           `json:"status"`
	Cover      *string          `json:"cover"`
	Candidates []QueueCandidate `json:"candidates"`
}

// ErrTaken is returned by Accept when the IGDB game is already linked to another library game.
type ErrTaken struct {
	GameID int64
	Title  string
}

func (e *ErrTaken) Error() string { return fmt.Sprintf("already linked to %q", e.Title) }

// ErrAlreadyLinked is returned by Accept when the game already has a different IGDB link: a link is never replaced.
type ErrAlreadyLinked struct{ IGDBID int64 }

func (e *ErrAlreadyLinked) Error() string {
	return fmt.Sprintf("already linked to IGDB game %d", e.IGDBID)
}

// ErrNoGame is returned when the game does not exist.
var ErrNoGame = errors.New("no such game")

// Queue lists the games the sync could not link on its own: searched, still without igdb_id, not declared absent
// from IGDB. Games with candidates come first (best score first), then the ones IGDB did not know.
func Queue(d *sql.DB) ([]QueueItem, error) {
	rows, err := d.Query(`SELECT g.id, g.title, g.release_year, s.code, g.cover_file,
			coalesce((SELECT group_concat(p.name, char(10)) FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = g.id), '')
		FROM game g JOIN status s ON s.id = g.status_id
		WHERE g.igdb_id IS NULL AND g.igdb_checked_at IS NOT NULL AND g.igdb_not_found = 0
		ORDER BY g.title_sort`)
	if err != nil {
		return nil, err
	}
	items := []QueueItem{}
	byID := map[int64]int{}
	for rows.Next() {
		var it QueueItem
		var plats string
		if err := rows.Scan(&it.GameID, &it.Title, &it.Year, &it.Status, &it.Cover, &plats); err != nil {
			rows.Close()
			return nil, err
		}
		it.Platforms = []string{}
		if plats != "" {
			it.Platforms = strings.Split(plats, "\n")
		}
		it.Candidates = []QueueCandidate{}
		byID[it.GameID] = len(items)
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	crows, err := d.Query(`SELECT game_id, igdb_id, name, year, score, platforms, slug FROM sync_candidate WHERE status = 'pending' ORDER BY game_id, score DESC, id`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var gid int64
		var c QueueCandidate
		var plats string
		if err := crows.Scan(&gid, &c.IGDBID, &c.Name, &c.Year, &c.Score, &plats, &c.Slug); err != nil {
			return nil, err
		}
		c.Platforms = []string{}
		if plats != "" {
			c.Platforms = strings.Split(plats, ", ")
		}
		if i, ok := byID[gid]; ok {
			items[i].Candidates = append(items[i].Candidates, c)
		}
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Candidates, items[j].Candidates
		if (len(a) > 0) != (len(b) > 0) {
			return len(a) > 0
		}
		return len(a) > 0 && a[0].Score > b[0].Score
	})
	return items, nil
}

// Accept links a game to an IGDB game: a proposed candidate, or one the owner picked with the lookup. The game's
// other proposals are closed as rejected.
func Accept(d *sql.DB, gameID, igdbID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current sql.NullInt64
	if err := tx.QueryRow("SELECT igdb_id FROM game WHERE id = ?", gameID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return ErrNoGame
	} else if err != nil {
		return err
	}
	if current.Valid {
		if current.Int64 == igdbID {
			return nil // already done (a second tab, a double tap)
		}
		return &ErrAlreadyLinked{IGDBID: current.Int64}
	}
	var other int64
	var title string
	err = tx.QueryRow("SELECT id, title FROM game WHERE igdb_id = ? AND id <> ?", igdbID, gameID).Scan(&other, &title)
	if err == nil {
		return &ErrTaken{GameID: other, Title: title}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	t := now()
	if _, err := tx.Exec("UPDATE game SET igdb_id = ?, igdb_synced_at = ?, igdb_checked_at = coalesce(igdb_checked_at, ?), igdb_not_found = 0 WHERE id = ?", igdbID, t, t, gameID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE sync_candidate SET status = CASE WHEN igdb_id = ? THEN 'accepted' ELSE 'rejected' END WHERE game_id = ? AND status = 'pending'", igdbID, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// NotOnIGDB records that a game has no IGDB entry: its proposals are rejected and the sync never searches it again.
func NotOnIGDB(d *sql.DB, gameID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current sql.NullInt64
	if err := tx.QueryRow("SELECT igdb_id FROM game WHERE id = ?", gameID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return ErrNoGame
	} else if err != nil {
		return err
	}
	if current.Valid { // linked meanwhile (another device, Fill from IGDB): never mark a linked game as absent
		return &ErrAlreadyLinked{IGDBID: current.Int64}
	}
	if _, err := tx.Exec("UPDATE game SET igdb_not_found = 1, igdb_checked_at = coalesce(igdb_checked_at, ?) WHERE id = ?", now(), gameID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE sync_candidate SET status = 'rejected' WHERE game_id = ? AND status = 'pending'", gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// PrintReport writes a human-readable summary of a run.
func PrintReport(w io.Writer, r *Report) {
	mode := "Sync report"
	if r.DryRun {
		mode = "Dry run (nothing was written)"
	}
	fmt.Fprintf(w, "%s\n  games to link:  %d\n  searched:       %d\n", mode, r.Total, r.Searched)
	fmt.Fprintf(w, "  confident:      %d  (title score >= %d and a shared platform; %s)\n", r.Confident, ConfidentScore, verb(r.DryRun, "would be linked", "linked"))
	fmt.Fprintf(w, "  uncertain:      %d  (score %d-%d, no shared platform or several candidates; %s)\n", r.Uncertain, MinScore, ConfidentScore-1, verb(r.DryRun, "would go to the review queue", "in the review queue"))
	fmt.Fprintf(w, "  not found:      %d\n", r.NotFound)
	if r.Conflicts > 0 {
		fmt.Fprintf(w, "  (of the uncertain, %d matched an IGDB game another library game already has)\n", r.Conflicts)
	}
	if r.Errors > 0 {
		fmt.Fprintf(w, "  failed searches: %d (tried again next run)\n", r.Errors)
	}
	if r.BackupFile != "" {
		fmt.Fprintf(w, "  backup before applying: %s\n", r.BackupFile)
	}
	if r.Stopped != "" {
		fmt.Fprintf(w, "  STOPPED EARLY: %s\n  Run the same command again to continue.\n", r.Stopped)
	}
	for _, b := range []Bucket{Confident, Uncertain, NotFound} {
		if len(r.Examples[b]) == 0 {
			continue
		}
		fmt.Fprintf(w, "\nExamples, %s:\n", b)
		for _, e := range r.Examples[b] {
			if e.Match == "" {
				fmt.Fprintf(w, "  %s\n", e.Title)
			} else {
				fmt.Fprintf(w, "  %s  ->  %s\n", e.Title, e.Match)
			}
		}
	}
}

func verb(dry bool, dryText, text string) string {
	if dry {
		return dryText
	}
	return text
}
