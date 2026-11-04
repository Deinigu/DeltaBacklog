package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// histSource is the fake ITAD with a price history: lows is the lowest price of the last two years by ITAD id.
type histSource struct {
	*fakeSource
	lows  map[string]int64
	calls []string
	err   error
}

func (h *histSource) LowSince(_ context.Context, id, _ string, since time.Time) (Money, bool, error) {
	h.calls = append(h.calls, id)
	if h.err != nil {
		return Money{}, false, h.err
	}
	if time.Since(since) < HistoryWindow-time.Hour || time.Since(since) > HistoryWindow+time.Hour {
		return Money{}, false, errors.New("the window is not two years")
	}
	c, ok := h.lows[id]
	return eur(c), ok, nil
}

type sink struct {
	mu       sync.Mutex
	sent     []string
	previews []string // the preview address of each sent message
	err      error
	failIf   func(text string) bool // fail only the messages it says yes to
}

func (s *sink) Send(_ context.Context, text, preview string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil || (s.failIf != nil && s.failIf(text)) {
		return errors.New("Telegram could not be reached")
	}
	s.sent = append(s.sent, text)
	s.previews = append(s.previews, preview)
	return nil
}

func cur(price, regular int64, cut int) *Current {
	return &Current{Shop: "Steam", Price: eur(price), Regular: eur(regular), Cut: cut, URL: "https://shop.example/x"}
}

// alertDB: five backlog games, each matched with the ITAD game g<N>.
func alertDB(t *testing.T) (*sql.DB, *histSource) {
	t.Helper()
	d := newDB(t)
	src := &histSource{fakeSource: &fakeSource{byTitle: map[string]Game{}, prices: map[string]Overview{}}, lows: map[string]int64{}}
	for i, title := range []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"} {
		id := int64(i + 1)
		addGame(t, d, id, title, "backlog", "")
		src.byTitle[title] = Game{ID: "g" + string(rune('0'+id)), Title: title}
	}
	return d, src
}

func setPrice(src *histSource, id string, current *Current, low int64) {
	src.prices[id] = Overview{Current: current, Low: &Low{Shop: "GOG", Price: eur(low), At: "2024-01-01T00:00:00Z"}}
}

func TestAlertRule(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500) // at the historical low
	setPrice(src, "g2", cur(700, 1500, 53), 600) // above the historical low, at the two-year low
	src.lows["g2"] = 700
	setPrice(src, "g3", cur(900, 1500, 40), 600) // above both
	src.lows["g3"] = 800
	setPrice(src, "g4", cur(1500, 1500, 0), 1500) // never on sale: at its "low" but no discount
	setPrice(src, "g5", cur(400, 1500, 73), 300)  // above the historical low; two-year low unknown
	sk := &sink{}
	res, err := Check(context.Background(), d, src, sk)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range res.Alerts {
		got[a.Title] = a.Kind
	}
	if len(got) != 2 || got["Alpha"] != KindHistorical || got["Beta"] != KindTwoYear {
		t.Fatalf("alerts %v", got)
	}
	// The history was asked only for games on sale above their historical low: g2, g3, g5 (not g1, not g4).
	if strings.Join(src.calls, ",") != "g2,g3,g5" {
		t.Errorf("history calls %v", src.calls)
	}
	if len(sk.sent) != 2 || !res.Sent || !strings.Contains(sk.sent[0], "Historical low") || !strings.Contains(sk.sent[1], "Lowest in 2 years") ||
		!strings.Contains(sk.sent[0], "€5.00</b>") || !strings.Contains(sk.sent[0], "https://shop.example/x") {
		t.Errorf("messages %q", sk.sent)
	}
	l, _ := Load(d)
	if len(l.Alerts) != 2 || l.Alerts[0].Seen {
		t.Errorf("standing alerts %+v", l.Alerts)
	}
	var low2y sql.NullInt64
	d.QueryRow(`SELECT low2y_price FROM deal_price WHERE game_id = 3`).Scan(&low2y)
	if low2y.Int64 != 800 {
		t.Errorf("two-year low of Gamma: %v", low2y)
	}
	// Gamma has no history low from ITAD at all: the two-year low stays NULL, no alert, and it is not asked again at once.
	d.QueryRow(`SELECT low2y_price FROM deal_price WHERE game_id = 5`).Scan(&low2y)
	if low2y.Valid {
		t.Errorf("Epsilon low2y = %v", low2y)
	}
}

