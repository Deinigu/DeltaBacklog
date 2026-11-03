package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSteamID = "76561198000000000"

type fakeWish struct {
	entries []WishEntry
	err     error
	calls   int
}

func (f *fakeWish) Wishlist(context.Context) ([]WishEntry, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func entries(ids ...int64) []WishEntry {
	var out []WishEntry
	for _, id := range ids {
		out = append(out, WishEntry{AppID: id, AddedAt: "2025-05-01T10:00:00Z"})
	}
	return out
}

// wishSource: ITAD knows apps 10 (Hollow Knight) and 20 (Celeste) and not app 30.
func wishSource() *histSource {
	return &histSource{
		fakeSource: &fakeSource{
			byApp: map[int64]Game{10: {ID: "w10", Slug: "hollow-knight", Title: "Hollow Knight"}, 20: {ID: "w20", Slug: "celeste", Title: "Celeste"}},
			prices: map[string]Overview{
				"w10": {Current: cur(500, 1500, 66), Low: &Low{Shop: "GOG", Price: eur(500), At: "2024-01-01T00:00:00Z"}},
				"w20": {Current: cur(1500, 2000, 25), Low: &Low{Shop: "GOG", Price: eur(500), At: "2024-01-01T00:00:00Z"}},
			},
		},
		lows: map[string]int64{},
	}
}

func steamClient(t *testing.T, h http.HandlerFunc) *Steam {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewSteam(testSteamID)
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = srv.URL
	return c
}

func TestSteamWishlist(t *testing.T) {
	ctx := context.Background()
	var mode = "public"
	c := steamClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/IWishlistService/GetWishlist/v1/" || r.URL.Query().Get("steamid") != testSteamID || r.URL.Query().Get("key") != "" {
			t.Errorf("request %s", r.URL)
		}
		switch mode {
		case "public":
			w.Write([]byte(`{"response":{"items":[{"appid":10,"priority":0,"date_added":1700000000},{"appid":20,"date_added":0},{"appid":10},{"appid":-5}]}}`))
		case "private":
			w.Write([]byte(`{"response":{}}`))
		case "empty":
			w.Write([]byte(`{"response":{"items":[]}}`))
		case "json":
			w.Write([]byte(`<html>`))
		case "500":
			w.WriteHeader(500)
		case "redirect":
			http.Redirect(w, r, "https://evil.example/?steamid="+testSteamID, http.StatusFound)
		}
	})
	got, err := c.Wishlist(ctx)
	if err != nil || len(got) != 2 || got[0].AppID != 10 || got[0].AddedAt != "2023-11-14T22:13:20Z" || got[1].AppID != 20 || got[1].AddedAt != "" {
		t.Fatalf("public wishlist: %+v %v", got, err)
	}
	for _, m := range []string{"private", "empty"} {
		mode = m
		if _, err := c.Wishlist(ctx); !errors.Is(err, ErrWishlistEmpty) {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, m := range []string{"json", "500", "redirect"} {
		mode = m
		if _, err := c.Wishlist(ctx); err == nil || strings.Contains(err.Error(), testSteamID) || errors.Is(err, ErrWishlistEmpty) {
			t.Errorf("%s: %v", m, err)
		}
	}
	// An unreachable server: the transport error carries the address, and with it the id; ours must not.
	dead := httptest.NewServer(http.NotFoundHandler())
	c.BaseURL = dead.URL
	dead.Close()
	if _, err := c.Wishlist(ctx); err == nil || strings.Contains(err.Error(), testSteamID) {
		t.Errorf("unreachable: %v", err)
	}
	c.BaseURL = "https://evil.example"
	if _, err := c.Wishlist(ctx); err == nil || strings.Contains(err.Error(), testSteamID) || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("a foreign host must be refused: %v", err)
	}
}

