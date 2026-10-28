package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// DefaultCountry is the country prices are read for until the owner changes it.
const DefaultCountry = "ES"

const watchedSQL = `('wishlist', 'backlog', 'up_next')`

// retryUnmatched is how long an automatic "no match" is trusted before the search is made again.
const retryUnmatched = 7 * 24 * time.Hour

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// ValidCountry reports whether s is a two-letter upper-case country code.
func ValidCountry(s string) bool { return countryRe.MatchString(s) }

// Country is the country prices are read for.
func Country(d *sql.DB) string {
	var c string
	if err := d.QueryRow(`SELECT value FROM setting WHERE key = 'deals_country'`).Scan(&c); err != nil || !ValidCountry(c) {
		return DefaultCountry
	}
	return c
}

// SetCountry changes the country. The cached prices belong to the old one, so they are dropped.
func SetCountry(d *sql.DB, country string) error {
	if !ValidCountry(country) {
		return errors.New("country must be a two-letter code such as ES")
	}
	if country == Country(d) {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO setting(key, value) VALUES ('deals_country', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, country); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM deal_price`); err != nil {
		return err
	}
	return tx.Commit()
}

// Match is the ITAD game a library game is linked to. ITADID is empty when there is no match.
type Match struct {
	GameID int64  `json:"game_id"`
	ITADID string `json:"itad_id"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	Source string `json:"source"` // "steam", "title" or "user"
}

// Price is the cached price of a game; the amounts are cents of Currency. Cur* are nil when no shop sells it.
type Price struct {
	GameID    int64   `json:"game_id"`
	Currency  string  `json:"currency"`
	Shop      *string `json:"shop"`
	Price     *int64  `json:"price"`
	Regular   *int64  `json:"regular"`
	Cut       *int    `json:"cut"`
	URL       *string `json:"url"`
	Expiry    *string `json:"expiry"`
	LowPrice  *int64  `json:"low_price"`
	LowShop   *string `json:"low_shop"`
	LowAt     *string `json:"low_at"`
	FetchedAt string  `json:"fetched_at"`
}

// List is everything the Deals page and the drawer show: matches and prices of the watched games.
type List struct {
	Country string
	Matches []Match
	Prices  []Price
}

// Load reads the cache (never ITAD).
func Load(d *sql.DB) (*List, error) {
	l := &List{Country: Country(d), Matches: []Match{}, Prices: []Price{}}
	rows, err := d.Query(`SELECT m.game_id, coalesce(m.itad_id, ''), m.itad_title, m.itad_slug, m.source
		FROM deal_match m JOIN game g ON g.id = m.game_id JOIN status s ON s.id = g.status_id
		WHERE s.code IN ` + watchedSQL + ` ORDER BY m.game_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.GameID, &m.ITADID, &m.Title, &m.Slug, &m.Source); err != nil {
			rows.Close()
			return nil, err
		}
		l.Matches = append(l.Matches, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = d.Query(`SELECT p.game_id, p.currency, p.cur_shop, p.cur_price, p.cur_regular, p.cur_cut, p.cur_url, p.cur_expiry,
		p.low_price, p.low_shop, p.low_at, p.fetched_at
		FROM deal_price p JOIN game g ON g.id = p.game_id JOIN status s ON s.id = g.status_id
		WHERE s.code IN ` + watchedSQL + ` ORDER BY p.game_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.GameID, &p.Currency, &p.Shop, &p.Price, &p.Regular, &p.Cut, &p.URL, &p.Expiry,
			&p.LowPrice, &p.LowShop, &p.LowAt, &p.FetchedAt); err != nil {
			return nil, err
		}
		l.Prices = append(l.Prices, p)
	}
	return l, rows.Err()
}

// ErrUnknownGame: no such game in the library.
var ErrUnknownGame = errors.New("unknown game")

// SetMatch records the owner's choice for a game: a game (g.ID set) or, with an empty g, "no match". Prices read for a
// previous choice are dropped.
func SetMatch(d *sql.DB, gameID int64, g Game) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM game WHERE id = ?`, gameID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknownGame
	}
	var id any
	if g.ID != "" {
		id = g.ID
	}
	if _, err := tx.Exec(`INSERT INTO deal_match(game_id, itad_id, itad_title, itad_slug, source, matched_at) VALUES (?, ?, ?, ?, 'user', ?)
		ON CONFLICT(game_id) DO UPDATE SET itad_id = excluded.itad_id, itad_title = excluded.itad_title, itad_slug = excluded.itad_slug,
		source = 'user', matched_at = excluded.matched_at`, gameID, id, g.Title, g.Slug, now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM deal_price WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearMatch forgets a game's match; the next refresh looks it up again.
func ClearMatch(d *sql.DB, gameID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM deal_match WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM deal_price WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// ---------------- matching ----------------

var steamApp = regexp.MustCompile(`^https?://store\.steampowered\.com/app/(\d+)`)