func TestAlertNoRepeat(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	ctx := context.Background()
	if res, err := Check(ctx, d, src, nil); err != nil || len(res.Alerts) != 1 {
		t.Fatalf("first run: %+v %v", res, err)
	}
	// Same deal tomorrow: nothing.
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 0 {
		t.Errorf("repeated: %+v", res.Alerts)
	}
	// A higher price that is still a low (a new low elsewhere does not matter): nothing either.
	setPrice(src, "g1", cur(500, 1500, 66), 600)
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 0 {
		t.Errorf("repeated at the same price: %+v", res.Alerts)
	}
	// A lower price is a new deal.
	setPrice(src, "g1", cur(450, 1500, 70), 450)
	res, _ := Check(ctx, d, src, nil)
	if len(res.Alerts) != 1 || res.Alerts[0].Price != 450 {
		t.Fatalf("lower price: %+v", res.Alerts)
	}
	var price int64
	d.QueryRow(`SELECT last_alert_price FROM deal_alert WHERE game_id = 1`).Scan(&price)
	if price != 450 {
		t.Errorf("recorded %d", price)
	}
	// The same price after 30 days is announced again; 29 days is not enough.
	d.Exec(`UPDATE deal_alert SET last_alert_at = ?`, time.Now().Add(-29*24*time.Hour).UTC().Format(timeFmt))
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 0 {
		t.Errorf("after 29 days: %+v", res.Alerts)
	}
	d.Exec(`UPDATE deal_alert SET last_alert_at = ?`, time.Now().Add(-31*24*time.Hour).UTC().Format(timeFmt))
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 1 {
		t.Errorf("after 31 days: %+v", res.Alerts)
	}
}

func TestAlertMuted(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	if err := SetMuted(d, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := SetMuted(d, 99, true); !errors.Is(err, ErrUnknownGame) {
		t.Errorf("unknown game: %v", err)
	}
	sk := &sink{}
	res, err := Check(context.Background(), d, src, sk)
	if err != nil || len(res.Alerts) != 0 || len(sk.sent) != 0 {
		t.Fatalf("muted game alerted: %+v %v %q", res.Alerts, err, sk.sent)
	}
	if l, _ := Load(d); len(l.Muted) != 1 || l.Muted[0] != 1 {
		t.Errorf("muted %v", l.Muted)
	}
	SetMuted(d, 1, false)
	if res, _ := Check(context.Background(), d, src, nil); len(res.Alerts) != 1 {
		t.Errorf("after unmuting: %+v", res.Alerts)
	}
}

func TestCheckSkipsHiddenAndOutOfScope(t *testing.T) {
	d, src := alertDB(t)
	for i := 1; i <= 3; i++ {
		id := "g" + string(rune('0'+i))
		setPrice(src, id, cur(500, 1500, 66), 500)
	}
	ctx := context.Background()
	// Prices are cached first, then Alpha is hidden, Beta finished (out of scope) and Gamma owned.
	if _, err := Refresh(ctx, d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	SetHidden(d, 1, true)
	d.Exec(`UPDATE game SET status_id = (SELECT id FROM status WHERE code = 'finished') WHERE id = 2`)
	d.Exec(`INSERT OR IGNORE INTO ownership(name) VALUES ('Owned')`)
	d.Exec(`UPDATE game SET ownership_id = (SELECT id FROM ownership WHERE name = 'Owned') WHERE id = 3`)
	src.overview = nil
	res, err := Check(ctx, d, src, nil)
	if err != nil || len(res.Alerts) != 0 {
		t.Fatalf("alerts for hidden or out-of-scope games: %+v %v", res.Alerts, err)
	}
	for _, ids := range src.overview {
		for _, id := range ids {
			if id != "g4" && id != "g5" {
				t.Errorf("read prices of %s", id)
			}
		}
	}
	if len(src.calls) != 0 {
		t.Errorf("history asked for %v", src.calls)
	}
}

func TestTwoYearLowIsCachedAWeek(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(900, 1500, 40), 500)
	src.lows["g1"] = 800
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		Check(ctx, d, src, nil)
	}
	if len(src.calls) != 1 {
		t.Fatalf("history calls %v", src.calls)
	}
	// The price drops to the cached two-year low a day later: alert, still no extra call.
	setPrice(src, "g1", cur(800, 1500, 47), 500)
	res, _ := Check(ctx, d, src, nil)
	if len(res.Alerts) != 1 || res.Alerts[0].Kind != KindTwoYear || len(src.calls) != 1 {
		t.Errorf("alerts %+v calls %v", res.Alerts, src.calls)
	}
	// After a week it is read again.
	d.Exec(`UPDATE deal_price SET low2y_checked_at = ?`, time.Now().Add(-8*24*time.Hour).UTC().Format(timeFmt))
	Check(ctx, d, src, nil)
	if len(src.calls) != 2 {
		t.Errorf("history calls %v", src.calls)
	}
}