func TestNewSteamValidatesTheID(t *testing.T) {
	if c, err := NewSteam(""); c != nil || err != nil {
		t.Errorf("no id means no wishlist: %v %v", c, err)
	}
	if c, err := NewSteam("  " + testSteamID + "\n"); err != nil || c == nil {
		t.Errorf("surrounding space is fine: %v", err)
	}
	for _, bad := range []string{"1234", "7656119800000000x", "765611980000000001", "https://steamcommunity.com/id/me", "7656 1198000000000"} {
		c, err := NewSteam(bad)
		if c != nil || !errors.Is(err, ErrBadSteamID) || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: %v %v", bad, c, err)
		}
	}
}

func wishRowsOf(t *testing.T, d *sql.DB) map[int64]WishItem {
	t.Helper()
	l, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]WishItem{}
	for _, it := range append(append([]WishItem{}, l.Wish.Items...), l.Wish.Hidden...) {
		out[it.AppID] = it
	}
	return out
}

func TestRefreshReadsTheWishlist(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20, 30)}
	var last [2]int
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: wl, Progress: func(done, total int) { last = [2]int{done, total} }})
	if err != nil || rep.Stopped != "" || rep.WishGames != 3 || rep.WishPriced != 2 || rep.WishUnmatched != 1 || rep.WishNote != "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if !strings.Contains(rep.String(), "Wishlist: 2 games priced, 1 without a match") {
		t.Errorf("report: %s", rep)
	}
	if last[0] != last[1] || last[1] == 0 {
		t.Errorf("progress ended at %v", last)
	}
	items := wishRowsOf(t, d)
	if len(items) != 3 {
		t.Fatalf("items: %+v", items)
	}
	hk := items[10]
	if hk.Title != "Hollow Knight" || hk.ITADID != "w10" || hk.Slug != "hollow-knight" || hk.AddedAt != "2025-05-01T10:00:00Z" || hk.Price == nil || *hk.Price.Price != 500 || *hk.Price.LowPrice != 500 {
		t.Errorf("hollow knight: %+v %+v", hk, hk.Price)
	}
	if un := items[30]; un.Title != "Steam app 30" || un.ITADID != "" || un.Price != nil || !un.Matched {
		t.Errorf("unmatched: %+v", un)
	}
	l, _ := Load(d)
	if l.Wish.Status != "" || l.Wish.SyncedAt == "" || len(l.Watched) != 0 || len(l.Prices) != 0 {
		t.Errorf("status %q synced %q, library sections must stay empty: %+v", l.Wish.Status, l.Wish.SyncedAt, l)
	}
	// Nothing about the wishlist reaches the library.
	var games int
	d.QueryRow(`SELECT count(*) FROM game`).Scan(&games)
	if games != 0 {
		t.Errorf("wishlist entries must not be games: %d", games)
	}
	// A second run does not look up again what it knows (the unmatched one waits a week).
	lookups := src.lookups
	if _, err := Refresh(context.Background(), d, src, Options{Wishlist: wl}); err != nil || src.lookups != lookups {
		t.Errorf("lookups %d -> %d, %v", lookups, src.lookups, err)
	}
	// ...but retries it after a week.
	d.Exec(`UPDATE wish_item SET matched_at = ? WHERE appid = 30`, time.Now().Add(-8*24*time.Hour).UTC().Format(timeFmt))
	if _, err := Refresh(context.Background(), d, src, Options{Wishlist: wl}); err != nil || src.lookups != lookups+1 {
		t.Errorf("stale no-match: lookups %d -> %d, %v", lookups, src.lookups, err)
	}
}

