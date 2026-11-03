package deals

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Games that turn out, once matched, to be library games are not "the rest": a run that finished says so.
func TestReportDoesNotCountLibraryTwinsAsTheRest(t *testing.T) {
	d := newDB(t)
	src := &bulkSource{histSource: wishSource(), known: map[int64]string{}, failAt: -1}
	addGame(t, d, 1, "Library Twin", "backlog", "")
	d.Exec(`INSERT INTO deal_match(game_id, itad_id, source) VALUES (1, 'dup', 'user')`)
	src.prices["dup"] = Overview{Current: cur(900, 2000, 55)}
	var ids []int64
	for i := int64(2000); i < 2005; i++ {
		ids = append(ids, i)
		src.known[i] = "bulk-" + strconv.FormatInt(i, 10)
		src.prices[src.known[i]] = Overview{Current: cur(900, 2000, 55)}
	}
	src.known[2000] = "dup" // the same ITAD game as the library game
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: &namedWish{fakeWish: &fakeWish{entries: entries(ids...)}}})
	if err != nil || rep.Stopped != "" || rep.WishGames != 4 || rep.WishPriced != 4 {
		t.Fatalf("%+v %v", rep, err)
	}
	if strings.Contains(rep.String(), "rest next run") || !strings.Contains(rep.String(), "Wishlist: 4 games priced.") {
		t.Errorf("report: %s", rep)
	}
}

// Any error of the matching step, not only a rate limit, still lets the matched games be priced; it is reported.
func TestMatchingErrorStillPricesWhatIsMatched(t *testing.T) {
	d := newDB(t)
	src, wl := manyApps(30)
	if _, err := Refresh(context.Background(), d, src, Options{Wishlist: wl}); err != nil {
		t.Fatal(err)
	}
	wl.entries = entries(append(idsOf(wl.entries), 5000, 5001)...)
	src.chunks, src.failAt, src.failed = nil, 0, errors.New("IsThereAnyDeal answered 500 Internal Server Error")
	src.fakeSource.overview = nil
	rep, err := Refresh(context.Background(), d, src, Options{Wishlist: wl})
	if err != nil || !strings.Contains(rep.Stopped, "matching the wishlist failed: IsThereAnyDeal answered 500") {
		t.Fatalf("%+v %v", rep, err)
	}
	if rep.WishPriced != 27 || len(src.overview) == 0 || rep.WishGames != 32 || rep.WishUnmatched != 3 {
		t.Errorf("the 27 matched games are priced again: %+v", rep)
	}
	if !strings.Contains(rep.String(), "Stopped: matching the wishlist failed") {
		t.Errorf("report: %s", rep)
	}
}

func idsOf(es []WishEntry) []int64 {
	var out []int64
	for _, e := range es {
		out = append(out, e.AppID)
	}
	return out
}

// The history budget is shared: library games first, the wishlist gets what is left.
func TestHistoryBudgetIsSharedWithTheWishlist(t *testing.T) {
	d := newDB(t)
	src := &histSource{fakeSource: &fakeSource{byTitle: map[string]Game{}, byApp: map[int64]Game{}, prices: map[string]Overview{}}, lows: map[string]int64{}}
	var wid []int64
	for i := 1; i <= 10; i++ {
		title, id := "Lib "+strconv.Itoa(i), "lib"+strconv.Itoa(i)
		addGame(t, d, int64(i), title, "backlog", "")
		src.byTitle[title] = Game{ID: id, Title: title}
		setPrice(src, id, cur(700, 1500, 53), 600)
		app, wl := int64(3000+i), "wl"+strconv.Itoa(i)
		wid = append(wid, app)
		src.byApp[app] = Game{ID: wl, Title: "Wish " + strconv.Itoa(i)}
		setPrice(src, wl, cur(700, 1500, 53), 600)
	}
	if _, err := CheckWith(context.Background(), d, src, &fakeWish{entries: entries(wid...)}, nil); err != nil {
		t.Fatal(err)
	}
	lib, wish := 0, 0
	for _, id := range src.calls {
		if strings.HasPrefix(id, "lib") {
			lib++
		} else {
			wish++
		}
	}
	if lib != 10 || wish != HistoryBudget-10 {
		t.Errorf("library %d, wishlist %d calls, want 10 and %d", lib, wish, HistoryBudget-10)
	}
}

func instant(waits *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		*waits = append(*waits, d)
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
}

func TestRetryAfterIsWaitedOutOnce(t *testing.T) {
	ctx := context.Background()
	var calls int
	var waits []time.Duration
	var always bool
	retry := "2"
	c := fakeITAD(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 || always {
			w.Header().Set("Retry-After", retry)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"app/1":"u1"}`))
	})
	c.sleep = instant(&waits)

	// A short Retry-After is waited out once and the request is sent again (here the bulk lookup).
	if got, err := c.LookupAppIDs(ctx, []int64{1}); err != nil || got[1] != "u1" || calls != 2 || len(waits) != 1 || waits[0] != 2*time.Second {
		t.Fatalf("one retry after the wait: %v %v calls %d waits %v", got, err, calls, waits)
	}
	// A second 429 is returned, not waited out again.
	calls, waits, always = 0, nil, true
	_, err := c.LookupAppIDs(ctx, []int64{1})
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 2*time.Second || calls != 2 || len(waits) != 1 {
		t.Fatalf("a second 429 is returned: %v calls %d waits %v", err, calls, waits)
	}
	// A long Retry-After is not waited out at all.
	calls, waits, retry = 0, nil, "60"
	if _, err := c.LookupAppIDs(ctx, []int64{1}); !errors.As(err, &rl) || rl.RetryAfter != time.Minute || calls != 1 || len(waits) != 0 {
		t.Errorf("a long wait stops at once: %v calls %d waits %v", err, calls, waits)
	}
	// No Retry-After: nothing to wait for.
	calls, waits, retry = 0, nil, ""
	if _, err := c.LookupAppIDs(ctx, []int64{1}); !errors.As(err, &rl) || calls != 1 || len(waits) != 0 {
		t.Errorf("no header: %v calls %d", err, calls)
	}
	// The wait respects the context.
	retry = "5"
	c.sleep = func(time.Duration) <-chan time.Time { return nil } // never fires
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.LookupAppIDs(cctx, []int64{1}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled while waiting: %v", err)
	}
}

func TestRetryAfterForms(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{
		"7": 7 * time.Second, " 30 ": 30 * time.Second, "0": 0, "-3": 0, "": 0, "soon": 0,
		now.Add(10 * time.Second).Format(http.TimeFormat): 10 * time.Second,
		now.Add(-time.Hour).Format(http.TimeFormat):       0,
	} {
		if got := retryAfter(in, now); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}