func TestHistoryRateLimitKeepsWhatWasDone(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	setPrice(src, "g2", cur(900, 1500, 40), 500)
	src.err = &RateLimitError{}
	sk := &sink{}
	res, err := Check(context.Background(), d, src, sk)
	if err != nil || len(res.Alerts) != 1 || len(sk.sent) != 1 {
		t.Errorf("%+v %v %q", res, err, sk.sent)
	}
}

var (
	tagRe    = regexp.MustCompile(`</?([a-z]+)[^>]*>`)
	entityRe = regexp.MustCompile(`&(lt|gt|amp|quot|#[0-9]+|#x[0-9a-fA-F]+);`)
)

// checkHTML fails when text is not something Telegram parses: only b, s, i and a tags (balanced, a with an https href),
// and every other "<", ">" or "&" escaped.
func checkHTML(t *testing.T, text string) {
	t.Helper()
	var stack []string
	for _, m := range tagRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		switch name {
		case "b", "s", "i":
		case "a":
			if !strings.HasPrefix(m[0], "</") && !regexp.MustCompile(`^<a href="https?://[^"<>]*">$`).MatchString(m[0]) {
				t.Errorf("bad link tag %q in %q", m[0], text)
			}
		default:
			t.Errorf("tag <%s> in %q", name, text)
		}
		if strings.HasPrefix(m[0], "</") {
			if len(stack) == 0 || stack[len(stack)-1] != name {
				t.Errorf("unbalanced </%s> in %q", name, text)
				return
			}
			stack = stack[:len(stack)-1]
		} else {
			stack = append(stack, name)
		}
	}
	if len(stack) != 0 {
		t.Errorf("unclosed %v in %q", stack, text)
	}
	bare := entityRe.ReplaceAllString(tagRe.ReplaceAllString(text, ""), "")
	if strings.ContainsAny(bare, "<>&") {
		t.Errorf("unescaped character in %q", text)
	}
}

