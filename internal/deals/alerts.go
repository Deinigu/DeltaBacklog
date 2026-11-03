package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The two kinds of alert. The alert rule: the best price right now (on sale) is at or below the historical low
// (KindHistorical), or else at or below the lowest price of the last two years (KindTwoYear).
const (
	KindHistorical = "historical"
	KindTwoYear    = "two_year"
)

// KindLabel is how a kind reads in a message.
func KindLabel(kind string) string {
	if kind == KindHistorical {
		return "historical low"
	}
	return "lowest in 2 years"
}

const (
	// Cooldown: an alert for the same game is repeated only after this long, or at a lower price.
	Cooldown = 30 * 24 * time.Hour
	// HistoryWindow is "two years".
	HistoryWindow = 2 * 365 * 24 * time.Hour
	// historyTTL: a two-year low read less than this long ago is trusted (the window moves slowly).
	historyTTL = 7 * 24 * time.Hour
	// maxSingles: more alerts than this in one run go out as one message.
	maxSingles = 3
	// lockStale: a run that left its lock behind (a crash) is forgotten after this long. Longer than the 20 minutes a run may take.
	lockStale = 30 * time.Minute
)

// ErrBusy: another price run (the app's Refresh or the daily check) is going on.
var ErrBusy = errors.New("another price check is running")

const timeFmt = "2006-01-02T15:04:05Z"

// AcquireRun takes the lock that keeps the app's refresh and `deals-check` from running together. It is a row in the
// database, so it holds across processes. The returned func gives it back.
func AcquireRun(d *sql.DB) (release func(), err error) {
	res, err := d.Exec(`INSERT INTO setting(key, value) VALUES ('deals_run', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value WHERE setting.value < ?`,
		time.Now().UTC().Format(timeFmt), time.Now().Add(-lockStale).UTC().Format(timeFmt))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrBusy
	}
	return func() { d.Exec(`DELETE FROM setting WHERE key = 'deals_run'`) }, nil
}

// alertKind applies the rule to one price (cents). Only a price that is a discount counts: a game that never left
// its full price is not "at its low".
func alertKind(price *int64, cut *int, low, low2y *int64) string {
	if price == nil || cut == nil || *cut <= 0 {
		return ""
	}
	if low != nil && *price <= *low {
		return KindHistorical
	}
	if low2y != nil && *price <= *low2y {
		return KindTwoYear
	}
	return ""
}

// Alert is a deal worth telling the owner about.
type Alert struct {
	GameID   int64
	Title    string
	Kind     string
	Price    int64
	Currency string
	Cut      int
	Shop     string
	URL      string
}

