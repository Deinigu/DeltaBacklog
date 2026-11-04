package deals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func offer(shop string, price int64) Current {
	return Current{Shop: shop, Price: eur(price), Regular: eur(2000), Cut: 0, URL: "https://shop.example/" + strings.ToLower(shop)}
}

func TestChoose(t *testing.T) {
	steamEpic := []Current{offer("Epic Game Store", 1000), offer("Steam", 1500), offer("GOG", 1400)}
	tests := []struct {
		name       string
		offers     []Current
		shops      []string
		threshold  int
		chosen     string
		alt        string // "" = none
		chosenCent int64
	}{
		{"ranked hit wins over cheaper", steamEpic, []string{"Steam"}, 15, "Steam", "Epic Game Store", 1500},
		{"first ranked shop with an offer", steamEpic, []string{"Humble Store", "GOG", "Steam"}, 15, "GOG", "Epic Game Store", 1400},
		{"ranking is case-insensitive", steamEpic, []string{"steam"}, 15, "Steam", "Epic Game Store", 1500},
		{"unlisted shops rank below, cheapest among them", []Current{offer("Epic Game Store", 900), offer("Fanatical", 800)}, []string{"Steam"}, 15, "Fanatical", "", 800},
		{"no listed shop sells it: cheapest", steamEpic, []string{"Humble Store"}, 15, "Epic Game Store", "", 1000},
		{"empty list: cheapest, no hint", steamEpic, nil, 15, "Epic Game Store", "", 1000},
		{"exactly at the threshold: no hint", []Current{offer("Steam", 1000), offer("Epic Game Store", 850)}, []string{"Steam"}, 15, "Steam", "", 1000},
		{"one cent past the threshold: hint", []Current{offer("Steam", 1000), offer("Epic Game Store", 849)}, []string{"Steam"}, 15, "Steam", "Epic Game Store", 1000},
		{"threshold 0: any cheaper offer", []Current{offer("Steam", 1000), offer("Epic Game Store", 999)}, []string{"Steam"}, 0, "Steam", "Epic Game Store", 1000},
		{"threshold 100: never", []Current{offer("Steam", 1000), offer("Epic Game Store", 0)}, []string{"Steam"}, 100, "Steam", "", 1000},
		{"chosen is the cheapest: no hint", []Current{offer("Steam", 500), offer("Epic Game Store", 900)}, []string{"Steam"}, 15, "Steam", "", 500},
		{"hint is the cheapest of the others", []Current{offer("Steam", 1000), offer("GOG", 700), offer("Epic Game Store", 600)}, []string{"Steam"}, 15, "Steam", "Epic Game Store", 1000},
		{"ranked cheaper shop beats a dearer ranked one only by rank", []Current{offer("GOG", 700), offer("Steam", 1000)}, []string{"Steam", "GOG"}, 15, "Steam", "GOG", 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chosen, alt := Choose(tc.offers, StorePrefs{Shops: tc.shops, Threshold: tc.threshold})
			if chosen == nil || chosen.Shop != tc.chosen || chosen.Price.Cents != tc.chosenCent {
				t.Fatalf("chosen = %+v", chosen)
			}
			if (tc.alt == "") != (alt == nil) || (alt != nil && alt.Shop != tc.alt) {
				t.Fatalf("alt = %+v, want %q", alt, tc.alt)
			}
		})
	}
	if c, a := Choose(nil, StorePrefs{Shops: []string{"Steam"}}); c != nil || a != nil {
		t.Error("no offers, no choice")
	}
}