func TestMessageShapes(t *testing.T) {
	a := func(n string) Alert {
		return Alert{Title: n, Kind: KindHistorical, Price: 749, Currency: "EUR", Shop: "GOG", URL: "https://x.example/" + n}
	}
	if m := Messages(nil); m != nil {
		t.Error(m)
	}

	// Single, historical low: regular price, cut and the low it beats.
	hist := Alert{Title: "Celeste", Kind: KindHistorical, Price: 499, Currency: "EUR", Cut: 75, Shop: "Steam", URL: "https://shop.example/c?a=1&b=2",
		Regular: 1999, Low: 599, LowAt: "2025-03-12T10:00:00Z"}
	want := "🔥 <b>Historical low</b>\n<b>Celeste</b>\n<b>€4.99</b>  <s>€19.99</s>  −75% at Steam\nLowest ever before: €5.99 (12 Mar 2025)\n" +
		`<a href="https://shop.example/c?a=1&amp;b=2">View deal</a>`
	g := messageGroups([]Alert{hist})
	if len(g) != 1 || g[0].text != want || g[0].preview != "" {
		t.Errorf("historical:\n%q\nwant\n%q", g[0].text, want)
	}
	checkHTML(t, g[0].text)

	// Single, lowest in 2 years: 📉, the historical low as the reference.
	two := hist
	two.Kind, two.Low = KindTwoYear, 399
	m := Messages([]Alert{two})
	if len(m) != 1 || !strings.HasPrefix(m[0], "📉 <b>Lowest in 2 years</b>\n") || !strings.Contains(m[0], "Historical low: €3.99 (12 Mar 2025)") {
		t.Errorf("two-year: %q", m)
	}
	checkHTML(t, m[0])

	// A price that is the low itself has nothing to compare with.
	same := hist
	same.Low = 499
	if m := Messages([]Alert{same}); strings.Contains(m[0], "before") {
		t.Errorf("%q", m)
	}

	// Wishlist: tag, and a large preview of the Steam page.
	wish := Alert{AppID: 504230, Title: "Celeste", Kind: KindHistorical, Price: 499, Currency: "EUR", Cut: 75, Shop: "Steam"}
	g = messageGroups([]Alert{wish})
	if g[0].preview != "https://store.steampowered.com/app/504230/" || !strings.Contains(g[0].text, "💙 Wishlist") || strings.Contains(g[0].text, "View deal") {
		t.Errorf("wishlist: %+v", g[0])
	}
	checkHTML(t, g[0].text)

	// A library game: no preview.
	if g := messageGroups([]Alert{a("A")}); g[0].preview != "" {
		t.Errorf("preview for a library game: %q", g[0].preview)
	}
	three := Messages([]Alert{a("A"), a("B"), a("C")})
	if len(three) != 3 || !strings.Contains(three[0], "<b>A</b>") || !strings.Contains(three[0], `<a href="https://x.example/A">View deal</a>`) {
		t.Errorf("%q", three)
	}

	// List: a header, then the library and the wishlist under their headings (library first even when the wishlist game
	// comes first), each game on two lines with the shop, a blank line between games; no preview.
	wishD := Alert{AppID: 9, Title: "D", Kind: KindTwoYear, Price: 100, Regular: 200, Currency: "SEK", Cut: 40, Shop: "GOG", URL: "https://x.example/D"}
	four := messageGroups([]Alert{wishD, a("A"), a("B"), a("C")})
	if len(four) != 1 || four[0].preview != "" {
		t.Fatalf("%+v", four)
	}
	lt := four[0].text
	want = "🔔 <b>4 price alerts</b>\n🔥 historical low  ·  📉 lowest in 2 years\n\n📚 <b>Your library</b>\n\n" +
		"🔥 <a href=\"https://x.example/A\"><b>A</b></a>\n" + a("A").priceLine() + "\n\n"
	if !strings.HasPrefix(lt, want) || !strings.Contains(lt, "€7.49</b> at GOG") ||
		!strings.HasSuffix(lt, "\n\n💙 <b>Your wishlist</b>\n\n📉 <a href=\"https://x.example/D\"><b>D</b></a>\n<b>1.00 SEK</b>  <s>2.00 SEK</s>  −40% at GOG") {
		t.Errorf("list: %q", lt)
	}
	if strings.Count(lt, "Your library") != 1 || strings.Count(lt, "Your wishlist") != 1 {
		t.Errorf("one heading per section: %q", lt)
	}
	checkHTML(t, lt)
	if len(four[0].ids) != 3 || len(four[0].wish) != 1 {
		t.Errorf("%+v", four[0])
	}
}

