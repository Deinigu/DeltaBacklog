package deals

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLive asks the real IsThereAnyDeal about a few games. Run it with the key in the environment:
//
//	set -a; . ./.env; set +a; DELTABACKLOG_LIVE=1 go test ./internal/deals -run TestLive -v
//
// It prints prices only, never the key.
func TestLive(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") == "" {
		t.Skip("set DELTABACKLOG_LIVE=1 to call the real service")
	}
	c := New(os.Getenv("ITAD_API_KEY"))
	if !c.Configured() {
		t.Skip("ITAD_API_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var ids []string
	names := map[string]string{}
	for _, title := range []string{"Hollow Knight", "Disco Elysium", "Hades"} {
		g, ok, err := c.LookupTitle(ctx, title)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", title, ok, err)
		}
		t.Logf("%s -> %q (same title: %v)", title, g.Title, SameTitle(g.Title, title))
		ids = append(ids, g.ID)
		names[g.ID] = title
	}
	if g, ok, err := c.LookupAppID(ctx, 367520); err != nil || !ok {
		t.Fatalf("appid: %v %v", ok, err)
	} else {
		t.Logf("steam app 367520 -> %q", g.Title)
	}
	res, err := c.Overview(ctx, ids, "ES")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		o := res[id]
		if o.Current != nil {
			t.Logf("%s: %.2f %s at %s (regular %.2f, -%d%%)", names[id], float64(o.Current.Price.Cents)/100, o.Current.Price.Currency, o.Current.Shop, float64(o.Current.Regular.Cents)/100, o.Current.Cut)
		} else {
			t.Logf("%s: no current price", names[id])
		}
		if o.Low != nil {
			t.Logf("%s: lowest ever %.2f %s at %s on %s", names[id], float64(o.Low.Price.Cents)/100, o.Low.Price.Currency, o.Low.Shop, o.Low.At)
		}
	}
	list, err := c.Search(ctx, "Hades")
	if err != nil || len(list) == 0 {
		t.Fatalf("search: %v %v", list, err)
	}
	t.Logf("search \"Hades\" -> %d results, first %q", len(list), list[0].Title)
}