func TestWishlistWithoutOptionIsLeftAlone(t *testing.T) {
	d := newDB(t)
	rep, err := Refresh(context.Background(), d, wishSource(), Options{})
	if err != nil || rep.WishGames != 0 || strings.Contains(rep.String(), "ishlist") {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestBoughtGamesLeaveTheWishlist(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20)}
	ctx := context.Background()
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE wish_item SET muted = 1, last_alert_price = 500, last_alert_kind = 'historical', last_alert_at = '2026-01-01T00:00:00Z' WHERE appid = 20`)
	wl.entries = entries(10) // 20 was bought: Steam takes it off the wishlist
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	items := wishRowsOf(t, d)
	if len(items) != 1 || items[10].Title != "Hollow Knight" {
		t.Fatalf("items: %+v", items)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM wish_price WHERE appid = 20`).Scan(&n)
	if n != 0 {
		t.Error("the prices of a removed game must go with it")
	}
}

func TestPrivateOrEmptyWishlistKeepsWhatIsStored(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20)}
	ctx := context.Background()
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	wl.entries, wl.err = nil, ErrWishlistEmpty
	rep, err := Refresh(ctx, d, src, Options{Wishlist: wl})
	if err != nil || rep.WishNote == "" || !strings.Contains(rep.String(), "not public") {
		t.Fatalf("%+v %v", rep, err)
	}
	if l, _ := Load(d); len(l.Wish.Items) != 2 || !strings.Contains(l.Wish.Status, "not public") {
		t.Errorf("a private read must not empty the section: %+v", l.Wish)
	}
	// A Steam outage is a note too, not a failed run, and a good read clears it.
	wl.err = errors.New("Steam could not be reached")
	if rep, err = Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil || !strings.Contains(rep.WishNote, "Steam could not be reached") {
		t.Fatalf("%+v %v", rep, err)
	}
	wl.err, wl.entries = nil, entries(10, 20)
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	if l, _ := Load(d); l.Wish.Status != "" {
		t.Errorf("status after a good read: %q", l.Wish.Status)
	}
	// The library part still ran: a Steam problem does not stop it.
	addGame(t, d, 1, "Hades", "backlog", "")
	src.byTitle = map[string]Game{"Hades": {ID: "h", Title: "Hades"}}
	src.prices["h"] = Overview{Current: cur(1000, 2000, 50)}
	wl.err = ErrWishlistEmpty
	if rep, err = Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil || rep.Priced != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestLibraryGamesHideTheirWishlistTwin(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	src.byApp[40] = Game{ID: "w40", Title: "Hades"}
	src.byApp[50] = Game{ID: "w50", Title: "Outer Wilds"}
	src.prices["w40"] = Overview{Current: cur(900, 2000, 55)}
	src.prices["w50"] = Overview{Current: cur(900, 2000, 55)}
	wl := &fakeWish{entries: entries(10, 20, 30, 40, 50)}
	ctx := context.Background()
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	if got := wishRowsOf(t, d); len(got) != 5 {
		t.Fatalf("before: %d", len(got))
	}
	// The owner adds games from the wishlist: one by Steam link, one by title only (Owned, so Deals never matches it),
	// one that Deals matched by ITAD id.
	addGame(t, d, 1, "Whatever", "finished", "https://store.steampowered.com/app/10/Hollow_Knight/")
	addGame(t, d, 2, "Hades", "finished", "")
	addGame(t, d, 3, "Another Name", "backlog", "")
	d.Exec(`INSERT INTO deal_match(game_id, itad_id, source) VALUES (3, 'w20', 'user')`)
	got := wishRowsOf(t, d)
	if len(got) != 2 || got[30].AppID == 0 || got[50].AppID == 0 {
		t.Fatalf("only the games not in the library stay: %+v", got)
	}
	// A hidden twin is not listed in Hidden either, and nothing is refreshed or alerted for it.
	SetWishHidden(d, 40, true)
	if l, _ := Load(d); len(l.Wish.Hidden) != 0 {
		t.Errorf("hidden: %+v", l.Wish.Hidden)
	}
	src.fakeSource.overview = nil
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	for _, batch := range src.overview {
		for _, id := range batch {
			if id == "w10" || id == "w40" { // w20 is priced as the library game 3 itself
				t.Errorf("prices asked for %s, a library game or hidden", id)
			}
		}
	}
}