func TestMessagesEscapeHostileText(t *testing.T) {
	bad := `<b>&"'><a href="x">`
	one := Alert{Title: bad, Kind: KindHistorical, Price: 100, Currency: "EUR", Cut: 10, Regular: 200, Shop: bad, URL: `https://x.example/?q="><script>&a=<`, Low: 300, LowAt: "2025-01-01T00:00:00Z"}
	for _, m := range [][]Alert{{one}, {one, one, one, one}, {{AppID: 5, Title: bad, Kind: KindTwoYear, Price: 1, Currency: bad}}} {
		for _, text := range Messages(m) {
			checkHTML(t, text)
			if strings.Contains(text, "<script") || strings.Contains(text, bad) {
				t.Errorf("hostile text got through: %q", text)
			}
		}
	}
	if !strings.Contains(Messages([]Alert{one})[0], "&lt;b&gt;&amp;\"'&gt;&lt;a href=\"x\"&gt;") {
		t.Errorf("%q", Messages([]Alert{one})[0])
	}
	// An address that is not http(s) is no link.
	if m := Messages([]Alert{{Title: "x", Kind: KindHistorical, Price: 1, Currency: "EUR", URL: "javascript:alert(1)"}}); strings.Contains(m[0], "<a ") {
		t.Errorf("%q", m)
	}
	checkHTML(t, TestMessage())
}

func TestTestMessage(t *testing.T) {
	m := TestMessage()
	if !strings.HasPrefix(m, "✅ <b>DeltaBacklog is connected</b>\n") || !strings.Contains(m, "Example alert") || !strings.Contains(m, "<s>€19.99</s>") {
		t.Errorf("%q", m)
	}
}

func TestMessagesLong(t *testing.T) {
	a := Alert{Title: strings.Repeat("x", 50), Kind: KindHistorical, Price: 749, Currency: "EUR", Shop: "GOG", URL: "https://x.example/a"}
	var many []Alert
	for i := 0; i < 200; i++ {
		many = append(many, a)
	}
	if m := Messages(many); len(m) != 1 || utf16Len(m[0]) > 4096 {
		t.Errorf("long message: %d messages, %d units", len(m), utf16Len(m[0]))
	}
}

func TestSeenAndStandingAlerts(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	Check(context.Background(), d, src, nil)
	if l, _ := Load(d); len(l.Alerts) != 1 || l.Alerts[0].Seen || l.Alerts[0].Kind != KindHistorical {
		t.Fatalf("%+v", l.Alerts)
	}
	if err := MarkSeen(d); err != nil {
		t.Fatal(err)
	}
	if l, _ := Load(d); len(l.Alerts) != 1 || !l.Alerts[0].Seen {
		t.Errorf("%+v", l.Alerts)
	}
	// When the deal is over (price back up), the alert is no longer standing.
	setPrice(src, "g1", cur(1500, 1500, 0), 500)
	Refresh(context.Background(), d, src, Options{})
	if l, _ := Load(d); len(l.Alerts) != 0 {
		t.Errorf("%+v", l.Alerts)
	}
}

