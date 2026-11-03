package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// The Steam wishlist section of Deals. Its games are not library games (no `game` row, no status, no flag): they live in
// wish_item / wish_price (migration 021), are shown beside the library's deals and take the same price row, alerts and Hide.
// A wishlist game that is also in the library is shown only as the library game (see libKeys).

// ErrUnknownWish: no such game on the wishlist.
var ErrUnknownWish = errors.New("unknown wishlist game")

// libKeys is what identifies a library game to a wishlist game: its Steam app id (from the Steam link), its ITAD id (from
// the Deals match) or, for a game that has neither, its title (normalised). Any one of them makes the wishlist game "already in the library", whatever the
// library game's status is: that is what makes the entry vanish after "Add to library", even for an Owned game.
type libKeys struct {
	apps   map[int64]bool
	itad   map[string]bool
	titles map[string]bool
}

func loadLibKeys(d *sql.DB) (libKeys, error) {
	k := libKeys{apps: map[int64]bool{}, itad: map[string]bool{}, titles: map[string]bool{}}
	// The title only identifies a library game that has nothing better: a game with a Steam link or an ITAD match is told
	// apart by those, so a remake that shares its name with a game in the library (Resident Evil 4) is not hidden by it.
	rows, err := d.Query(`SELECT g.title FROM game g
		WHERE NOT EXISTS (SELECT 1 FROM game_link l WHERE l.game_id = g.id AND l.kind = 'steam')
		AND NOT EXISTS (SELECT 1 FROM deal_match m WHERE m.game_id = g.id AND m.itad_id IS NOT NULL)`)
	if err != nil {
		return k, err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return k, err
		}
		if key := normTitle(t); key != "" {
			k.titles[key] = true
		}
	}
	if err := rows.Close(); err != nil {
		return k, err
	}
	if rows, err = d.Query(`SELECT url FROM game_link WHERE kind = 'steam'`); err != nil {
		return k, err
	}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return k, err
		}
		if m := steamApp.FindStringSubmatch(u); m != nil {
			if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				k.apps[id] = true
			}
		}
	}
	if err := rows.Close(); err != nil {
		return k, err
	}
	if rows, err = d.Query(`SELECT itad_id FROM deal_match WHERE itad_id IS NOT NULL`); err != nil {
		return k, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return k, err
		}
		k.itad[id] = true
	}
	return k, rows.Err()
}

func (k libKeys) has(r wishRow) bool {
	if k.apps[r.appid] || (r.itadID != "" && k.itad[r.itadID]) {
		return true
	}
	return r.titled && k.titles[normTitle(r.title)]
}

// wishRow is a wish_item with its cached price.
type wishRow struct {
	appid    int64
	itadID   string
	slug     string
	title    string
	titled   bool // the title came from ITAD (not the placeholder)
	addedAt  string
	matched  string // matched_at, "" = never looked up
	hidden   bool
	muted    bool
	lastP    *int64
	lastKind string
	lastAt   string
	seen     bool
	pending  bool
	price    *PriceData
}

