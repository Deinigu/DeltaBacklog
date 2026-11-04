package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

// shopsFake adds the shop list to the offline ITAD.
type shopsFake struct {
	dealsFake
	calls int
}

func (s *shopsFake) Shops(context.Context, string) ([]string, error) {
	s.calls++
	return []string{"GOG", "Steam"}, nil
}

func TestDealsStoresSetting(t *testing.T) {
	srv, _ := dealsServer(t, &shopsFake{})
	code, out := send(t, "GET", srv.URL+"/api/deals/stores", nil)
	if code != 200 || len(out["stores"].([]any)) != 0 || out["threshold"] != float64(deals.DefaultThreshold) {
		t.Fatalf("default: %d %v", code, out)
	}
	code, out = send(t, "PUT", srv.URL+"/api/deals/stores", map[string]any{"stores": []string{" Steam ", "GOG"}, "threshold": 25})
	if code != 200 {
		t.Fatalf("put: %d %v", code, out)
	}
	st := out["stores"].(map[string]any) // the answer is the Deals list, which carries the ranking
	if len(st["stores"].([]any)) != 2 || st["stores"].([]any)[0] != "Steam" || st["threshold"] != float64(25) {
		t.Errorf("list: %v", st)
	}
	if code, out = send(t, "GET", srv.URL+"/api/deals/stores", nil); code != 200 || out["threshold"] != float64(25) {
		t.Errorf("get: %v", out)
	}
	many := make([]string, 31)
	for i := range many {
		many[i] = "Shop " + strings.Repeat("x", i+1)
	}
	for name, body := range map[string]any{
		"too many":  map[string]any{"stores": many, "threshold": 15},
		"markup":    map[string]any{"stores": []string{"<b>"}, "threshold": 15},
		"twice":     map[string]any{"stores": []string{"Steam", "STEAM"}, "threshold": 15},
		"high":      map[string]any{"stores": []string{"Steam"}, "threshold": 101},
		"negative":  map[string]any{"stores": []string{"Steam"}, "threshold": -5},
		"not array": map[string]any{"stores": "Steam", "threshold": 15},
	} {
		if code, _ := send(t, "PUT", srv.URL+"/api/deals/stores", body); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	if _, out = send(t, "GET", srv.URL+"/api/deals/stores", nil); out["threshold"] != float64(25) {
		t.Errorf("a refused change must change nothing: %v", out)
	}
	if code, _ = send(t, "PUT", srv.URL+"/api/deals/stores", map[string]any{"stores": []string{}, "threshold": 0}); code != 200 {
		t.Errorf("clearing: %d", code)
	}
}

func TestDealsShopsList(t *testing.T) {
	f := &shopsFake{}
	srv, _ := dealsServer(t, f)
	code, out := send(t, "GET", srv.URL+"/api/deals/shops", nil)
	if code != 200 || len(out["shops"].([]any)) != 2 || f.calls != 1 {
		t.Fatalf("%d %v", code, out)
	}
	srv, _ = dealsServer(t, dealsFake{}) // a source that cannot list shops: an empty list, not an error
	if code, out = send(t, "GET", srv.URL+"/api/deals/shops", nil); code != 200 || len(out["shops"].([]any)) != 0 {
		t.Errorf("%d %v", code, out)
	}
	srv, _ = dealsServer(t, dealsFake{off: true})
	if code, _ = send(t, "GET", srv.URL+"/api/deals/shops", nil); code != http.StatusServiceUnavailable {
		t.Errorf("not configured: %d", code)
	}
}

func TestDealsStoresMissingThresholdKeepsTheCurrentOne(t *testing.T) {
	srv, _ := dealsServer(t, &shopsFake{})
	if code, _ := send(t, "PUT", srv.URL+"/api/deals/stores", map[string]any{"stores": []string{"Steam"}}); code != 200 {
		t.Fatalf("put: %d", code)
	}
	if _, out := send(t, "GET", srv.URL+"/api/deals/stores", nil); out["threshold"] != float64(deals.DefaultThreshold) {
		t.Errorf("default expected, got %v", out)
	}
	send(t, "PUT", srv.URL+"/api/deals/stores", map[string]any{"stores": []string{"Steam"}, "threshold": 30})
	send(t, "PUT", srv.URL+"/api/deals/stores", map[string]any{"stores": []string{"Steam", "GOG"}})
	if _, out := send(t, "GET", srv.URL+"/api/deals/stores", nil); out["threshold"] != float64(30) {
		t.Errorf("current expected, got %v", out)
	}
}