func TestRunLock(t *testing.T) {
	d := newDB(t)
	release, err := AcquireRun(d, HolderApp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = AcquireRun(d, HolderCheck)
	var busy *BusyError
	if !errors.Is(err, ErrBusy) || !errors.As(err, &busy) || busy.Holder != HolderApp {
		t.Fatalf("second run: %v", err)
	}
	release()
	release2, err := AcquireRun(d, HolderCheck)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	release2()

	// A lock left by a crash is forgotten after 30 minutes, and the first holder's late release must not free the lock
	// of the one that took over.
	r1, _ := AcquireRun(d, HolderApp)
	d.Exec(`UPDATE setting SET value = ? || substr(value, 21) WHERE key = 'deals_run'`, time.Now().Add(-time.Hour).UTC().Format(timeFmt))
	r2, err := AcquireRun(d, HolderCheck)
	if err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	r1()
	if _, err := AcquireRun(d, HolderApp); !errors.As(err, &busy) || busy.Holder != HolderCheck {
		t.Errorf("the first holder freed the second one's lock: %v", err)
	}
	r2()
	if r, err := AcquireRun(d, HolderApp); err != nil {
		t.Errorf("after the second release: %v", err)
	} else {
		r()
	}
}

// ---------------- the ITAD history and Telegram ----------------

func TestClientLowSince(t *testing.T) {
	var q map[string][]string
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/games/history/v2" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		w.Write([]byte(`[
		 {"timestamp":"2024-10-20T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amountInt":900,"currency":"EUR"}}},
		 {"timestamp":"2024-10-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amountInt":100,"currency":"EUR"}}},
		 {"timestamp":"2025-01-01T00:00:00Z","shop":{"id":61,"name":"Steam"},"deal":{"price":{"amount":9.99,"amountInt":999,"currency":"EUR"},"regular":{"amountInt":1999,"currency":"EUR"},"cut":50}},
		 {"timestamp":"2025-02-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amount":7.5,"currency":"EUR"},"cut":60}},
		 {"timestamp":"2025-03-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":null},
		 {"timestamp":"2025-04-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amountInt":1200,"currency":"EUR"}}}]`))
	})
	since := time.Date(2024, 11, 3, 10, 0, 0, 0, time.UTC)
	// The 1.00 of 2024-10-01 was replaced by the 9.00 of 2024-10-20 before the window: it does not count. The 9.00 does
	// (it was the price when the window opened), but 7.50 is lower.
	low, ok, err := c.LowSince(context.Background(), "g1", "ES", since)
	if err != nil || !ok || low.Cents != 750 || low.Currency != "EUR" {
		t.Fatalf("%+v %v %v", low, ok, err)
	}
	// It asks from eight weeks before the window.
	if q["id"][0] != "g1" || q["country"][0] != "ES" || q["since"][0] != "2024-09-08T10:00:00Z" || len(q["key"]) != 0 {
		t.Errorf("query %v", q)
	}
	empty := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	if _, ok, err := empty.LowSince(context.Background(), "g1", "ES", since); ok || err != nil {
		t.Errorf("empty history: %v %v", ok, err)
	}
	// The price in effect when the window opens counts even though it started before it: here it is the lowest.
	before := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"timestamp":"2024-10-20T00:00:00Z","deal":{"price":{"amountInt":500,"currency":"EUR"}}},
		 {"timestamp":"2025-01-01T00:00:00Z","deal":{"price":{"amountInt":999,"currency":"EUR"}}}]`))
	})
	if low, ok, _ := before.LowSince(context.Background(), "g1", "ES", since); !ok || low.Cents != 500 {
		t.Errorf("price running at the start of the window: %+v %v", low, ok)
	}
	// Not sold when the window opened (the last change before it has no deal): an earlier low does not count.
	unsold := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"timestamp":"2024-10-01T00:00:00Z","deal":{"price":{"amountInt":100,"currency":"EUR"}}},
		 {"timestamp":"2024-10-20T00:00:00Z","deal":null},{"timestamp":"2025-01-01T00:00:00Z","deal":{"price":{"amountInt":999,"currency":"EUR"}}}]`))
	})
	if low, ok, _ := unsold.LowSince(context.Background(), "g1", "ES", since); !ok || low.Cents != 999 {
		t.Errorf("not sold at the start of the window: %+v %v", low, ok)
	}
}

const (
	testToken = "123456789:AAH-testtesttesttesttesttesttesttest1"
	testChat  = "-1001234567890"
)

func TestTelegramFormats(t *testing.T) {
	if tg, err := NewTelegram("", ""); tg != nil || err != nil {
		t.Errorf("off: %v %v", tg, err)
	}
	if _, err := NewTelegram(testToken, testChat); err != nil {
		t.Errorf("valid: %v", err)
	}
	if _, err := NewTelegram(testToken, "@mychannel"); err != nil {
		t.Errorf("channel name: %v", err)
	}
	for _, bad := range []string{"nope", "123:short", testToken + " x", "abc:" + strings.Repeat("a", 35)} {
		_, err := NewTelegram(bad, testChat)
		if !errors.Is(err, ErrBadToken) || strings.Contains(err.Error(), bad) {
			t.Errorf("token %q: %v", bad, err)
		}
	}
	if _, err := NewTelegram(testToken, ""); !errors.Is(err, ErrBadChatID) {
		t.Errorf("missing chat: %v", err)
	}
	if _, err := NewTelegram("", testChat); !errors.Is(err, ErrBadToken) {
		t.Errorf("missing token: %v", err)
	}
	if _, err := NewTelegram(testToken, "https://evil"); !errors.Is(err, ErrBadChatID) {
		t.Errorf("bad chat: %v", err)
	}
}

func TestTelegramRequest(t *testing.T) {
	var gotPath, gotType, gotMethod string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType, gotMethod = r.URL.Path, r.Header.Get("Content-Type"), r.Method
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	tg, _ := NewTelegram(testToken, testChat)
	tg.base = srv.URL
	if err := tg.Send(context.Background(), "<b>hello</b>\nworld", ""); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotPath != "/bot"+testToken+"/sendMessage" || gotType != "application/json" ||
		body["chat_id"] != testChat || body["text"] != "<b>hello</b>\nworld" || body["parse_mode"] != "HTML" || len(body) != 4 {
		t.Errorf("%s %s %s %v", gotMethod, gotPath, gotType, body)
	}
	if lp, _ := body["link_preview_options"].(map[string]any); lp["is_disabled"] != true || len(lp) != 1 {
		t.Errorf("previews are not off: %v", body["link_preview_options"])
	}
	if _, old := body["disable_web_page_preview"]; old {
		t.Error("the deprecated disable_web_page_preview is sent")
	}

	// A preview address turns it on, large.
	if err := tg.Send(context.Background(), "x", "https://store.steampowered.com/app/7/"); err != nil {
		t.Fatal(err)
	}
	lp, _ := body["link_preview_options"].(map[string]any)
	if lp["url"] != "https://store.steampowered.com/app/7/" || lp["prefer_large_media"] != true || lp["is_disabled"] != nil || len(lp) != 2 {
		t.Errorf("preview: %v", lp)
	}
}

func TestTelegramErrorsNeverHoldTheToken(t *testing.T) {
	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "400":
			w.WriteHeader(400)
			io.WriteString(w, `{"ok":false,"description":"Bad Request: chat not found"}`)
		case "500":
			w.WriteHeader(500)
		case "redirect":
			http.Redirect(w, r, "https://evil.example/"+testToken, http.StatusFound)
		}
	}))
	tg, _ := NewTelegram(testToken, testChat)
	tg.base = srv.URL
	for _, m := range []string{"400", "500", "redirect"} {
		mode = m
		err := tg.Send(context.Background(), "x", "")
		if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "bot") {
			t.Errorf("%s: %v", m, err)
		}
		if m == "400" && !strings.Contains(err.Error(), "chat not found") {
			t.Errorf("Telegram's own words are lost: %v", err)
		}
	}
	srv.Close() // unreachable
	if err := tg.Send(context.Background(), "x", ""); err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestNotifierFailureIsRetried(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500) // Alpha
	setPrice(src, "g2", cur(500, 1500, 66), 500) // Beta
	ctx := context.Background()
	// Alpha's message fails; Beta's is still sent (one failure does not stop the others) and the run reports the failure.
	bad := &sink{failIf: func(text string) bool { return strings.Contains(text, "Alpha") }}
	res, err := Check(ctx, d, src, bad)
	if err == nil || strings.Contains(err.Error(), "sekrit") || !res.Sent || len(bad.sent) != 1 || !strings.Contains(bad.sent[0], "Beta") {
		t.Fatalf("%+v %v %q", res, err, bad.sent)
	}
	// The app shows both alerts whatever Telegram did.
	if l, _ := Load(d); len(l.Alerts) != 2 {
		t.Errorf("%+v", l.Alerts)
	}
	// Next run: nothing is new, and only the undelivered alert is sent.
	good := &sink{}
	res, err = Check(ctx, d, src, good)
	if err != nil || len(res.Alerts) != 0 || len(good.sent) != 1 || !strings.Contains(good.sent[0], "Alpha") {
		t.Fatalf("retry: %+v %v %q", res, err, good.sent)
	}
	// And then nothing more.
	again := &sink{}
	if _, err := Check(ctx, d, src, again); err != nil || len(again.sent) != 0 {
		t.Errorf("sent again: %v %q", err, again.sent)
	}
}

func TestUndeliveredAlertIsDroppedWhenTheDealEnds(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	ctx := context.Background()
	if _, err := Check(ctx, d, src, &sink{err: errors.New("down")}); err == nil {
		t.Fatal("no error")
	}
	// The sale is over by the next run: nothing to tell.
	setPrice(src, "g1", cur(1500, 1500, 0), 500)
	good := &sink{}
	if _, err := Check(ctx, d, src, good); err != nil || len(good.sent) != 0 {
		t.Errorf("%v %q", err, good.sent)
	}
	// Without Telegram the alerts are not queued for a bot set up later.
	setPrice(src, "g1", cur(400, 1500, 73), 400)
	Check(ctx, d, src, nil)
	late := &sink{}
	if _, err := Check(ctx, d, src, late); err != nil || len(late.sent) != 0 {
		t.Errorf("queued while Telegram was off: %v %q", err, late.sent)
	}
}

func TestStalePricesAndEndedDealsDoNotAlert(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	ctx := context.Background()
	// A cache from an earlier refresh, and a refresh that stops at a rate limit before reading anything.
	if _, err := Refresh(ctx, d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE deal_price SET fetched_at = '2020-01-01T00:00:00Z'`)
	src.fakeSource.err = &RateLimitError{}
	sk := &sink{}
	res, err := Check(ctx, d, src, sk)
	if err != nil || len(res.Alerts) != 0 || len(sk.sent) != 0 {
		t.Fatalf("stale price announced: %+v %v %q", res.Alerts, err, sk.sent)
	}
	src.fakeSource.err = nil

	// A deal whose end time has passed is not announced, one still running is.
	past := cur(500, 1500, 66)
	past.Expiry = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	setPrice(src, "g1", past, 500)
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 0 {
		t.Errorf("ended deal announced: %+v", res.Alerts)
	}
	soon := cur(500, 1500, 66)
	soon.Expiry = time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	setPrice(src, "g1", soon, 500)
	if res, _ := Check(ctx, d, src, nil); len(res.Alerts) != 1 {
		t.Errorf("running deal not announced: %+v", res.Alerts)
	}
}