// Evaluate applies the rule to the cached prices of the watched games, skips muted games and deals already announced
// (see Cooldown), records what it returns (unseen in the app) and returns it.
func Evaluate(d *sql.DB, now time.Time) ([]Alert, error) {
	rows, err := d.Query(`SELECT p.game_id, g.title, p.currency, coalesce(p.cur_shop, ''), p.cur_price, p.cur_cut, coalesce(p.cur_url, ''),
		p.low_price, p.low2y_price, coalesce(a.muted, 0), a.last_alert_price, coalesce(a.last_alert_at, '')
		FROM deal_price p JOIN game g ON g.id = p.game_id LEFT JOIN deal_alert a ON a.game_id = p.game_id
		WHERE ` + watchedSQL + ` ORDER BY g.title_sort, g.id`)
	if err != nil {
		return nil, err
	}
	var out []Alert
	for rows.Next() {
		var a Alert
		var price, low, low2y, lastPrice *int64
		var cut *int
		var muted int
		var lastAt string
		if err := rows.Scan(&a.GameID, &a.Title, &a.Currency, &a.Shop, &price, &cut, &a.URL, &low, &low2y, &muted, &lastPrice, &lastAt); err != nil {
			rows.Close()
			return nil, err
		}
		a.Kind = alertKind(price, cut, low, low2y)
		if a.Kind == "" || muted == 1 {
			continue
		}
		a.Price, a.Cut = *price, *cut
		if at, err := time.Parse(timeFmt, lastAt); err == nil && lastPrice != nil && a.Price >= *lastPrice && now.Sub(at) < Cooldown {
			continue // announced already, at this price or a lower one
		}
		out = append(out, a)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, a := range out {
		if _, err := d.Exec(`INSERT INTO deal_alert(game_id, last_alert_price, last_alert_kind, last_alert_at, seen) VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(game_id) DO UPDATE SET last_alert_price = excluded.last_alert_price, last_alert_kind = excluded.last_alert_kind,
			last_alert_at = excluded.last_alert_at, seen = 0`, a.GameID, a.Price, a.Kind, now.UTC().Format(timeFmt)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AlertInfo is a standing alert, for the app: the game's price is still at or below the one announced.
type AlertInfo struct {
	GameID int64  `json:"game_id"`
	Kind   string `json:"kind"`
	Price  int64  `json:"price"`
	At     string `json:"at"`
	Seen   bool   `json:"seen"`
}

// loadAlerts lists the standing alerts of the watched games (not muted).
func loadAlerts(d *sql.DB) ([]AlertInfo, error) {
	rows, err := d.Query(`SELECT a.game_id, a.last_alert_kind, a.last_alert_price, a.last_alert_at, a.seen
		FROM deal_alert a JOIN deal_price p ON p.game_id = a.game_id JOIN game g ON g.id = a.game_id
		WHERE a.muted = 0 AND a.last_alert_kind IS NOT NULL AND p.cur_price IS NOT NULL AND p.cur_price <= a.last_alert_price
		AND ` + watchedSQL + ` ORDER BY a.last_alert_at DESC, a.game_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertInfo{}
	for rows.Next() {
		var a AlertInfo
		var seen int
		if err := rows.Scan(&a.GameID, &a.Kind, &a.Price, &a.At, &seen); err != nil {
			return nil, err
		}
		a.Seen = seen == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

func loadMuted(d *sql.DB) ([]int64, error) {
	return gameIDs(d, watchedSQL+` AND EXISTS (SELECT 1 FROM deal_alert a WHERE a.game_id = g.id AND a.muted = 1)`)
}

// SetMuted turns the alerts of one game off (or on again).
func SetMuted(d *sql.DB, gameID int64, mute bool) error {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM game WHERE id = ?`, gameID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknownGame
	}
	v := 0
	if mute {
		v = 1
	}
	_, err := d.Exec(`INSERT INTO deal_alert(game_id, muted) VALUES (?, ?) ON CONFLICT(game_id) DO UPDATE SET muted = excluded.muted`, gameID, v)
	return err
}

// MarkSeen records that the owner has opened Deals: the alerts stop counting as new.
func MarkSeen(d *sql.DB) error {
	_, err := d.Exec(`UPDATE deal_alert SET seen = 1 WHERE seen = 0`)
	return err
}

// ---------------- the two-year low ----------------

// fillTwoYearLows reads the two-year low of the games that may need it. ITAD's overview has the all-time low only, so
// the price history is asked, one call per game, only for a game that is on sale above its historical low (at the low
// the alert is "historical low" anyway) and whose two-year low was not read within a week. A rate limit ends the step.
func fillTwoYearLows(ctx context.Context, d *sql.DB, hs HistorySource, now time.Time) error {
	rows, err := d.Query(`SELECT p.game_id, m.itad_id, p.currency FROM deal_price p
		JOIN deal_match m ON m.game_id = p.game_id JOIN game g ON g.id = p.game_id
		WHERE `+watchedSQL+` AND m.itad_id IS NOT NULL AND p.cur_price IS NOT NULL AND p.cur_cut > 0
		AND p.low_price IS NOT NULL AND p.cur_price > p.low_price
		AND (p.low2y_checked_at IS NULL OR p.low2y_checked_at < ?) ORDER BY p.game_id`, now.Add(-historyTTL).UTC().Format(timeFmt))
	if err != nil {
		return err
	}
	type job struct {
		id       int64
		itad     string
		currency string
	}
	var todo []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.itad, &j.currency); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, j)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	country := Country(d)
	for _, j := range todo {
		low, ok, err := hs.LowSince(ctx, j.itad, country, now.Add(-HistoryWindow))
		if err != nil {
			var rl *RateLimitError
			if errors.As(err, &rl) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		var cents any
		if ok && low.Currency == j.currency {
			cents = low.Cents
		}
		// Same guards as savePrice: the game is still matched with this ITAD game, still watched, same country.
		if _, err := d.Exec(`UPDATE deal_price SET low2y_price = ?, low2y_checked_at = ? WHERE game_id = ?
			AND EXISTS (SELECT 1 FROM deal_match WHERE game_id = ?3 AND itad_id = ?4)
			AND EXISTS (SELECT 1 FROM game g WHERE g.id = ?3 AND `+watchedSQL+`)
			AND coalesce((SELECT value FROM setting WHERE key = 'deals_country'), 'ES') = ?5`,
			cents, now.UTC().Format(timeFmt), j.id, j.itad, country); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- the daily check ----------------

// CheckResult is what a `deals-check` run did.
type CheckResult struct {
	Report Report
	Alerts []Alert
	Sent   bool // a notifier was set up and took the messages
}

// Check is the daily run: it refreshes the prices (same rules as the app's Refresh), reads the two-year lows that are
// needed, applies the alert rule and sends the alerts to notify (nil: only recorded for the app). The caller holds AcquireRun.
func Check(ctx context.Context, d *sql.DB, src Source, notify Notifier) (CheckResult, error) {
	var res CheckResult
	rep, err := Refresh(ctx, d, src, Options{})
	res.Report = rep
	if err != nil {
		return res, err
	}
	now := time.Now()
	if hs, ok := src.(HistorySource); ok && rep.Stopped == "" {
		if err := fillTwoYearLows(ctx, d, hs, now); err != nil {
			return res, err
		}
	}
	if res.Alerts, err = Evaluate(d, now); err != nil {
		return res, err
	}
	if notify != nil && len(res.Alerts) > 0 {
		for _, text := range Messages(res.Alerts) {
			if err := notify.Send(ctx, text); err != nil {
				return res, fmt.Errorf("sending the alerts: %w", err)
			}
		}
		res.Sent = true
	}
	return res, nil
}

// ---------------- messages ----------------

// String formats the amount with a currency symbol when it is a common one.
func (m Money) String() string {
	s := fmt.Sprintf("%d.%02d", m.Cents/100, m.Cents%100)
	switch m.Currency {
	case "EUR":
		return "€" + s
	case "USD":
		return "$" + s
	case "GBP":
		return "£" + s
	}
	return s + " " + m.Currency
}

func (a Alert) line() string {
	s := fmt.Sprintf("%s: %s", a.Title, Money{a.Price, a.Currency})
	if a.Shop != "" {
		s += " at " + a.Shop
	}
	return s + " (" + KindLabel(a.Kind) + ")"
}

// Messages turns the alerts of one run into texts: one per game, or a single list when there are more than three.
func Messages(alerts []Alert) []string {
	if len(alerts) == 0 {
		return nil
	}
	if len(alerts) <= maxSingles {
		out := make([]string, len(alerts))
		for i, a := range alerts {
			out[i] = "Price alert\n" + a.line()
			if a.URL != "" {
				out[i] += "\n" + a.URL
			}
		}
		return out
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Price alert: %d games\n", len(alerts))
	for _, a := range alerts {
		b.WriteString("\n• " + a.line())
		if a.URL != "" {
			b.WriteString("\n  " + a.URL)
		}
	}
	text := b.String()
	if r := []rune(text); len(r) > 4000 { // Telegram's limit is 4096
		text = string(r[:4000]) + "…"
	}
	return []string{text}
}
