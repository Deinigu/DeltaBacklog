package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
	mu   sync.Mutex
	sent []string
	err  error
}

func (s *sink) Send(_ context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, text)
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
	if len(sk.sent) != 2 || !res.Sent || !strings.Contains(sk.sent[0], "historical low") || !strings.Contains(sk.sent[1], "lowest in 2 years") ||
		!strings.Contains(sk.sent[0], "€5.00 at Steam") || !strings.Contains(sk.sent[0], "https://shop.example/x") {
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

func TestMessages(t *testing.T) {
	a := func(n string) Alert {
		return Alert{Title: n, Kind: KindHistorical, Price: 749, Currency: "EUR", Shop: "GOG", URL: "https://x.example/" + n}
	}
	if m := Messages(nil); m != nil {
		t.Error(m)
	}
	three := Messages([]Alert{a("A"), a("B"), a("C")})
	if len(three) != 3 || three[0] != "Price alert\nA: €7.49 at GOG (historical low)\nhttps://x.example/A" {
		t.Errorf("%q", three)
	}
	four := Messages([]Alert{a("A"), a("B"), a("C"), {Title: "D", Kind: KindTwoYear, Price: 100, Currency: "SEK"}})
	if len(four) != 1 || !strings.Contains(four[0], "4 games") || !strings.Contains(four[0], "D: 1.00 SEK (lowest in 2 years)") || !strings.Contains(four[0], "https://x.example/C") {
		t.Errorf("%q", four)
	}
	var many []Alert
	for i := 0; i < 200; i++ {
		many = append(many, a(strings.Repeat("x", 50)))
	}
	if m := Messages(many); len(m) != 1 || len([]rune(m[0])) > 4001 {
		t.Errorf("long message: %d", len([]rune(m[0])))
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
	release, err := AcquireRun(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireRun(d); !errors.Is(err, ErrBusy) {
		t.Fatalf("second run: %v", err)
	}
	release()
	release2, err := AcquireRun(d)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	release2()
	// A lock left by a crash is forgotten after 30 minutes.
	d.Exec(`INSERT INTO setting(key, value) VALUES ('deals_run', ?)`, time.Now().Add(-time.Hour).UTC().Format(timeFmt))
	if _, err := AcquireRun(d); err != nil {
		t.Errorf("stale lock: %v", err)
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
		 {"timestamp":"2025-01-01T00:00:00Z","shop":{"id":61,"name":"Steam"},"deal":{"price":{"amount":9.99,"amountInt":999,"currency":"EUR"},"regular":{"amountInt":1999,"currency":"EUR"},"cut":50}},
		 {"timestamp":"2025-02-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amount":7.5,"currency":"EUR"},"cut":60}},
		 {"timestamp":"2025-03-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":null},
		 {"timestamp":"2025-04-01T00:00:00Z","shop":{"id":1,"name":"GOG"},"deal":{"price":{"amountInt":1200,"currency":"EUR"}}}]`))
	})
	since := time.Date(2024, 11, 3, 10, 0, 0, 0, time.UTC)
	low, ok, err := c.LowSince(context.Background(), "g1", "ES", since)
	if err != nil || !ok || low.Cents != 750 || low.Currency != "EUR" {
		t.Fatalf("%+v %v %v", low, ok, err)
	}
	if q["id"][0] != "g1" || q["country"][0] != "ES" || q["since"][0] != "2024-11-03T10:00:00Z" || len(q["key"]) != 0 {
		t.Errorf("query %v", q)
	}
	empty := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	if _, ok, err := empty.LowSince(context.Background(), "g1", "ES", since); ok || err != nil {
		t.Errorf("empty history: %v %v", ok, err)
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
	if err := tg.Send(context.Background(), "hello\nworld"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotPath != "/bot"+testToken+"/sendMessage" || gotType != "application/json" ||
		body["chat_id"] != testChat || body["text"] != "hello\nworld" || body["disable_web_page_preview"] != true || len(body) != 3 {
		t.Errorf("%s %s %s %v", gotMethod, gotPath, gotType, body)
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
		err := tg.Send(context.Background(), "x")
		if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "bot") {
			t.Errorf("%s: %v", m, err)
		}
		if m == "400" && !strings.Contains(err.Error(), "chat not found") {
			t.Errorf("Telegram's own words are lost: %v", err)
		}
	}
	srv.Close() // unreachable
	if err := tg.Send(context.Background(), "x"); err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestCheckStopsOnNotifierFailureButRecords(t *testing.T) {
	d, src := alertDB(t)
	setPrice(src, "g1", cur(500, 1500, 66), 500)
	res, err := Check(context.Background(), d, src, &sink{err: errors.New("Telegram could not be reached")})
	if err == nil || res.Sent {
		t.Fatalf("%+v %v", res, err)
	}
	// The app still shows the alert.
	if l, _ := Load(d); len(l.Alerts) != 1 {
		t.Errorf("%+v", l.Alerts)
	}
}
