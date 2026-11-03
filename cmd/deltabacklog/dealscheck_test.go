package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

type fakeITAD struct{ off bool }

func (f fakeITAD) Configured() bool { return !f.off }
func (fakeITAD) LookupAppID(_ context.Context, id int64) (deals.Game, bool, error) {
	if id == 10 {
		return deals.Game{ID: "w10", Title: "Hollow Knight"}, true, nil
	}
	return deals.Game{}, false, nil
}
func (fakeITAD) LookupTitle(_ context.Context, t string) (deals.Game, bool, error) {
	return deals.Game{ID: "id", Title: t}, true, nil
}
func (fakeITAD) Search(context.Context, string) ([]deals.Game, error) { return nil, nil }
func (fakeITAD) Overview(_ context.Context, ids []string, _ string) (map[string]deals.Overview, error) {
	out := map[string]deals.Overview{}
	for _, id := range ids {
		out[id] = deals.Overview{
			Current: &deals.Current{Shop: "Steam", Price: deals.Money{Cents: 500, Currency: "EUR"}, Regular: deals.Money{Cents: 1500, Currency: "EUR"}, Cut: 66, URL: "https://shop.example/x"},
			Low:     &deals.Low{Shop: "GOG", Price: deals.Money{Cents: 500, Currency: "EUR"}}}
	}
	return out, nil
}

type collect struct{ sent []string }

func (c *collect) Send(_ context.Context, s, _ string) error { c.sent = append(c.sent, s); return nil }

func TestDealsCheckCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO game(id, title, title_sort, status_id) VALUES (1, 'Hades', 'hades', (SELECT id FROM status WHERE code = 'backlog'))`)
	d.Close()

	var out bytes.Buffer
	if code := runDealsCheck(path, fakeITAD{off: true}, nil, nil, &out); code != 1 || !strings.Contains(out.String(), "ITAD_API_KEY") {
		t.Errorf("no key: %d %q", code, out.String())
	}
	out.Reset()
	if code := runDealsCheck(filepath.Join(t.TempDir(), "none.db"), fakeITAD{}, nil, nil, &out); code != 1 {
		t.Errorf("no database: %d", code)
	}

	n := &collect{}
	out.Reset()
	if code := runDealsCheck(path, fakeITAD{}, nil, n, &out); code != 0 || len(n.sent) != 1 || !strings.Contains(n.sent[0], "Hades") || !strings.Contains(out.String(), "1 new price alert, sent") {
		t.Fatalf("check: %d %q %q", code, out.String(), n.sent)
	}
	// Again: the same deal is not announced twice.
	out.Reset()
	if code := runDealsCheck(path, fakeITAD{}, nil, n, &out); code != 0 || len(n.sent) != 1 || !strings.Contains(out.String(), "0 new price alerts") {
		t.Errorf("second check: %d %q %q", code, out.String(), n.sent)
	}

	// While the app's own refresh holds the lock, the check does nothing (and is no failure).
	lockWait, lockPoll = 60*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { lockWait, lockPoll = 20*time.Minute, 30*time.Second })
	d, _ = db.Open(path)
	defer d.Close()
	release, _ := deals.AcquireRun(d, deals.HolderApp)
	out.Reset()
	if code := runDealsCheck(path, fakeITAD{}, nil, n, &out); code != 1 || !strings.Contains(out.String(), "waiting") || !strings.Contains(out.String(), "gave up") {
		t.Errorf("locked for good: %d %q", code, out.String())
	}
	// A refresh that ends while the check waits lets it run (it waits instead of losing the day).
	lockWait = 5 * time.Second
	go func() { time.Sleep(50 * time.Millisecond); release() }()
	out.Reset()
	if code := runDealsCheck(path, fakeITAD{}, nil, n, &out); code != 0 || !strings.Contains(out.String(), "prices:") {
		t.Errorf("after the wait: %d %q", code, out.String())
	}
}

func TestTelegramFromEnvLogsNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(os.Stderr); log.SetFlags(log.LstdFlags) })
	t.Setenv("TELEGRAM_BOT_TOKEN", "SECRET-not-a-token")
	t.Setenv("TELEGRAM_CHAT_ID", "-100123")
	if telegramFromEnv() != nil {
		t.Error("a bad token must turn Telegram off")
	}
	if got := buf.String(); !strings.Contains(got, "Telegram alerts off: bad token format") || strings.Contains(got, "SECRET") {
		t.Errorf("log %q", got)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	buf.Reset()
	if telegramFromEnv() != nil || buf.Len() != 0 {
		t.Errorf("unset: %q", buf.String())
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456789:AAH-testtesttesttesttesttesttesttest1")
	t.Setenv("TELEGRAM_CHAT_ID", "-100123")
	if telegramFromEnv() == nil {
		t.Error("a valid pair must turn Telegram on")
	}
}

type oneWish struct{}

func (oneWish) Wishlist(context.Context) ([]deals.WishEntry, error) {
	return []deals.WishEntry{{AppID: 10}}, nil
}

func TestDealsCheckReadsTheWishlist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	n := &collect{}
	var out bytes.Buffer
	if code := runDealsCheck(path, fakeITAD{}, oneWish{}, n, &out); code != 0 || !strings.Contains(out.String(), "Wishlist: 1 game priced") || len(n.sent) != 1 || !strings.Contains(n.sent[0], "Wishlist") {
		t.Fatalf("check: %d %q %q", code, out.String(), n.sent)
	}
}

func TestSteamFromEnvNeverLogsTheID(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	t.Setenv("STEAM_ID", "")
	if steamFromEnv(t.TempDir()) != nil {
		t.Error("no STEAM_ID, no wishlist")
	}
	t.Setenv("STEAM_ID", "not-an-id-123")
	if steamFromEnv(t.TempDir()) != nil || strings.Contains(logged.String(), "not-an-id-123") || !strings.Contains(logged.String(), "17-digit") {
		t.Errorf("bad id: %q", logged.String())
	}
	t.Setenv("STEAM_ID", "76561198000000000")
	if steamFromEnv(t.TempDir()) == nil || strings.Contains(logged.String(), "76561198000000000") {
		t.Errorf("good id: %q", logged.String())
	}
}