func TestWishlistHideMuteAndGuards(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20)}
	ctx := context.Background()
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	if err := SetWishHidden(d, 999, true); !errors.Is(err, ErrUnknownWish) {
		t.Errorf("unknown: %v", err)
	}
	if err := SetWishMuted(d, 999, true); !errors.Is(err, ErrUnknownWish) {
		t.Errorf("unknown: %v", err)
	}
	if err := SetWishHidden(d, 20, true); err != nil {
		t.Fatal(err)
	}
	l, _ := Load(d)
	if len(l.Wish.Items) != 1 || len(l.Wish.Hidden) != 1 || l.Wish.Hidden[0].AppID != 20 {
		t.Fatalf("hide: %+v", l.Wish)
	}
	// A hidden game is not read again...
	src.fakeSource.overview = nil
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	if len(src.overview) != 1 || len(src.overview[0]) != 1 || src.overview[0][0] != "w10" {
		t.Errorf("overview calls: %v", src.overview)
	}
	// ...and a price read before it was hidden cannot land afterwards (stale-write guard).
	if ok, err := saveWishPrice(d, 20, "w20", "ES", src.prices["w20"]); err != nil || ok {
		t.Errorf("hidden game saved: %v %v", ok, err)
	}
	SetWishHidden(d, 20, false)
	if ok, err := saveWishPrice(d, 20, "other", "ES", src.prices["w20"]); err != nil || ok {
		t.Errorf("another ITAD id saved: %v %v", ok, err)
	}
	if ok, err := saveWishPrice(d, 20, "w20", "FR", src.prices["w20"]); err != nil || ok {
		t.Errorf("another country saved: %v %v", ok, err)
	}
	if ok, err := saveWishPrice(d, 20, "w20", "ES", src.prices["w20"]); err != nil || !ok {
		t.Errorf("a good write: %v %v", ok, err)
	}
	if err := SetCountry(d, "FR"); err != nil {
		t.Fatal(err)
	}
	if l, _ = Load(d); l.Wish.Items[0].Price != nil || l.Wish.Items[1].Price != nil {
		t.Errorf("a new country drops the cached prices: %+v", l.Wish.Items)
	}
}

func TestWishlistAlerts(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20)}
	sk := &sink{}
	ctx := context.Background()
	res, err := CheckWith(ctx, d, src, wl, sk)
	if err != nil || len(res.Alerts) != 1 || res.Alerts[0].AppID != 10 || res.Alerts[0].GameID != 0 || res.Alerts[0].Kind != KindHistorical || !res.Sent {
		t.Fatalf("%+v %v", res, err)
	}
	if len(sk.sent) != 1 || !strings.Contains(sk.sent[0], "Hollow Knight") || !strings.Contains(sk.sent[0], "historical low") || !strings.Contains(sk.sent[0], "[wishlist]") || !strings.Contains(sk.sent[0], "https://shop.example/x") {
		t.Errorf("message: %q", sk.sent)
	}
	l, _ := Load(d)
	if l.Wish.Unseen != 1 || l.Wish.Items[0].Alert == nil || l.Wish.Items[0].Alert.Kind != KindHistorical || l.Wish.Items[0].Alert.Seen || l.Wish.Items[1].Alert != nil {
		t.Fatalf("standing alert: %+v", l.Wish.Items)
	}
	var pending int
	d.QueryRow(`SELECT pending FROM wish_item WHERE appid = 10`).Scan(&pending)
	if pending != 0 {
		t.Error("a sent alert is no longer pending")
	}
	// Same deal, same price: not announced twice.
	if res, _ = CheckWith(ctx, d, src, wl, sk); len(res.Alerts) != 0 || len(sk.sent) != 1 {
		t.Errorf("repeat: %+v %v", res.Alerts, sk.sent)
	}
	if err := MarkSeen(d); err != nil {
		t.Fatal(err)
	}
	if l, _ = Load(d); l.Wish.Unseen != 0 || !l.Wish.Items[0].Alert.Seen {
		t.Errorf("seen: %+v", l.Wish)
	}
	// The two-year low, read from the history, alerts a game that is not at its all-time low.
	src.lows["w20"] = 1500
	d.Exec(`UPDATE wish_item SET last_alert_price = NULL WHERE appid = 10`)
	d.Exec(`UPDATE wish_price SET low2y_checked_at = NULL`)
	src.calls = nil
	if res, err = CheckWith(ctx, d, src, wl, sk); err != nil || len(res.Alerts) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	kinds := map[int64]string{}
	for _, a := range res.Alerts {
		kinds[a.AppID] = a.Kind
	}
	if kinds[20] != KindTwoYear || kinds[10] != KindHistorical || len(src.calls) != 1 || src.calls[0] != "w20" {
		t.Errorf("kinds %v history calls %v", kinds, src.calls)
	}
}

