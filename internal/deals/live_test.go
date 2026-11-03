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

// TestLiveHistory reads the price history of one well-known game and checks that the two-year low parses and is not
// below the all-time low. Same way to run as TestLive; prints prices only.
func TestLiveHistory(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") == "" {
		t.Skip("set DELTABACKLOG_LIVE=1 to call the real service")
	}
	c := New(os.Getenv("ITAD_API_KEY"))
	if !c.Configured() {
		t.Skip("ITAD_API_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	g, ok, err := c.LookupTitle(ctx, "Hollow Knight")
	if err != nil || !ok {
		t.Fatalf("lookup: %v %v", ok, err)
	}
	ov, err := c.Overview(ctx, []string{g.ID}, "ES")
	if err != nil || ov[g.ID].Low == nil {
		t.Fatalf("overview: %v", err)
	}
	low, ok, err := c.LowSince(ctx, g.ID, "ES", time.Now().Add(-HistoryWindow))
	if err != nil || !ok {
		t.Fatalf("history: %v %v", ok, err)
	}
	t.Logf("%s: lowest in 2 years %s, lowest ever %s", g.Title, low, ov[g.ID].Low.Price)
	if low.Currency != ov[g.ID].Low.Price.Currency || low.Cents < ov[g.ID].Low.Price.Cents {
		t.Errorf("the two-year low %s is below the all-time low %s", low, ov[g.ID].Low.Price)
	}
}

// TestLiveWishlist reads the real Steam wishlist of STEAM_ID, names its games and matches them with ITAD in bulk (one
// request per 200 games, not one per game), then reads the prices of the matched ones. It prints counts only, never the
// SteamID or the key. Run: set -a; . ./.env; set +a; export STEAM_ID=...; DELTABACKLOG_LIVE=1 go test ./internal/deals -run TestLiveWishlist -v
func TestLiveWishlist(t *testing.T) {
	if os.Getenv("DELTABACKLOG_LIVE") == "" {
		t.Skip("set DELTABACKLOG_LIVE=1 to call the real services")
	}
	c := New(os.Getenv("ITAD_API_KEY"))
	steam, err := NewSteam(os.Getenv("STEAM_ID"))
	if err != nil || steam == nil || !c.Configured() {
		t.Skip("ITAD_API_KEY or a valid STEAM_ID is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	entries, err := steam.Wishlist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, e := range entries {
		ids = append(ids, e.AppID)
	}
	names := map[int64]string{}
	requests := 0
	for rest := ids; len(rest) > 0; {
		n := min(len(rest), MaxTitleBatch)
		got, err := steam.Titles(ctx, rest[:n])
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range got {
			names[k] = v
		}
		rest, requests = rest[n:], requests+1
	}
	t.Logf("wishlist: %d games, %d named in %d Steam requests", len(ids), len(names), requests)
	matched := map[int64]string{}
	requests = 0
	for rest := ids; len(rest) > 0; {
		n := min(len(rest), MaxLookupBatch)
		got, err := c.LookupAppIDs(ctx, rest[:n])
		if err != nil {
			t.Fatalf("bulk lookup: %v (a rate limit? wait a few minutes and run again)", err)
		}
		for k, v := range got {
			matched[k] = v
		}
		rest, requests = rest[n:], requests+1
	}
	t.Logf("matched %d of %d with ITAD in %d requests", len(matched), len(ids), requests)
	var itad []string
	for _, v := range matched {
		itad = append(itad, v)
	}
	priced, requests := 0, 0
	for rest := itad; len(rest) > 0; {
		n := min(len(rest), MaxBatch)
		res, err := c.Overview(ctx, rest[:n], "ES")
		if err != nil {
			t.Fatalf("overview: %v", err)
		}
		for _, o := range res {
			if o.Current != nil {
				priced++
			}
		}
		rest, requests = rest[n:], requests+1
	}
	t.Logf("%d with a price now, read in %d requests", priced, requests)
	if len(matched) == 0 {
		t.Error("nothing matched")
	}
}