// normTitle makes two spellings of a title comparable: case, accents of the trademark kind, "&" and punctuation.
func normTitle(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("’", "'", "&", " and ", "™", "", "®", "", "©", "").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SameTitle is the strict title test of the automatic match: equal after normalising.
func SameTitle(a, b string) bool {
	na := normTitle(a)
	return na != "" && na == normTitle(b)
}

type libGame struct {
	id    int64
	title string
	appID int64 // Steam app id from the game's Steam link, 0 when none
}

// watchedGames lists the games that get prices, with their Steam app id.
func watchedGames(d *sql.DB) ([]libGame, error) {
	rows, err := d.Query(`SELECT g.id, g.title, coalesce(l.url, '') FROM game g JOIN status s ON s.id = g.status_id
		LEFT JOIN game_link l ON l.game_id = g.id AND l.kind = 'steam'
		WHERE s.code IN ` + watchedSQL + ` ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []libGame
	for rows.Next() {
		var g libGame
		var link string
		if err := rows.Scan(&g.id, &g.title, &link); err != nil {
			return nil, err
		}
		if m := steamApp.FindStringSubmatch(link); m != nil {
			g.appID, _ = strconv.ParseInt(m[1], 10, 64)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---------------- refresh ----------------

// Report is the outcome of a refresh.
type Report struct {
	Games     int    // watched games
	Matched   int    // games with an ITAD match
	Unmatched int    // games ITAD could not be matched to
	Priced    int    // games whose prices were read
	Stopped   string // why the run ended early; empty when it ran to the end
}

func (r Report) String() string {
	s := fmt.Sprintf("%d %s priced", r.Priced, plural(r.Priced, "game", "games"))
	if r.Unmatched > 0 {
		s += fmt.Sprintf(", %d without a match", r.Unmatched)
	}
	if r.Stopped != "" {
		s += ". Stopped: " + r.Stopped
	}
	return s + "."
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Options tune a refresh.
type Options struct {
	Progress func(done, total int)
}

// Refresh matches the watched games that have no match yet and reads the prices of all matched ones. It stops at the first
// rate limit, keeping what it has saved so far.
func Refresh(ctx context.Context, d *sql.DB, src Source, opt Options) (Report, error) {
	var rep Report
	if src == nil || !src.Configured() {
		return rep, ErrNotConfigured
	}
	games, err := watchedGames(d)
	if err != nil {
		return rep, err
	}
	rep.Games = len(games)
	matches, err := loadMatches(d)
	if err != nil {
		return rep, err
	}
	var todo []libGame
	for _, g := range games {
		if m, ok := matches[g.id]; ok && !(m.noID && m.source != "user" && m.stale()) {
			continue
		}
		todo = append(todo, g)
	}
	progress := func(done, total int) {
		if opt.Progress != nil {
			opt.Progress(done, total)
		}
	}
	total, done := len(todo)+len(games), 0
	progress(0, total)
	stop := func(err error) (Report, error) {
		var rl *RateLimitError
		if errors.As(err, &rl) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			rep.Stopped = err.Error()
			return rep, nil
		}
		return rep, err
	}
	for _, g := range todo {
		found, err := match(ctx, src, g)
		if err != nil {
			return stop(err)
		}
		if err := saveAutoMatch(d, g.id, found); err != nil {
			return rep, err
		}
		// What is stored wins over what was just found: the owner may have chosen or reset a match meanwhile.
		stored, err := storedMatch(d, g.id)
		if err != nil {
			return rep, err
		}
		matches[g.id] = stored
		done++
		progress(done, total)
	}
	// Prices, one ITAD id at a time even if two library games share it.
	byID := map[string][]int64{}
	var ids []string
	for _, g := range games {
		m := matches[g.id]
		if m.id == "" {
			rep.Unmatched++
			done++
			continue
		}
		rep.Matched++
		if _, seen := byID[m.id]; !seen {
			ids = append(ids, m.id)
		}
		byID[m.id] = append(byID[m.id], g.id)
	}
	progress(done, total)
	country := Country(d)
	for len(ids) > 0 {
		n := min(len(ids), MaxBatch)
		batch := ids[:n]
		ids = ids[n:]
		res, err := src.Overview(ctx, batch, country)
		if err != nil {
			return stop(err)
		}
		for _, id := range batch {
			for _, gid := range byID[id] {
				ok, err := savePrice(d, gid, id, country, res[id])
				if err != nil {
					return rep, err
				}
				if ok {
					rep.Priced++
				}
				done++
			}
		}
		progress(done, total)
	}
	return rep, nil
}

// RefreshOne reads the prices of one matched game (after the owner fixed its match).
func RefreshOne(ctx context.Context, d *sql.DB, src Source, gameID int64) error {
	if src == nil || !src.Configured() {
		return ErrNotConfigured
	}
	var id string
	err := d.QueryRow(`SELECT m.itad_id FROM deal_match m JOIN game g ON g.id = m.game_id JOIN status s ON s.id = g.status_id
		WHERE m.game_id = ? AND m.itad_id IS NOT NULL AND s.code IN `+watchedSQL, gameID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	country := Country(d)
	res, err := src.Overview(ctx, []string{id}, country)
	if err != nil {
		return err
	}
	_, err = savePrice(d, gameID, id, country, res[id])
	return err
}

// storedMatch reads a game's match as it is now. A game with no row (the owner reset it) counts as unmatched.
func storedMatch(d *sql.DB, gameID int64) (matchRow, error) {
	var id sql.NullString
	var m matchRow
	err := d.QueryRow(`SELECT itad_id, source FROM deal_match WHERE game_id = ?`, gameID).Scan(&id, &m.source)
	if errors.Is(err, sql.ErrNoRows) {
		return matchRow{noID: true, source: "title"}, nil
	}
	if err != nil {
		return matchRow{}, err
	}
	m.id, m.noID = id.String, !id.Valid
	return m, nil
}

type matchRow struct {
	id     string
	noID   bool
	source string
	at     time.Time
}

func (m matchRow) stale() bool { return time.Since(m.at) > retryUnmatched }

func loadMatches(d *sql.DB) (map[int64]matchRow, error) {
	rows, err := d.Query(`SELECT game_id, itad_id, source, matched_at FROM deal_match`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]matchRow{}
	for rows.Next() {
		var gid int64
		var id sql.NullString
		var m matchRow
		var at string
		if err := rows.Scan(&gid, &id, &m.source, &at); err != nil {
			return nil, err
		}
		m.id, m.noID = id.String, !id.Valid
		m.at, _ = time.Parse("2006-01-02T15:04:05Z", at)
		out[gid] = m
	}
	return out, rows.Err()
}

type found struct {
	Game   Game
	Source string
}

// match finds the ITAD game of a library game: by Steam app id when it has one, else by an exact title.
func match(ctx context.Context, src Source, g libGame) (found, error) {
	if g.appID > 0 {
		itad, ok, err := src.LookupAppID(ctx, g.appID)
		if err != nil {
			return found{}, err
		}
		if ok {
			return found{itad, "steam"}, nil
		}
	}
	itad, ok, err := src.LookupTitle(ctx, g.title)
	if err != nil {
		return found{}, err
	}
	if ok && SameTitle(itad.Title, g.title) {
		return found{itad, "title"}, nil
	}
	return found{Source: "title"}, nil
}

// saveAutoMatch stores an automatic result without touching a choice the owner made meanwhile.
func saveAutoMatch(d *sql.DB, gameID int64, f found) error {
	var id any
	if f.Game.ID != "" {
		id = f.Game.ID
	}
	_, err := d.Exec(`INSERT INTO deal_match(game_id, itad_id, itad_title, itad_slug, source, matched_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id) DO UPDATE SET itad_id = excluded.itad_id, itad_title = excluded.itad_title, itad_slug = excluded.itad_slug,
		source = excluded.source, matched_at = excluded.matched_at WHERE deal_match.source <> 'user'`,
		gameID, id, f.Game.Title, f.Game.Slug, f.Source, now())
	return err
}

// savePrice stores prices read for itadID in country. It does nothing (and says false) unless the game is still matched
// with that ITAD game and the country is still the one the prices were read for: a refresh takes minutes, and the owner may
// fix a match, reset it or change the country meanwhile.
func savePrice(d *sql.DB, gameID int64, itadID, country string, o Overview) (bool, error) {
	cur := o.Current
	var currency, shop, curURL, expiry any
	var price, regular, cut any
	var lowPrice, lowShop, lowAt any
	currency = "EUR"
	if cur != nil {
		currency, shop, price, regular, cut = cur.Price.Currency, cur.Shop, cur.Price.Cents, cur.Regular.Cents, cur.Cut
		if cur.URL != "" {
			curURL = cur.URL
		}
		if cur.Expiry != "" {
			expiry = cur.Expiry
		}
	}
	if o.Low != nil {
		lowPrice, lowShop, lowAt = o.Low.Price.Cents, o.Low.Shop, o.Low.At
		if cur == nil {
			currency = o.Low.Price.Currency
		}
	}
	res, err := d.Exec(`INSERT INTO deal_price(game_id, currency, cur_shop, cur_price, cur_regular, cur_cut, cur_url, cur_expiry,
		low_price, low_shop, low_at, fetched_at) SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM deal_match WHERE game_id = ?1 AND itad_id = ?13)
		AND coalesce((SELECT value FROM setting WHERE key = 'deals_country'), 'ES') = ?14
		ON CONFLICT(game_id) DO UPDATE SET currency = excluded.currency, cur_shop = excluded.cur_shop, cur_price = excluded.cur_price,
		cur_regular = excluded.cur_regular, cur_cut = excluded.cur_cut, cur_url = excluded.cur_url, cur_expiry = excluded.cur_expiry,
		low_price = excluded.low_price, low_shop = excluded.low_shop, low_at = excluded.low_at, fetched_at = excluded.fetched_at`,
		gameID, currency, shop, price, regular, cut, curURL, expiry, lowPrice, lowShop, lowAt, now(), itadID, country)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