func loadWishRows(d *sql.DB) ([]wishRow, error) {
	rows, err := d.Query(`SELECT w.appid, coalesce(w.itad_id, ''), w.itad_slug, w.title, coalesce(w.added_at, ''), coalesce(w.matched_at, ''),
		w.hidden_at IS NOT NULL, w.muted, w.last_alert_price, coalesce(w.last_alert_kind, ''), coalesce(w.last_alert_at, ''), w.seen, w.pending,
		p.currency, p.cur_shop, p.cur_price, p.cur_regular, p.cur_cut, p.cur_url, p.cur_expiry, p.low_price, p.low_shop, p.low_at, p.low2y_price, p.fetched_at
		FROM wish_item w LEFT JOIN wish_price p ON p.appid = w.appid ORDER BY w.appid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wishRow
	for rows.Next() {
		var r wishRow
		var hidden, muted, seen, pending int
		var currency, fetched sql.NullString
		var pd PriceData
		if err := rows.Scan(&r.appid, &r.itadID, &r.slug, &r.title, &r.addedAt, &r.matched, &hidden, &muted, &r.lastP, &r.lastKind, &r.lastAt, &seen, &pending,
			&currency, &pd.Shop, &pd.Price, &pd.Regular, &pd.Cut, &pd.URL, &pd.Expiry, &pd.LowPrice, &pd.LowShop, &pd.LowAt, &pd.Low2y, &fetched); err != nil {
			return nil, err
		}
		r.hidden, r.muted, r.seen, r.pending = hidden == 1, muted == 1, seen == 1, pending == 1
		r.titled = r.title != placeholderTitle(r.appid)
		if currency.Valid {
			pd.Currency, pd.FetchedAt = currency.String, fetched.String
			r.price = &pd
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// activeWish lists the wishlist games Deals works on: not hidden and not already in the library.
func activeWish(d *sql.DB) ([]wishRow, error) {
	all, err := loadWishRows(d)
	if err != nil {
		return nil, err
	}
	keys, err := loadLibKeys(d)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, r := range all {
		if !r.hidden && !keys.has(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// WishAlert is a standing alert of a wishlist game.
type WishAlert struct {
	Kind  string `json:"kind"`
	Price int64  `json:"price"`
	At    string `json:"at"`
	Seen  bool   `json:"seen"`
}

// WishItem is a wishlist game as the Deals page shows it.
type WishItem struct {
	AppID   int64      `json:"appid"`
	ITADID  string     `json:"itad_id"` // "" = ITAD has no such game (or it was not looked up yet: see Matched)
	Matched bool       `json:"matched"` // the lookup was made
	Title   string     `json:"title"`
	Slug    string     `json:"slug"`
	AddedAt string     `json:"added_at"`
	Price   *PriceData `json:"price"`
	Alert   *WishAlert `json:"alert"`
	Muted   bool       `json:"muted"`
	Image   string     `json:"image"` // file under /covers/ ("steam/123.jpg"), "" until downloaded (the API fills it; see ImageName)
}

// Wishlist is the section: the games shown, the hidden ones, and what the last read said.
type Wishlist struct {
	Items    []WishItem `json:"items"`
	Hidden   []WishItem `json:"hidden"`
	Status   string     `json:"status"`    // why the last read gave nothing new ("" = fine)
	SyncedAt string     `json:"synced_at"` // when the wishlist was last read from Steam
	Unseen   int        `json:"-"`
}

func loadWishlist(d *sql.DB) (Wishlist, error) {
	w := Wishlist{Items: []WishItem{}, Hidden: []WishItem{}}
	rows, err := loadWishRows(d)
	if err != nil {
		return w, err
	}
	keys, err := loadLibKeys(d)
	if err != nil {
		return w, err
	}
	for _, r := range rows {
		if keys.has(r) {
			continue
		}
		it := WishItem{AppID: r.appid, ITADID: r.itadID, Matched: r.matched != "", Title: r.title, Slug: r.slug, AddedAt: r.addedAt, Price: r.price, Muted: r.muted}
		if r.hidden {
			w.Hidden = append(w.Hidden, it)
			continue
		}
		if !r.muted && r.lastKind != "" && r.price != nil && r.price.Price != nil && r.lastP != nil && *r.price.Price <= *r.lastP {
			it.Alert = &WishAlert{Kind: r.lastKind, Price: *r.lastP, At: r.lastAt, Seen: r.seen}
			if !r.seen {
				w.Unseen++
			}
		}
		w.Items = append(w.Items, it)
	}
	w.Status, w.SyncedAt = settingValue(d, "wish_status"), settingValue(d, "wish_synced_at")
	return w, nil
}

func settingValue(d *sql.DB, key string) string {
	var v string
	d.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	return v
}

func setSetting(d *sql.DB, key, value string) error {
	_, err := d.Exec(`INSERT INTO setting(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetWishHidden hides a wishlist game from Deals (or shows it again). A hidden game is not refreshed.
func SetWishHidden(d *sql.DB, appid int64, hide bool) error {
	var res sql.Result
	var err error
	if hide {
		res, err = d.Exec(`UPDATE wish_item SET hidden_at = coalesce(hidden_at, ?) WHERE appid = ?`, now(), appid)
	} else {
		res, err = d.Exec(`UPDATE wish_item SET hidden_at = NULL WHERE appid = ?`, appid)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUnknownWish
	}
	return nil
}

// SetWishMuted turns the alerts of a wishlist game off (or on again).
func SetWishMuted(d *sql.DB, appid int64, mute bool) error {
	res, err := d.Exec(`UPDATE wish_item SET muted = ? WHERE appid = ?`, b2i(mute), appid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUnknownWish
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------- refresh ----------------

// syncWishlist stores the wishlist as Steam gave it: new games are added, the ones no longer on it (bought, or removed)
// are deleted with their prices and alerts. It is only called with a non-empty list, so a private or failing read never
// empties the section.
func syncWishlist(d *sql.DB, entries []WishEntry) (removed int, err error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	keep := make(map[int64]bool, len(entries))
	for _, e := range entries {
		keep[e.AppID] = true
		var added any
		if e.AddedAt != "" {
			added = e.AddedAt
		}
		if _, err := tx.Exec(`INSERT INTO wish_item(appid, title, added_at, seen_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(appid) DO UPDATE SET added_at = excluded.added_at, seen_at = excluded.seen_at`, e.AppID, placeholderTitle(e.AppID), added, t); err != nil {
			return 0, err
		}
	}
	rows, err := tx.Query(`SELECT appid FROM wish_item`)
	if err != nil {
		return 0, err
	}
	var gone []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range gone {
		if _, err := tx.Exec(`DELETE FROM wish_item WHERE appid = ?`, id); err != nil {
			return 0, err
		}
	}
	return len(gone), tx.Commit()
}

// matchWish looks up the ITAD game of a wishlist game by its Steam app id. A game ITAD does not know is looked up again
// after a week.
func matchWish(ctx context.Context, d *sql.DB, src Source, r wishRow) error {
	g, ok, err := src.LookupAppID(ctx, r.appid)
	if err != nil {
		return err
	}
	if !ok {
		_, err = d.Exec(`UPDATE wish_item SET itad_id = NULL, matched_at = ? WHERE appid = ?`, now(), r.appid)
		return err
	}
	title := g.Title
	if title == "" {
		title = placeholderTitle(r.appid)
	}
	_, err = d.Exec(`UPDATE wish_item SET itad_id = ?, title = ?, itad_slug = ?, matched_at = ? WHERE appid = ?`, g.ID, title, g.Slug, now(), r.appid)
	return err
}

func (r wishRow) needsMatch() bool {
	if r.matched == "" {
		return true
	}
	if r.itadID != "" {
		return false
	}
	at, err := time.Parse(timeFmt, r.matched)
	return err != nil || time.Since(at) > retryUnmatched
}

// refreshWishlist reads the wishlist, matches new games with ITAD and reads the prices of all, like the library's refresh
// (same pacing through src, same stale-write guards). A Steam problem is reported in rep.WishNote and never fails the run.
// done/total carry on the progress of the library part.
func refreshWishlist(ctx context.Context, d *sql.DB, src Source, opt Options, rep Report, done, total int) (Report, error) {
	stop := func(err error) (Report, error) {
		var rl *RateLimitError
		if errors.As(err, &rl) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			rep.Stopped = err.Error()
			return rep, nil
		}
		return rep, err
	}
	entries, err := opt.Wishlist.Wishlist(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return stop(ctx.Err())
		}
		if errors.Is(err, ErrWishlistEmpty) {
			rep.WishNote = err.Error()
		} else {
			rep.WishNote = "Steam wishlist: " + err.Error() // Steam errors never carry the SteamID (see Steam.Wishlist)
		}
		if e := setSetting(d, "wish_status", rep.WishNote); e != nil {
			return rep, e
		}
		return rep, nil
	}
	if _, err := syncWishlist(d, entries); err != nil {
		return rep, err
	}
	if err := setSetting(d, "wish_status", ""); err != nil {
		return rep, err
	}
	if err := setSetting(d, "wish_synced_at", now()); err != nil {
		return rep, err
	}
	active, err := activeWish(d)
	if err != nil {
		return rep, err
	}
	rep.WishGames = len(active)
	var todo []wishRow
	for _, r := range active {
		if r.needsMatch() {
			todo = append(todo, r)
		}
	}
	total += len(todo) + len(active)
	progress := func() {
		if opt.Progress != nil {
			opt.Progress(done, total)
		}
	}
	progress()
	nameWishGames(ctx, d, opt.Wishlist, active)
	syncWishImages(ctx, opt.Wishlist, active)
	// Matching, in bulk when the source can. A rate limit here only ends the matching: what is matched is still priced
	// below, and the games not looked up yet wait for the next run.
	var cut error
	if bulk, ok := src.(BulkSource); ok {
		cut = matchWishBulk(ctx, d, bulk, todo, func(n int) { done += n; progress() })
	} else {
		for _, r := range todo {
			if cut = matchWish(ctx, d, src, r); cut != nil {
				break
			}
			done++
			progress()
		}
	}
	// Whatever ended the matching (a rate limit, a timeout, an error of the lookup), what is matched is still priced below
	// and the problem is reported at the end.
	// What was matched may now be a library game (same ITAD id): look again before reading prices.
	before := len(active)
	if active, err = activeWish(d); err != nil {
		return rep, err
	}
	total -= before - len(active) // games that turned out to be in the library are not priced here
	rep.WishGames = len(active)
	byID := map[string][]int64{}
	var ids []string
	for _, r := range active {
		if r.itadID == "" {
			if r.matched != "" {
				rep.WishUnmatched++
			}
			done++
			continue
		}
		if _, seen := byID[r.itadID]; !seen {
			ids = append(ids, r.itadID)
		}
		byID[r.itadID] = append(byID[r.itadID], r.appid)
	}
	progress()
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
			for _, appid := range byID[id] {
				ok, err := saveWishPrice(d, appid, id, country, res[id])
				if err != nil {
					return rep, err
				}
				if ok {
					rep.WishPriced++
				}
				done++
			}
		}
		progress()
	}
	if cut != nil {
		if isStop(cut) {
			return stop(cut)
		}
		rep.Stopped = "matching the wishlist failed: " + cut.Error() // the lookup's errors are fixed texts, never the key
	}
	return rep, nil
}

// isStop: an error that ends a run with what it has saved (a rate limit, a timeout or a cancel), not a failure.
func isStop(err error) bool {
	var rl *RateLimitError
	return errors.As(err, &rl) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// nameWishGames gives the games that still carry the placeholder title the name Steam has for them, one request per
// MaxTitleBatch games. A source that cannot name games, or an error, leaves the placeholder; a game Steam cannot name
// (delisted, region-locked) keeps "Steam app <id>" and is asked about again at the next run (one cheap Steam request).
func nameWishGames(ctx context.Context, d *sql.DB, wl WishlistSource, active []wishRow) {
	ts, ok := wl.(TitleSource)
	if !ok {
		return
	}
	var need []int64
	for _, r := range active {
		if !r.titled {
			need = append(need, r.appid)
		}
	}
	for len(need) > 0 {
		n := min(len(need), MaxTitleBatch)
		names, err := ts.Titles(ctx, need[:n])
		need = need[n:]
		if err != nil {
			return
		}
		for id, name := range names {
			if _, err := d.Exec(`UPDATE wish_item SET title = ? WHERE appid = ? AND title = ?`, name, id, placeholderTitle(id)); err != nil {
				return
			}
		}
	}
}

// syncWishImages brings the pictures of the active games into the local cache (a few batched requests, only for the games
// that have no picture yet). A source without pictures, or an error, leaves the coloured initials.
func syncWishImages(ctx context.Context, wl WishlistSource, active []wishRow) {
	is, ok := wl.(ImageSource)
	if !ok {
		return
	}
	ids := make([]int64, 0, len(active))
	for _, r := range active {
		ids = append(ids, r.appid)
	}
	is.SyncImages(ctx, ids)
}

// matchWishBulk looks up the ITAD ids of the games in todo with the bulk endpoint, MaxLookupBatch per request. It stops at the
// first error and returns it; what earlier chunks found is saved. counted is told how many games each chunk settled.
func matchWishBulk(ctx context.Context, d *sql.DB, bulk BulkSource, todo []wishRow, counted func(int)) error {
	for len(todo) > 0 {
		n := min(len(todo), MaxLookupBatch)
		chunk := todo[:n]
		todo = todo[n:]
		ids := make([]int64, len(chunk))
		for i, r := range chunk {
			ids[i] = r.appid
		}
		found, err := bulk.LookupAppIDs(ctx, ids)
		if err != nil {
			return err
		}
		for _, r := range chunk {
			if itad, ok := found[r.appid]; ok {
				// The title (from Steam) stays; a game Steam could not name keeps the placeholder.
				_, err = d.Exec(`UPDATE wish_item SET itad_id = ?, matched_at = ? WHERE appid = ?`, itad, now(), r.appid)
			} else {
				_, err = d.Exec(`UPDATE wish_item SET itad_id = NULL, matched_at = ? WHERE appid = ?`, now(), r.appid)
			}
			if err != nil {
				return err
			}
		}
		counted(len(chunk))
	}
	return nil
}

// saveWishPrice is savePrice for a wishlist game: it does nothing (and says false) unless the game is still on the
// wishlist, still matched with that ITAD game, not hidden, and the country is still the one the prices were read for.
func saveWishPrice(d *sql.DB, appid int64, itadID, country string, o Overview) (bool, error) {
	currency, shop, price, regular, cut, curURL, expiry, lowPrice, lowShop, lowAt := priceArgs(o)
	res, err := d.Exec(`INSERT INTO wish_price(appid, currency, cur_shop, cur_price, cur_regular, cur_cut, cur_url, cur_expiry,
		low_price, low_shop, low_at, fetched_at) SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM wish_item WHERE appid = ?1 AND itad_id = ?13 AND hidden_at IS NULL)
		AND coalesce((SELECT value FROM setting WHERE key = 'deals_country'), 'ES') = ?14
		ON CONFLICT(appid) DO UPDATE SET currency = excluded.currency, cur_shop = excluded.cur_shop, cur_price = excluded.cur_price,
		cur_regular = excluded.cur_regular, cur_cut = excluded.cur_cut, cur_url = excluded.cur_url, cur_expiry = excluded.cur_expiry,
		low_price = excluded.low_price, low_shop = excluded.low_shop, low_at = excluded.low_at, fetched_at = excluded.fetched_at`,
		appid, currency, shop, price, regular, cut, curURL, expiry, lowPrice, lowShop, lowAt, now(), itadID, country)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------------- alerts ----------------

// evaluateWish is Evaluate for the wishlist: same rule, same cooldown, recorded in wish_item.
func evaluateWish(d *sql.DB, now, fetchedSince time.Time) ([]Alert, error) {
	active, err := activeWish(d)
	if err != nil {
		return nil, err
	}
	var out []Alert
	for _, r := range active {
		p := r.price
		if p == nil {
			continue
		}
		kind := alertKind(p.Price, p.Cut, p.LowPrice, p.Low2y)
		if kind == "" || !alertDue(r.muted, p.FetchedAt, deref(p.Expiry), *p.Price, r.lastP, r.lastAt, now, fetchedSince) {
			continue
		}
		out = append(out, Alert{AppID: r.appid, Title: r.title, Kind: kind, Price: *p.Price, Currency: p.Currency, Cut: *p.Cut, Shop: deref(p.Shop), URL: deref(p.URL), Regular: deref64(p.Regular), Low: deref64(p.LowPrice), LowAt: deref(p.LowAt)})
	}
	for _, a := range out {
		if _, err := d.Exec(`UPDATE wish_item SET last_alert_price = ?, last_alert_kind = ?, last_alert_at = ?, seen = 0, pending = 1 WHERE appid = ?`,
			a.Price, a.Kind, now.UTC().Format(timeFmt), a.AppID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func deref64(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pendingWishAlerts: the wishlist alerts that still have to reach Telegram and still stand.
func pendingWishAlerts(d *sql.DB, now time.Time) ([]Alert, error) {
	active, err := activeWish(d)
	if err != nil {
		return nil, err
	}
	var out []Alert
	for _, r := range active {
		p := r.price
		if !r.pending || r.muted || r.lastKind == "" || p == nil || p.Price == nil || r.lastP == nil || *p.Price > *r.lastP || expired(deref(p.Expiry), now) {
			continue
		}
		cut := 0
		if p.Cut != nil {
			cut = *p.Cut
		}
		out = append(out, Alert{AppID: r.appid, Title: r.title, Kind: r.lastKind, Price: *p.Price, Currency: p.Currency, Cut: cut, Shop: deref(p.Shop), URL: deref(p.URL), Regular: deref64(p.Regular), Low: deref64(p.LowPrice), LowAt: deref(p.LowAt)})
	}
	return out, nil
}

// fillWishTwoYearLows is fillTwoYearLows for the wishlist: the price history is read only for a game on sale above its
// historical low whose two-year low was not read within a week.
func fillWishTwoYearLows(ctx context.Context, d *sql.DB, hs HistorySource, now time.Time, budget *int) error {
	active, err := activeWish(d)
	if err != nil {
		return err
	}
	var todo []wishRow
	cutoff := now.Add(-historyTTL).UTC().Format(timeFmt)
	for _, r := range active {
		p := r.price
		if r.itadID == "" || p == nil || p.Price == nil || p.Cut == nil || *p.Cut <= 0 || p.LowPrice == nil || *p.Price <= *p.LowPrice {
			continue
		}
		var checked sql.NullString
		d.QueryRow(`SELECT low2y_checked_at FROM wish_price WHERE appid = ?`, r.appid).Scan(&checked)
		if checked.Valid && checked.String >= cutoff {
			continue
		}
		todo = append(todo, r)
	}
	country := Country(d)
	for _, r := range todo {
		if *budget <= 0 {
			return nil
		}
		*budget--
		low, ok, err := hs.LowSince(ctx, r.itadID, country, now.Add(-HistoryWindow))
		if err != nil {
			var rl *RateLimitError
			if errors.As(err, &rl) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		var cents any
		if ok && low.Currency == r.price.Currency {
			cents = low.Cents
		}
		if _, err := d.Exec(`UPDATE wish_price SET low2y_price = ?, low2y_checked_at = ? WHERE appid = ?
			AND EXISTS (SELECT 1 FROM wish_item WHERE appid = ?3 AND itad_id = ?4 AND hidden_at IS NULL)
			AND coalesce((SELECT value FROM setting WHERE key = 'deals_country'), 'ES') = ?5`,
			cents, now.UTC().Format(timeFmt), r.appid, r.itadID, country); err != nil {
			return err
		}
	}
	return nil
}

func (r Report) wishString() string {
	switch {
	case r.WishNote != "":
		return " " + r.WishNote
	case r.WishGames > 0:
		s := fmt.Sprintf(" Wishlist: %d %s priced", r.WishPriced, plural(r.WishPriced, "game", "games"))
		if rest := r.WishGames - r.WishPriced - r.WishUnmatched; rest > 0 { // a rate limit cut the run short
			s = fmt.Sprintf(" Wishlist: %d of %d priced; the rest next run", r.WishPriced, r.WishGames-r.WishUnmatched)
		}
		if r.WishUnmatched > 0 {
			s += fmt.Sprintf(", %d without a match", r.WishUnmatched)
		}
		return s + "."
	}
	return ""
}
