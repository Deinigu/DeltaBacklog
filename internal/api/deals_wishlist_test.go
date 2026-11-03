package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

// fakeWishlist is an offline Steam wishlist; no ids = private or empty.
type fakeWishlist struct{ ids []int64 }

func (f fakeWishlist) Wishlist(context.Context) ([]deals.WishEntry, error) {
	if len(f.ids) == 0 {
		return nil, deals.ErrWishlistEmpty
	}
	var out []deals.WishEntry
	for _, id := range f.ids {
		out = append(out, deals.WishEntry{AppID: id, AddedAt: "2025-05-01T10:00:00Z"})
	}
	return out, nil
}

// wishFake knows apps 10 and 20 (the rest is dealsFake).
type wishFake struct{ dealsFake }

func (wishFake) LookupAppID(_ context.Context, id int64) (deals.Game, bool, error) {
	switch id {
	case 10:
		return deals.Game{ID: "w10", Title: "Hollow Knight", Slug: "hk"}, true, nil
	case 20:
		return deals.Game{ID: "w20", Title: "Celeste", Slug: "ce"}, true, nil
	}
	return deals.Game{}, false, nil
}

func waitDealsIdle(t *testing.T, url string) map[string]any {
	t.Helper()
	var out map[string]any
	for i := 0; i < 150; i++ {
		time.Sleep(20 * time.Millisecond)
		_, out = send(t, "GET", url+"/api/deals", nil)
		if !out["refresh"].(map[string]any)["running"].(bool) {
			return out
		}
	}
	t.Fatal("the refresh did not finish")
	return out
}

func TestDealsWishlist(t *testing.T) {
	srv, d := dealsServer(t, wishFake{})
	// No STEAM_ID: no wishlist section at all.
	if _, out := send(t, "GET", srv.URL+"/api/deals", nil); out["wishlist"] != nil {
		t.Fatalf("no Steam id: %v", out["wishlist"])
	}
	srv2 := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Deals: wishFake{}, Wishlist: fakeWishlist{ids: []int64{10, 20, 30}}}).Handler())
	t.Cleanup(srv2.Close)
	_, out := send(t, "GET", srv2.URL+"/api/deals", nil)
	w, _ := out["wishlist"].(map[string]any)
	if w == nil || len(w["items"].([]any)) != 0 || w["status"] != "" {
		t.Fatalf("before a refresh: %v", out["wishlist"])
	}
	if code, _ := send(t, "POST", srv2.URL+"/api/deals/refresh", nil); code != http.StatusAccepted {
		t.Fatalf("refresh %d", code)
	}
	out = waitDealsIdle(t, srv2.URL)
	w = out["wishlist"].(map[string]any)
	items := w["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items: %v", w)
	}
	hk := items[0].(map[string]any)
	p, _ := hk["price"].(map[string]any)
	if hk["appid"] != 10.0 || hk["title"] != "Hollow Knight" || hk["itad_id"] != "w10" || p == nil || p["price"] != 500.0 || p["low_price"] != 400.0 {
		t.Errorf("item %v", hk)
	}
	if un := items[2].(map[string]any); un["title"] != "Steam app 30" || un["price"] != nil || un["matched"] != true {
		t.Errorf("unmatched %v", un)
	}
	// Hide and show again, mute and unmute; unknown games are 404.
	code, out := send(t, "PUT", srv2.URL+"/api/deals/wishlist/20/hidden", nil)
	w = out["wishlist"].(map[string]any)
	if code != 200 || len(w["items"].([]any)) != 2 || len(w["hidden"].([]any)) != 1 {
		t.Errorf("hide: %d %v", code, w)
	}
	code, out = send(t, "DELETE", srv2.URL+"/api/deals/wishlist/20/hidden", nil)
	if w = out["wishlist"].(map[string]any); code != 200 || len(w["items"].([]any)) != 3 || len(w["hidden"].([]any)) != 0 {
		t.Errorf("show: %d %v", code, w)
	}
	code, out = send(t, "PUT", srv2.URL+"/api/deals/wishlist/10/muted", nil)
	if w = out["wishlist"].(map[string]any); code != 200 || w["items"].([]any)[0].(map[string]any)["muted"] != true {
		t.Errorf("mute: %d %v", code, w)
	}
	code, out = send(t, "DELETE", srv2.URL+"/api/deals/wishlist/10/muted", nil)
	if w = out["wishlist"].(map[string]any); code != 200 || w["items"].([]any)[0].(map[string]any)["muted"] != false {
		t.Errorf("unmute: %d %v", code, w)
	}
	for _, m := range []string{"PUT", "DELETE"} {
		for _, path := range []string{"/api/deals/wishlist/999/hidden", "/api/deals/wishlist/999/muted", "/api/deals/wishlist/x/hidden"} {
			if code, _ := send(t, m, srv2.URL+path, nil); code != 404 {
				t.Errorf("%s %s: %d", m, path, code)
			}
		}
	}
	// A standing alert counts in the badge, and opening Deals clears it.
	d.Exec(`UPDATE wish_item SET last_alert_price = 500, last_alert_kind = 'historical', last_alert_at = '2026-01-01T00:00:00Z', seen = 0 WHERE appid = 10`)
	_, out = send(t, "GET", srv2.URL+"/api/deals", nil)
	if out["unseen"] != 1.0 {
		t.Errorf("unseen %v", out["unseen"])
	}
	if al := out["wishlist"].(map[string]any)["items"].([]any)[0].(map[string]any)["alert"].(map[string]any); al["kind"] != "historical" || al["seen"] != false {
		t.Errorf("alert %v", al)
	}
	if _, out = send(t, "POST", srv2.URL+"/api/deals/alerts/seen", nil); out["unseen"] != 0.0 {
		t.Errorf("seen: %v", out["unseen"])
	}
	// A private wishlist is explained, and what was stored stays.
	srv3 := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Deals: wishFake{}, Wishlist: fakeWishlist{}}).Handler())
	t.Cleanup(srv3.Close)
	send(t, "POST", srv3.URL+"/api/deals/refresh", nil)
	out = waitDealsIdle(t, srv3.URL)
	if w = out["wishlist"].(map[string]any); !strings.Contains(w["status"].(string), "not public") || len(w["items"].([]any)) != 3 {
		t.Errorf("private: %v", w)
	}
}