func TestLongListKeepsLinksWholeAndUnderTheLimit(t *testing.T) {
	var many []Alert
	for i := 0; i < 200; i++ {
		many = append(many, Alert{GameID: int64(i + 1), Title: strings.Repeat("\U0001F3AE", 20) + "<&>", Kind: KindTwoYear, Price: 100, Currency: "EUR", Shop: "Steam", URL: "https://x.example/game/0123456789?a=1&b=2"})
	}
	groups := messageGroups(many)
	if len(groups) != 1 || len(groups[0].ids) != 200 {
		t.Fatalf("groups %d", len(groups))
	}
	text := groups[0].text
	if n := utf16Len(text); n > 4096 {
		t.Errorf("%d UTF-16 units", n)
	}
	if !strings.Contains(text, "and ") || !strings.Contains(text, "more: open Deals") {
		t.Errorf("no note about the rest: %q", text[len(text)-80:])
	}
	checkHTML(t, text) // every entry whole: balanced tags, no cut link or entity
	if !utf8.ValidString(text) {
		t.Error("invalid UTF-8")
	}
}

func TestTelegramUserAgentAndRuneSafeReason(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.WriteHeader(400)
		io.WriteString(w, `{"ok":false,"description":"`+strings.Repeat("\u00e9", 300)+`"}`)
	}))
	defer srv.Close()
	tg, _ := NewTelegram(testToken, testChat)
	tg.base = srv.URL
	err := tg.Send(context.Background(), "x", "")
	if err == nil || !utf8.ValidString(err.Error()) || ua != "DeltaBacklog" {
		t.Errorf("%v %q", err, ua)
	}
}
