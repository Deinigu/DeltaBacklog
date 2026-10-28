package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

// dealsFake is an offline ITAD: every title is known and costs 5.00 (half of 10.00), its low is 4.00.
type dealsFake struct{ off bool }

func (f dealsFake) Configured() bool { return !f.off }
func (dealsFake) LookupAppID(context.Context, int64) (deals.Game, bool, error) {
	return deals.Game{}, false, nil
}
func (dealsFake) LookupTitle(_ context.Context, t string) (deals.Game, bool, error) {
	return deals.Game{ID: "id-" + t, Title: t, Slug: "s"}, true, nil
}
func (dealsFake) Search(_ context.Context, t string) ([]deals.Game, error) {
	return []deals.Game{{ID: "pick", Title: t + " Deluxe", Slug: "d"}}, nil
}
func (dealsFake) Overview(_ context.Context, ids []string, _ string) (map[string]deals.Overview, error) {
	out := map[string]deals.Overview{}
	for _, id := range ids {
		out[id] = deals.Overview{Current: &deals.Current{Shop: "Steam", Price: deals.Money{Cents: 500, Currency: "EUR"}, Regular: deals.Money{Cents: 1000, Currency: "EUR"}, Cut: 50, URL: "https://shop.example/x"},
			Low: &deals.Low{Shop: "GOG", Price: deals.Money{Cents: 400, Currency: "EUR"}, At: "2025-01-01T00:00:00Z"}}
	}
	return out, nil
}

func dealsServer(t *testing.T, src deals.Source) (*httptest.Server, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for i, s := range []string{"up_next", "backlog", "finished"} {
		d.Exec(`INSERT INTO game(id, title, title_sort, status_id) VALUES (?, ?, ?, (SELECT id FROM status WHERE code = ?))`, i+1, "Game "+s, s, s)
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Deals: src}).Handler())
	t.Cleanup(srv.Close)
	return srv, d
}

func send(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rd bytes.Buffer
	if body != nil {
		json.NewEncoder(&rd).Encode(body)
	}
	req, _ := http.NewRequest(method, url, &rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestDealsRefreshAndList(t *testing.T) {
	srv, _ := dealsServer(t, dealsFake{})
	code, out := send(t, "GET", srv.URL+"/api/deals", nil)
	if code != 200 || out["configured"] != true || out["country"] != "ES" || len(out["prices"].([]any)) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = send(t, "POST", srv.URL+"/api/deals/refresh", nil); code != http.StatusAccepted {
		t.Fatalf("refresh %d", code)
	}
	var prices []any
	for i := 0; i < 100 && len(prices) < 2; i++ {
		time.Sleep(20 * time.Millisecond)
		_, out = send(t, "GET", srv.URL+"/api/deals", nil)
		prices = out["prices"].([]any)
	}
	if len(prices) != 2 { // the finished game has none
		t.Fatalf("prices %v", out)
	}
	p := prices[0].(map[string]any)
	if p["price"] != 500.0 || p["cut"] != 50.0 || p["low_price"] != 400.0 || p["currency"] != "EUR" {
		t.Errorf("price %v", p)
	}
}

func TestDealsNotSetUp(t *testing.T) {
	for _, src := range []deals.Source{nil, dealsFake{off: true}} {
		srv, _ := dealsServer(t, src)
		if _, out := send(t, "GET", srv.URL+"/api/deals", nil); out["configured"] != false {
			t.Errorf("configured: %v", out)
		}
		if code, _ := send(t, "POST", srv.URL+"/api/deals/refresh", nil); code != http.StatusServiceUnavailable {
			t.Errorf("refresh: %d", code)
		}
		if code, _ := send(t, "GET", srv.URL+"/api/deals/search?title=x", nil); code != http.StatusServiceUnavailable {
			t.Errorf("search: %d", code)
		}
	}
}

func TestDealsMatchAndCountry(t *testing.T) {
	srv, _ := dealsServer(t, dealsFake{})
	url := srv.URL + "/api/games/1/deal-match"
	_, out := send(t, "GET", srv.URL+"/api/deals/search?title=Hades", nil)
	if g := out["games"].([]any); len(g) != 1 {
		t.Fatalf("search %v", out)
	}
	if code, _ := send(t, "GET", srv.URL+"/api/deals/search", nil); code != 400 {
		t.Errorf("search without a title: %d", code)
	}
	code, out := send(t, "PUT", url, map[string]any{"itad_id": "pick", "title": "Hades Deluxe", "slug": "d"})
	if code != 200 || len(out["prices"].([]any)) != 1 { // the picked game is priced at once
		t.Fatalf("pick %d %v", code, out)
	}
	if m := out["matches"].([]any)[0].(map[string]any); m["source"] != "user" || m["itad_id"] != "pick" {
		t.Errorf("match %v", m)
	}
	code, out = send(t, "PUT", url, map[string]any{"none": true})
	if code != 200 || len(out["prices"].([]any)) != 0 || out["matches"].([]any)[0].(map[string]any)["itad_id"] != "" {
		t.Errorf("none %d %v", code, out)
	}
	if code, _ = send(t, "PUT", url, map[string]any{"reset": true}); code != 200 {
		t.Errorf("reset %d", code)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"none": true, "reset": true}, map[string]any{"itad_id": "x", "none": true}} {
		if code, _ = send(t, "PUT", url, body); code != 400 {
			t.Errorf("%v: %d", body, code)
		}
	}
	if code, _ = send(t, "PUT", srv.URL+"/api/games/99/deal-match", map[string]any{"none": true}); code != 404 {
		t.Errorf("unknown game: %d", code)
	}
	if code, out = send(t, "PUT", srv.URL+"/api/deals/country", map[string]any{"country": "gb"}); code != 200 || out["country"] != "GB" {
		t.Errorf("country %d %v", code, out)
	}
	if code, _ = send(t, "PUT", srv.URL+"/api/deals/country", map[string]any{"country": "Spain"}); code != 400 {
		t.Errorf("bad country: %d", code)
	}
}
