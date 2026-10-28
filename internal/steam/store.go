package steam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

var storeLink = regexp.MustCompile(`^https?://store\.steampowered\.com/app/(\d+)`)

// Errors of the owner's actions.
var (
	ErrUnknownApp  = errors.New("unknown Steam app")
	ErrUnknownGame = errors.New("unknown game")
)

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// Row is a Steam app as the review list shows it. GameID is nil while no library game is matched; Source says how the
// match was made ("link", "title", "user") or, with no game, that the owner said there is none ("none").
type Row struct {
	AppID      int64   `json:"app_id"`
	Name       string  `json:"name"`
	Playtime   int64   `json:"playtime_min"`
	LastPlayed *int64  `json:"last_played"`
	GameID     *int64  `json:"game_id"`
	Source     *string `json:"source"`
	Ignored    bool    `json:"ignored"`
}

// List is what the Steam page shows: the cached apps and when they were read.
type List struct {
	Apps     []Row
	SyncedAt string
}

// Load reads the cache (never Steam).
func Load(d *sql.DB) (*List, error) {
	l := &List{Apps: []Row{}}
	_ = d.QueryRow(`SELECT value FROM setting WHERE key = 'steam_synced_at'`).Scan(&l.SyncedAt)
	rows, err := d.Query(`SELECT app_id, name, playtime_min, last_played, game_id, source, ignored FROM steam_app ORDER BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.AppID, &r.Name, &r.Playtime, &r.LastPlayed, &r.GameID, &r.Source, &r.Ignored); err != nil {
			return nil, err
		}
		l.Apps = append(l.Apps, r)
	}
	return l, rows.Err()
}

// ---------------- matching ----------------

// libraryIndex finds the library game of a Steam app: by the app id of the game's Steam store link, else by an exact
// title. A link or title shared by two games matches none of them (the owner picks).
type libraryIndex struct {
	byApp   map[int64][]int64
	byTitle map[string][]int64
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func loadIndex(q querier) (*libraryIndex, error) {
	rows, err := q.Query(`SELECT g.id, g.title, coalesce(l.url, '') FROM game g LEFT JOIN game_link l ON l.game_id = g.id AND l.kind = 'steam'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ix := &libraryIndex{byApp: map[int64][]int64{}, byTitle: map[string][]int64{}}
	for rows.Next() {
		var id int64
		var title, link string
		if err := rows.Scan(&id, &title, &link); err != nil {
			return nil, err
		}
		if m := storeLink.FindStringSubmatch(link); m != nil {
			app, _ := strconv.ParseInt(m[1], 10, 64)
			ix.byApp[app] = append(ix.byApp[app], id)
		}
		if k := deals.TitleKey(title); k != "" {
			ix.byTitle[k] = append(ix.byTitle[k], id)
		}
	}
	return ix, rows.Err()
}

// find returns the matched game id and how it matched; 0 when there is none.
func (ix *libraryIndex) find(appID int64, name string) (int64, string) {
	if ids := ix.byApp[appID]; len(ids) == 1 {
		return ids[0], "link"
	} else if len(ids) > 1 {
		return 0, ""
	}
	if k := deals.TitleKey(name); k != "" {
		if ids := ix.byTitle[k]; len(ids) == 1 {
			return ids[0], "title"
		}
	}
	return 0, ""
}

// ---------------- sync ----------------

// Report is the outcome of a sync, in counts only.
type Report struct {
	Owned     int // apps the account owns
	Matched   int // of them, in the library
	Unmatched int // not in the library and not ignored: the review list
	Ignored   int // hidden by the owner
}

func (r Report) String() string {
	return fmt.Sprintf("%d owned, %d in your library, %d to review.", r.Owned, r.Matched, r.Unmatched)
}

// Sync reads the owned games and stores them: names and hours of the apps, matches for the new ones (an owner's choice is
// never touched), and the hours of the matched library games. Hours only go up: a read that shows fewer minutes than are
// stored (or leaves an app out) changes nothing. When Steam cannot answer, nothing is written.
func Sync(ctx context.Context, d *sql.DB, src Source) (Report, error) {
	return doSync(ctx, d, src, false)
}

// DryRun reads Steam and reports what Sync would find, writing nothing.
func DryRun(ctx context.Context, d *sql.DB, src Source) (Report, error) {
	return doSync(ctx, d, src, true)
}

func doSync(ctx context.Context, d *sql.DB, src Source, dry bool) (Report, error) {
	var rep Report
	if src == nil || !src.Configured() {
		return rep, ErrNotConfigured
	}
	apps, err := src.Owned(ctx)
	if err != nil {
		return rep, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	ix, err := loadIndex(tx)
	if err != nil {
		return rep, err
	}
	type stored struct {
		game   sql.NullInt64
		source sql.NullString
	}
	known := map[int64]stored{}
	rows, err := tx.Query(`SELECT app_id, game_id, source FROM steam_app`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var id int64
		var s stored
		if err := rows.Scan(&id, &s.game, &s.source); err != nil {
			rows.Close()
			return rep, err
		}
		known[id] = s
	}
	if err := rows.Close(); err != nil {
		return rep, err
	}
	at := now()
	for _, a := range apps {
		var last any
		if a.LastPlayed > 0 {
			last = a.LastPlayed
		}
		if _, err := tx.Exec(`INSERT INTO steam_app(app_id, name, playtime_min, last_played, seen_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(app_id) DO UPDATE SET name = excluded.name, seen_at = excluded.seen_at,
			playtime_min = max(playtime_min, excluded.playtime_min),
			last_played = nullif(max(coalesce(last_played, 0), coalesce(excluded.last_played, 0)), 0)`,
			a.ID, a.Name, a.Playtime, last, at); err != nil {
			return rep, err
		}
		if s, ok := known[a.ID]; ok && (s.source.String == "none" || (s.source.String == "user" && s.game.Valid)) {
			continue // the owner's choice
		}
		var game, source any
		if id, how := ix.find(a.ID, a.Name); id != 0 {
			game, source = id, how
		}
		if _, err := tx.Exec(`UPDATE steam_app SET game_id = ?, source = ? WHERE app_id = ?`, game, source, a.ID); err != nil {
			return rep, err
		}
	}
	// Hours of the library games: only raised, from the apps' own (never shrinking) totals.
	if _, err := tx.Exec(`UPDATE game SET
			playtime_min = max(coalesce(playtime_min, 0), t.minutes),
			steam_last_played = nullif(max(coalesce(steam_last_played, 0), coalesce(t.last, 0)), 0)
		FROM (SELECT game_id, sum(playtime_min) AS minutes, max(last_played) AS last FROM steam_app WHERE game_id IS NOT NULL GROUP BY game_id) t
		WHERE game.id = t.game_id`); err != nil {
		return rep, err
	}
	if _, err := tx.Exec(`INSERT INTO setting(key, value) VALUES ('steam_synced_at', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, at); err != nil {
		return rep, err
	}
	if err := tx.QueryRow(`SELECT count(*), coalesce(sum(game_id IS NOT NULL), 0), coalesce(sum(game_id IS NULL AND NOT ignored), 0),
		coalesce(sum(game_id IS NULL AND ignored), 0) FROM steam_app`).Scan(&rep.Owned, &rep.Matched, &rep.Unmatched, &rep.Ignored); err != nil {
		return rep, err
	}
	if dry {
		return rep, nil // the deferred Rollback drops everything
	}
	return rep, tx.Commit()
}

// ---------------- the owner's choices ----------------

// recompute sets a game's hours from the apps matched to it right now (NULL when none). Used when the owner changes a
// match, the one time hours may go down.
func recompute(tx *sql.Tx, gameID int64) error {
	_, err := tx.Exec(`UPDATE game SET
			playtime_min = (SELECT sum(playtime_min) FROM steam_app WHERE game_id = game.id),
			steam_last_played = (SELECT max(last_played) FROM steam_app WHERE game_id = game.id)
		WHERE id = ?`, gameID)
	return err
}

// change runs one edit of an app's match and refreshes the hours of the games it leaves and joins.
func change(d *sql.DB, appID int64, edit func(tx *sql.Tx) error) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old sql.NullInt64
	if err := tx.QueryRow(`SELECT game_id FROM steam_app WHERE app_id = ?`, appID).Scan(&old); errors.Is(err, sql.ErrNoRows) {
		return ErrUnknownApp
	} else if err != nil {
		return err
	}
	if err := edit(tx); err != nil {
		return err
	}
	var now sql.NullInt64
	if err := tx.QueryRow(`SELECT game_id FROM steam_app WHERE app_id = ?`, appID).Scan(&now); err != nil {
		return err
	}
	for _, g := range []sql.NullInt64{old, now} {
		if g.Valid {
			if err := recompute(tx, g.Int64); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// SetMatch matches an app to a library game, as the owner's choice.
func SetMatch(d *sql.DB, appID, gameID int64) error {
	return change(d, appID, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM game WHERE id = ?`, gameID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrUnknownGame
		}
		_, err := tx.Exec(`UPDATE steam_app SET game_id = ?, source = 'user' WHERE app_id = ?`, gameID, appID)
		return err
	})
}

// SetNone says no library game is this app: it is never matched automatically.
func SetNone(d *sql.DB, appID int64) error {
	return change(d, appID, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE steam_app SET game_id = NULL, source = 'none' WHERE app_id = ?`, appID)
		return err
	})
}

// ClearMatch goes back to automatic matching for one app, and matches it again at once.
func ClearMatch(d *sql.DB, appID int64) error {
	return change(d, appID, func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRow(`SELECT name FROM steam_app WHERE app_id = ?`, appID).Scan(&name); err != nil {
			return err
		}
		ix, err := loadIndex(tx)
		if err != nil {
			return err
		}
		var game, source any
		if id, how := ix.find(appID, name); id != 0 {
			game, source = id, how
		}
		_, err = tx.Exec(`UPDATE steam_app SET game_id = ?, source = ? WHERE app_id = ?`, game, source, appID)
		return err
	})
}

// SetIgnored hides an app from the review list, or shows it again.
func SetIgnored(d *sql.DB, appID int64, ignored bool) error {
	v := 0
	if ignored {
		v = 1
	}
	res, err := d.Exec(`UPDATE steam_app SET ignored = ? WHERE app_id = ?`, v, appID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUnknownApp
	}
	return nil
}