func TestStorePrefsValidation(t *testing.T) {
	d := newDB(t)
	if p := LoadStorePrefs(d); len(p.Shops) != 0 || p.Threshold != DefaultThreshold || p.Shops == nil {
		t.Fatalf("default: %+v", p)
	}
	good := StorePrefs{Shops: []string{"Steam", "GOG", "Humble Store"}, Threshold: 20}
	if err := SaveStorePrefs(d, good); err != nil {
		t.Fatal(err)
	}
	if p := LoadStorePrefs(d); strings.Join(p.Shops, ",") != "Steam,GOG,Humble Store" || p.Threshold != 20 {
		t.Fatalf("round trip: %+v", p)
	}
	many := make([]string, MaxStores+1)
	for i := range many {
		many[i] = fmt.Sprint("Shop ", i)
	}
	for name, bad := range map[string]StorePrefs{
		"too many":      {Shops: many, Threshold: 15},
		"duplicate":     {Shops: []string{"Steam", "steam"}, Threshold: 15},
		"markup":        {Shops: []string{"<b>Steam</b>"}, Threshold: 15},
		"blank":         {Shops: []string{" "}, Threshold: 15},
		"long":          {Shops: []string{strings.Repeat("x", 61)}, Threshold: 15},
		"control":       {Shops: []string{"St\neam"}, Threshold: 15},
		"padded":        {Shops: []string{" Steam"}, Threshold: 15},
		"threshold low": {Shops: []string{"Steam"}, Threshold: -1},
		"threshold big": {Shops: []string{"Steam"}, Threshold: 101},
	} {
		if err := SaveStorePrefs(d, bad); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if p := LoadStorePrefs(d); p.Threshold != 20 || len(p.Shops) != 3 {
		t.Errorf("a refused save must change nothing: %+v", p)
	}
	if err := SaveStorePrefs(d, StorePrefs{Threshold: 0}); err != nil || len(LoadStorePrefs(d).Shops) != 0 {
		t.Errorf("clearing: %v", err)
	}
	// A damaged setting reads as no ranking.
	d.Exec(`UPDATE setting SET value = 'not json' WHERE key = 'deals_stores'`)
	if p := LoadStorePrefs(d); len(p.Shops) != 0 {
		t.Errorf("damaged: %+v", p)
	}
}

func TestClientOffersAndShops(t *testing.T) {
	var shopCalls, offerCalls int
	var body []string
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/games/prices/v3":
			offerCalls++
			if r.Method != http.MethodPost || r.URL.Query().Get("country") != "ES" {
				t.Errorf("request %s %s", r.Method, r.URL)
			}
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`[
			 {"id":"a","historyLow":{"all":{"amount":5,"amountInt":500,"currency":"EUR"}},"deals":[
			  {"shop":{"id":61,"name":"Steam"},"price":{"amountInt":1499,"currency":"EUR"},"regular":{"amountInt":1499,"currency":"EUR"},"cut":0,"url":"https://store.example/a"},
			  {"shop":{"id":16,"name":"Epic Game Store"},"price":{"amountInt":749,"currency":"EUR"},"regular":{"amountInt":1499,"currency":"EUR"},"cut":50,"url":"javascript:alert(1)","expiry":"2030-01-01T00:00:00Z"},
			  {"shop":{"id":2,"name":""},"price":{"amountInt":1,"currency":"EUR"},"regular":{"amountInt":1,"currency":"EUR"},"cut":0},
			  {"shop":{"id":3,"name":"Broken"},"price":{"amountInt":1},"cut":0}]},
			 {"id":"b","deals":[]}]`))
		case "/service/shops/v1":
			shopCalls++
			if r.URL.Query().Get("country") != "ES" {
				t.Errorf("country %s", r.URL)
			}
			w.Write([]byte(`[{"id":61,"title":"Steam"},{"id":1,"title":"GOG"},{"id":9,"title":"<script>"},{"id":5,"title":"Epic Game Store"}]`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	got, err := c.Offers(ctx, []string{"a", "b"}, "ES")
	if err != nil || len(body) != 2 || offerCalls != 1 {
		t.Fatalf("%v %v %d", err, body, offerCalls)
	}
	a := got["a"]
	if len(a) != 2 || a[0].Shop != "Steam" || a[0].Price.Cents != 1499 || a[0].Cut != 0 || a[0].URL != "https://store.example/a" ||
		a[1].Shop != "Epic Game Store" || a[1].Price.Cents != 749 || a[1].Cut != 50 || a[1].URL != "" || a[1].Expiry == "" {
		t.Errorf("offers = %+v", a)
	}
	if len(got["b"]) != 0 {
		t.Errorf("b = %+v", got["b"])
	}
	if _, err := c.Offers(ctx, make([]string, MaxOffersBatch+1), "ES"); err == nil {
		t.Error("an oversized batch must be refused")
	}
	shops, err := c.Shops(ctx, "ES")
	if err != nil || strings.Join(shops, ",") != "Epic Game Store,GOG,Steam" {
		t.Fatalf("shops %v %v", shops, err)
	}
	if _, err := c.Shops(ctx, "ES"); err != nil || shopCalls != 1 {
		t.Errorf("the shop list is cached: %d calls, %v", shopCalls, err)
	}
}

// shopSource is the fake ITAD with per-shop offers; it counts the requests.
type shopSource struct {
	*histSource
	offers     map[string][]Current
	offerCalls [][]string
}

func (s *shopSource) Offers(_ context.Context, ids []string, _ string) (map[string][]Current, error) {
	s.offerCalls = append(s.offerCalls, ids)
	out := map[string][]Current{}
	for _, id := range ids {
		out[id] = s.offers[id]
	}
	return out, nil
}

func saleAt(shop string, price, regular int64, cut int) Current {
	return Current{Shop: shop, Price: eur(price), Regular: eur(regular), Cut: cut, URL: "https://shop.example/" + strings.ToLower(shop)}
}

func TestRankedPriceIsStoredAndAlertsFollowIt(t *testing.T) {
	d, h := alertDB(t)
	src := &shopSource{histSource: h, offers: map[string][]Current{}}
	// Alpha: at its all-time low only at Epic; Steam asks the full price. Beta: Steam is on sale at its low too.
	setPrice(h, "g1", cur(500, 1500, 66), 500)
	src.offers["g1"] = []Current{saleAt("Epic Game Store", 500, 1500, 66), saleAt("Steam", 1500, 1500, 0)}
	setPrice(h, "g2", cur(400, 1500, 73), 400)
	src.offers["g2"] = []Current{saleAt("Steam", 400, 1500, 73), saleAt("Epic Game Store", 400, 1500, 73)}
	if err := SaveStorePrefs(d, StorePrefs{Shops: []string{"Steam"}, Threshold: 15}); err != nil {
		t.Fatal(err)
	}
	sk := &sink{}
	res, err := Check(context.Background(), d, src, sk)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Alerts) != 1 || res.Alerts[0].Title != "Beta" || res.Alerts[0].Shop != "Steam" {
		t.Fatalf("a low at a lower-ranked shop must not alert: %+v", res.Alerts)
	}
	l, _ := Load(d)
	var alpha *Price
	for i := range l.Prices {
		if l.Prices[i].GameID == 1 {
			alpha = &l.Prices[i]
		}
	}
	if alpha == nil || *alpha.Shop != "Steam" || *alpha.Price != 1500 || alpha.AltShop == nil || *alpha.AltShop != "Epic Game Store" || *alpha.AltPrice != 500 || alpha.AltURL == nil || *alpha.AltURL != "https://shop.example/epic game store" {
		t.Fatalf("alpha = %+v", alpha)
	}
	// Without a ranking the same data alerts for the cheapest offer, as before, and the hint disappears.
	if err := SaveStorePrefs(d, StorePrefs{Threshold: 15}); err != nil {
		t.Fatal(err)
	}
	d.Exec(`DELETE FROM deal_alert`)
	res, err = Check(context.Background(), d, src, nil)
	if err != nil || len(res.Alerts) != 2 {
		t.Fatalf("no ranking: %+v %v", res.Alerts, err)
	}
	var alt *string
	d.QueryRow(`SELECT alt_shop FROM deal_price WHERE game_id = 1`).Scan(&alt)
	if alt != nil {
		t.Errorf("no ranking, no hint: %v", *alt)
	}
}