func TestWishlistAlertsRespectMuteHideAndLibrary(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10, 20)}
	ctx := context.Background()
	src.prices["w20"] = Overview{Current: cur(500, 2000, 75), Low: &Low{Shop: "GOG", Price: eur(500)}}
	if _, err := Refresh(ctx, d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	SetWishMuted(d, 10, true)
	SetWishHidden(d, 20, true)
	res, err := CheckWith(ctx, d, src, wl, nil)
	if err != nil || len(res.Alerts) != 0 {
		t.Fatalf("muted and hidden games must not alert: %+v %v", res.Alerts, err)
	}
	SetWishMuted(d, 10, false)
	SetWishHidden(d, 20, false)
	addGame(t, d, 1, "Celeste", "finished", "")
	if res, err = CheckWith(ctx, d, src, wl, nil); err != nil || len(res.Alerts) != 1 || res.Alerts[0].AppID != 10 {
		t.Fatalf("a game in the library alerts as the library game only (here: not at all, it is finished): %+v %v", res.Alerts, err)
	}
	// Without Telegram nothing waits for a bot set up later.
	var pending int
	d.QueryRow(`SELECT count(*) FROM wish_item WHERE pending = 1`).Scan(&pending)
	if pending != 0 {
		t.Errorf("pending without a notifier: %d", pending)
	}
}

func TestWishlistAlertRetriedWhenTelegramFails(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10)}
	sk := &sink{err: errors.New("down")}
	ctx := context.Background()
	if _, err := CheckWith(ctx, d, src, wl, sk); err == nil {
		t.Fatal("a failed delivery must be reported")
	}
	var pending int
	d.QueryRow(`SELECT pending FROM wish_item WHERE appid = 10`).Scan(&pending)
	if pending != 1 {
		t.Fatal("an undelivered alert stays pending")
	}
	sk.err = nil
	res, err := CheckWith(ctx, d, src, wl, sk)
	if err != nil || len(sk.sent) != 1 || !res.Sent {
		t.Fatalf("retry: %v %v %+v", err, sk.sent, res)
	}
}

func TestWishlistAndLibraryAlertsShareMessages(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	src.byApp = map[int64]Game{10: {ID: "w10", Title: "Hollow Knight"}}
	src.prices["w10"] = Overview{Current: cur(500, 1500, 66), Low: &Low{Shop: "GOG", Price: eur(500)}}
	wl := &fakeWish{entries: entries(10)}
	sk := &sink{}
	res, err := CheckWith(context.Background(), d, src, wl, sk)
	if err != nil || len(res.Alerts) != 2 || len(sk.sent) != 2 {
		t.Fatalf("%+v %v %v", res, err, sk.sent)
	}
	var lib, wish int
	for _, a := range res.Alerts {
		if a.AppID > 0 {
			wish++
		} else if a.GameID == 1 {
			lib++
		}
	}
	if lib != 1 || wish != 1 {
		t.Errorf("library %d wishlist %d", lib, wish)
	}
	// Many alerts go out as one list that marks every wishlist game it carries as sent.
	ms := messageGroups([]Alert{{GameID: 1, Title: "A", Price: 1, Currency: "EUR"}, {AppID: 7, Title: "B", Price: 1, Currency: "EUR"},
		{AppID: 8, Title: "C", Price: 1, Currency: "EUR"}, {GameID: 2, Title: "D", Price: 1, Currency: "EUR"}})
	if len(ms) != 1 || len(ms[0].ids) != 2 || len(ms[0].wish) != 2 || !strings.Contains(ms[0].text, "[wishlist]") {
		t.Errorf("list: %+v", ms)
	}
}