func TestRefreshCallCounts(t *testing.T) {
	d, h := alertDB(t)
	src := &shopSource{histSource: h, offers: map[string][]Current{}}
	// 130 library games (more than two overview batches), all matched.
	for i := 6; i <= 130; i++ {
		title := fmt.Sprint("Game ", i)
		addGame(t, d, int64(i), title, "backlog", "")
		h.byTitle[title] = Game{ID: fmt.Sprint("x", i), Title: title}
	}
	for id := range h.byTitle {
		h.prices[h.byTitle[id].ID] = Overview{Current: cur(500, 1000, 50)}
		src.offers[h.byTitle[id].ID] = []Current{saleAt("Steam", 900, 1000, 10), saleAt("GOG", 500, 1000, 50)}
	}
	ctx := context.Background()
	if _, err := Refresh(ctx, d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(src.offerCalls) != 0 {
		t.Errorf("no ranking: no per-shop calls, got %d", len(src.offerCalls))
	}
	overview := len(src.overview)
	if err := SaveStorePrefs(d, StorePrefs{Shops: []string{"Steam"}, Threshold: 15}); err != nil {
		t.Fatal(err)
	}
	src.overview = nil
	if _, err := Refresh(ctx, d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(src.overview) != overview || overview != 3 {
		t.Errorf("overview calls: %d, then %d (want 3)", overview, len(src.overview))
	}
	if len(src.offerCalls) != 1 || len(src.offerCalls[0]) != 130 {
		t.Errorf("one batched per-shop call for 130 games, got %d calls", len(src.offerCalls))
	}
	var shop string
	var price, alt int64
	d.QueryRow(`SELECT cur_shop, cur_price, alt_price FROM deal_price WHERE game_id = 7`).Scan(&shop, &price, &alt)
	if shop != "Steam" || price != 900 || alt != 500 {
		t.Errorf("stored %s %d alt %d", shop, price, alt)
	}
}

func TestWishlistFollowsTheRanking(t *testing.T) {
	d := newDB(t)
	h := wishSource()
	src := &shopSource{histSource: h, offers: map[string][]Current{
		"w10": {saleAt("Steam", 1500, 1500, 0), saleAt("GOG", 500, 1500, 66)},
		"w20": {saleAt("Steam", 1500, 2000, 25)},
	}}
	if err := SaveStorePrefs(d, StorePrefs{Shops: []string{"Steam"}, Threshold: 15}); err != nil {
		t.Fatal(err)
	}
	wl := &fakeWish{entries: entries(10, 20)}
	res, err := CheckWith(context.Background(), d, src, wl, &sink{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Alerts) != 0 {
		t.Errorf("Hollow Knight's low is at GOG only: %+v", res.Alerts)
	}
	if len(src.offerCalls) != 1 || len(src.offerCalls[0]) != 2 {
		t.Errorf("one per-shop call for the wishlist: %v", src.offerCalls)
	}
	var shop, altShop string
	var price, altPrice int64
	d.QueryRow(`SELECT cur_shop, cur_price, alt_shop, alt_price FROM wish_price WHERE appid = 10`).Scan(&shop, &price, &altShop, &altPrice)
	if shop != "Steam" || price != 1500 || altShop != "GOG" || altPrice != 500 {
		t.Errorf("stored %s %d %s %d", shop, price, altShop, altPrice)
	}
}

func TestRefreshOneAppliesTheRanking(t *testing.T) {
	d, h := alertDB(t)
	src := &shopSource{histSource: h, offers: map[string][]Current{"g1": {saleAt("Steam", 900, 1000, 10), saleAt("GOG", 500, 1000, 50)}}}
	setPrice(h, "g1", cur(500, 1000, 50), 500)
	if _, err := d.Exec(`INSERT INTO deal_match(game_id, itad_id, source) VALUES (1, 'g1', 'user')`); err != nil {
		t.Fatal(err)
	}
	SaveStorePrefs(d, StorePrefs{Shops: []string{"Steam"}, Threshold: 15})
	if err := RefreshOne(context.Background(), d, src, 1); err != nil {
		t.Fatal(err)
	}
	var shop, alt string
	d.QueryRow(`SELECT cur_shop, alt_shop FROM deal_price WHERE game_id = 1`).Scan(&shop, &alt)
	if shop != "Steam" || alt != "GOG" {
		t.Errorf("%s %s", shop, alt)
	}
}

func TestMessageShowsTheCheaperOffer(t *testing.T) {
	a := Alert{Title: "Game", Kind: KindHistorical, Price: 1500, Currency: "EUR", Cut: 10, Shop: "Steam", Regular: 1600, AltShop: "Epic <Game>", AltPrice: 999}
	h := a.html()
	if !strings.Contains(h, "at Steam <i>(€9.99 at Epic &lt;Game&gt;)</i>") {
		t.Errorf("message: %q", h)
	}
	a.AltShop = ""
	if strings.Contains(a.html(), "<i>(") {
		t.Error("no hint without an alt")
	}
	a.AltShop, a.AltPrice = "Epic", 2000
	if strings.Contains(a.html(), "<i>(") {
		t.Error("a dearer alt is not a hint")
	}
}

func TestClearingTheRankingClearsTheHints(t *testing.T) {
	d, h := alertDB(t)
	src := &shopSource{histSource: h, offers: map[string][]Current{"g1": {saleAt("Steam", 900, 1000, 10), saleAt("GOG", 500, 1000, 50)}}}
	setPrice(h, "g1", cur(500, 1000, 50), 500)
	SaveStorePrefs(d, StorePrefs{Shops: []string{"Steam"}, Threshold: 15})
	if _, err := Refresh(context.Background(), d, src, Options{}); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM deal_price WHERE alt_shop IS NOT NULL`).Scan(&n)
	if n == 0 {
		t.Fatal("expected a hint")
	}
	if err := SaveStorePrefs(d, StorePrefs{Threshold: 15}); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT count(*) FROM deal_price WHERE alt_shop IS NOT NULL OR alt_price IS NOT NULL OR alt_url IS NOT NULL`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows keep a hint", n)
	}
}