// A remake with the same name as a library game is a different game: the library game's Steam link or ITAD match
// tells them apart, so the title alone does not hide it.
func TestSameNameRemakeStaysOnTheWishlist(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	src.byApp[60] = Game{ID: "re4-remake", Title: "Resident Evil 4"}
	src.byApp[61] = Game{ID: "doom-new", Title: "Doom"}
	src.byApp[62] = Game{ID: "prey-new", Title: "Prey"}
	for _, id := range []string{"re4-remake", "doom-new", "prey-new"} {
		src.prices[id] = Overview{Current: cur(900, 2000, 55)}
	}
	wl := &fakeWish{entries: entries(60, 61, 62)}
	if _, err := Refresh(context.Background(), d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	addGame(t, d, 1, "Resident Evil 4", "finished", "https://store.steampowered.com/app/254700/Resident_Evil_4/") // the 2005 game, with its own link
	addGame(t, d, 2, "Doom", "backlog", "")
	d.Exec(`INSERT INTO deal_match(game_id, itad_id, source) VALUES (2, 'doom-1993', 'title')`) // matched to the old one
	addGame(t, d, 3, "Prey", "finished", "")                                                    // nothing to tell them apart: the title decides
	got := wishRowsOf(t, d)
	if len(got) != 2 || got[60].AppID == 0 || got[61].AppID == 0 {
		t.Fatalf("remakes with a library namesake that has a link or a match stay: %+v", got)
	}
}

// With STEAM_ID removed, the wishlist left in the database neither alerts nor goes to Telegram.
func TestRemovedSteamIDSilencesTheWishlist(t *testing.T) {
	d := newDB(t)
	src := wishSource()
	wl := &fakeWish{entries: entries(10)}
	ctx := context.Background()
	if _, err := CheckWith(ctx, d, src, wl, nil); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE wish_item SET last_alert_price = 500, last_alert_kind = 'historical', last_alert_at = ?, seen = 0, pending = 1 WHERE appid = 10`, time.Now().UTC().Format(timeFmt))
	sk := &sink{}
	res, err := CheckWith(ctx, d, src, nil, sk)
	if err != nil || len(res.Alerts) != 0 || len(sk.sent) != 0 {
		t.Fatalf("%+v %v %v", res, err, sk.sent)
	}
	var pending int
	d.QueryRow(`SELECT pending FROM wish_item`).Scan(&pending)
	if pending != 0 {
		t.Error("a pending wishlist alert must be dropped when no wishlist is set up")
	}
}

// ---------------- bulk matching, rate limits and budgets ----------------

// bulkSource is the fake ITAD with the bulk app-id lookup: known maps app ids to ITAD ids; the call number failAt (0-based,
// -1 = never) answers a rate limit.
type bulkSource struct {
	*histSource
	known  map[int64]string
	chunks [][]int64
	failAt int
	failed error // what the failing call answers (default: a rate limit)
}

func (b *bulkSource) LookupAppIDs(_ context.Context, ids []int64) (map[int64]string, error) {
	if len(ids) > MaxLookupBatch {
		return nil, errors.New("too many ids")
	}
	b.chunks = append(b.chunks, ids)
	if len(b.chunks)-1 == b.failAt {
		if b.failed != nil {
			return nil, b.failed
		}
		return nil, &RateLimitError{RetryAfter: 4 * time.Minute}
	}
	out := map[int64]string{}
	for _, id := range ids {
		if v, ok := b.known[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

// namedWish is a wishlist that also names its games (like Steam).
type namedWish struct {
	*fakeWish
	calls [][]int64
}

func (n *namedWish) Titles(_ context.Context, ids []int64) (map[int64]string, error) {
	n.calls = append(n.calls, ids)
	out := map[int64]string{}
	for _, id := range ids {
		out[id] = "Game " + strconv.FormatInt(id, 10)
	}
	return out, nil
}

func manyApps(n int) (*bulkSource, *namedWish) {
	src := &bulkSource{histSource: wishSource(), known: map[int64]string{}, failAt: -1}
	var ids []int64
	for i := int64(1000); i < int64(1000+n); i++ {
		ids = append(ids, i)
		if i%10 != 9 { // one in ten is unknown to ITAD
			src.known[i] = "bulk-" + strconv.FormatInt(i, 10)
			src.prices["bulk-"+strconv.FormatInt(i, 10)] = Overview{Current: cur(900, 2000, 55)}
		}
	}
	return src, &namedWish{fakeWish: &fakeWish{entries: entries(ids...)}}
}

func TestBulkMatchingUsesFewRequests(t *testing.T) {
	d := newDB(t)
	src, wl := manyApps(450)
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: wl})
	if err != nil || rep.Stopped != "" || rep.WishGames != 450 || rep.WishPriced != 405 || rep.WishUnmatched != 45 {
		t.Fatalf("%+v %v", rep, err)
	}
	if src.lookups != 0 {
		t.Errorf("no per-game lookup: %d", src.lookups)
	}
	if len(src.chunks) != 3 || len(src.chunks[0]) != 200 || len(src.chunks[2]) != 50 {
		t.Errorf("lookup chunks: %d", len(src.chunks))
	}
	if len(wl.calls) != 5 || len(wl.calls[0]) != 100 {
		t.Errorf("title calls: %d", len(wl.calls))
	}
	items := wishRowsOf(t, d)
	if it := items[1000]; it.Title != "Game 1000" || it.ITADID != "bulk-1000" || it.Price == nil {
		t.Errorf("item: %+v", it)
	}
	if it := items[1009]; it.Title != "Game 1009" || it.ITADID != "" || !it.Matched || it.Price != nil {
		t.Errorf("unknown to ITAD: %+v", it)
	}
	// Known games are neither named nor looked up again.
	src.chunks, wl.calls = nil, nil
	if _, err := Refresh(context.Background(), d, src, Options{Wishlist: wl}); err != nil || len(src.chunks) != 0 || len(wl.calls) != 0 {
		t.Errorf("second run: %v chunks %d titles %d", err, len(src.chunks), len(wl.calls))
	}
}

func TestRateLimitInTheMatchStepStillPricesWhatIsMatched(t *testing.T) {
	d := newDB(t)
	src, wl := manyApps(450)
	src.failAt = 1 // the second chunk of 200 is refused
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: wl})
	if err != nil || !strings.Contains(rep.Stopped, "rate limit") {
		t.Fatalf("%+v %v", rep, err)
	}
	if rep.WishPriced != 180 || rep.WishUnmatched != 20 {
		t.Fatalf("the first chunk is matched and priced: %+v", rep)
	}
	if !strings.Contains(rep.String(), "Wishlist: 180 of 430 priced; the rest next run, 20 without a match.") {
		t.Errorf("report: %s", rep)
	}
	var waiting int
	d.QueryRow(`SELECT count(*) FROM wish_item WHERE matched_at IS NULL`).Scan(&waiting)
	if waiting != 250 {
		t.Errorf("games not looked up yet: %d", waiting)
	}
	// The next run carries on with the 250 left, in two requests.
	src.failAt, src.chunks = -1, nil
	rep, err = Refresh(context.Background(), d, src, Options{Wishlist: wl})
	if err != nil || rep.Stopped != "" || rep.WishPriced != 405 || len(src.chunks) != 2 || len(src.chunks[0])+len(src.chunks[1]) != 250 {
		t.Fatalf("%+v %v chunks %d", rep, err, len(src.chunks))
	}
}

func TestRateLimitOnTheFirstLookupKeepsTheWishlist(t *testing.T) {
	d := newDB(t)
	src, wl := manyApps(30)
	src.failAt = 0 // refused at once: nothing matched, nothing priced, and the run says so
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: wl})
	if err != nil || rep.Stopped == "" || rep.WishPriced != 0 || rep.WishUnmatched != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	if l, _ := Load(d); len(l.Wish.Items) != 30 || l.Wish.Items[0].Title != "Game 1000" {
		t.Errorf("the games are stored and named even when matching was refused: %d", len(l.Wish.Items))
	}
}

func TestTwoYearLowsStayWithinTheBudget(t *testing.T) {
	d := newDB(t)
	src := &histSource{fakeSource: &fakeSource{byTitle: map[string]Game{}, prices: map[string]Overview{}}, lows: map[string]int64{}}
	for i := 1; i <= 20; i++ {
		title := "Lib " + strconv.Itoa(i)
		id := "lib" + strconv.Itoa(i)
		addGame(t, d, int64(i), title, "backlog", "")
		src.byTitle[title] = Game{ID: id, Title: title}
		setPrice(src, id, cur(700, 1500, 53), 600) // on sale above its historical low: needs the history
	}
	if _, err := Check(context.Background(), d, src, nil); err != nil {
		t.Fatal(err)
	}
	if len(src.calls) != HistoryBudget {
		t.Fatalf("history calls: %d, want %d", len(src.calls), HistoryBudget)
	}
	// The next run reads the rest.
	src.calls = nil
	if _, err := Check(context.Background(), d, src, nil); err != nil || len(src.calls) != 5 {
		t.Errorf("second run: %v %d", err, len(src.calls))
	}
}

func TestClientBulkLookupAndSteamTitles(t *testing.T) {
	ctx := context.Background()
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		var body []string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/lookup/id/shop/61/v1" || len(body) != 3 || body[0] != "app/367520" {
			t.Errorf("request %s %s %v", r.Method, r.URL, body)
		}
		w.Write([]byte(`{"app/367520":"u-hk","app/1145360":null,"app/5":""}`))
	})
	got, err := c.LookupAppIDs(ctx, []int64{367520, 1145360, 5})
	if err != nil || len(got) != 1 || got[367520] != "u-hk" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := c.LookupAppIDs(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
	if _, err := c.LookupAppIDs(ctx, make([]int64, MaxLookupBatch+1)); err == nil {
		t.Error("an oversized batch must be refused")
	}

	var gotInput string
	s := steamClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotInput = r.URL.Query().Get("input_json")
		if r.URL.Path != "/IStoreBrowseService/GetItems/v1/" || strings.Contains(r.URL.RawQuery, testSteamID) {
			t.Errorf("request %s", r.URL)
		}
		w.Write([]byte(`{"response":{"store_items":[{"id":10,"success":1,"name":" Hollow Knight "},{"id":20,"success":2},{"id":30,"success":1,"name":""}]}}`))
	})
	names, err := s.Titles(ctx, []int64{10, 20, 30})
	if err != nil || len(names) != 1 || names[10] != "Hollow Knight" || !strings.Contains(gotInput, `"appid":10`) {
		t.Fatalf("%v %v %q", names, err, gotInput)
	}
	if _, err := s.Titles(ctx, make([]int64, MaxTitleBatch+1)); err == nil {
		t.Error("an oversized batch must be refused")
	}
}
