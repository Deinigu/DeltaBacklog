package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

type sinkNotifier struct {
	sent []string
	err  error
}

func (s *sinkNotifier) Send(_ context.Context, text, _ string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, text)
	return nil
}

func TestDealsMuteAndSeen(t *testing.T) {
	srv, d := dealsServer(t, dealsFake{})
	send(t, "POST", srv.URL+"/api/deals/refresh", nil)
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if _, out := send(t, "GET", srv.URL+"/api/deals", nil); !out["refresh"].(map[string]any)["running"].(bool) && len(out["prices"].([]any)) > 0 {
			break
		}
	}
	// The fake's price (5.00, -50%) is above its low (4.00): no alert yet. Record one by hand.
	d.Exec(`INSERT INTO deal_alert(game_id, last_alert_price, last_alert_kind, last_alert_at, seen) VALUES (1, 500, 'two_year', '2026-01-01T00:00:00Z', 0)`)
	_, out := send(t, "GET", srv.URL+"/api/deals", nil)
	alerts := out["alerts"].([]any)
	if len(alerts) != 1 || out["unseen"] != 1.0 || out["telegram"] != false || len(out["muted"].([]any)) != 0 {
		t.Fatalf("%v", out)
	}
	if a := alerts[0].(map[string]any); a["game_id"] != 1.0 || a["kind"] != "two_year" || a["seen"] != false {
		t.Errorf("alert %v", a)
	}
	code, out := send(t, "POST", srv.URL+"/api/deals/alerts/seen", nil)
	if code != 200 || out["unseen"] != 0.0 || len(out["alerts"].([]any)) != 1 {
		t.Errorf("seen: %d %v", code, out)
	}
	code, out = send(t, "PUT", srv.URL+"/api/deals/muted/1", nil)
	if code != 200 || len(out["muted"].([]any)) != 1 || len(out["alerts"].([]any)) != 0 {
		t.Errorf("mute: %d %v", code, out)
	}
	code, out = send(t, "DELETE", srv.URL+"/api/deals/muted/1", nil)
	if code != 200 || len(out["muted"].([]any)) != 0 || len(out["alerts"].([]any)) != 1 {
		t.Errorf("unmute: %d %v", code, out)
	}
	if code, _ := send(t, "PUT", srv.URL+"/api/deals/muted/99", nil); code != 404 {
		t.Errorf("unknown game: %d", code)
	}
	if code, _ := send(t, "PUT", srv.URL+"/api/deals/muted/x", nil); code != 404 {
		t.Errorf("bad id: %d", code)
	}
}

func TestDealsRefreshWaitsForTheScheduledCheck(t *testing.T) {
	srv, d := dealsServer(t, dealsFake{})
	release, err := deals.AcquireRun(d, deals.HolderCheck) // what `deals-check` holds while it runs
	if err != nil {
		t.Fatal(err)
	}
	if code, out := send(t, "POST", srv.URL+"/api/deals/refresh", nil); code != http.StatusConflict {
		t.Errorf("refresh during the check: %d %v", code, out)
	}
	release()
	// A lock an app run left behind (a restart in the middle of a refresh) is not blamed on the scheduled check.
	stale, _ := deals.AcquireRun(d, deals.HolderApp)
	if code, out := send(t, "POST", srv.URL+"/api/deals/refresh", nil); code != http.StatusConflict || strings.Contains(out["error"].(string), "scheduled") {
		t.Errorf("refresh with a leftover lock: %d %v", code, out)
	}
	stale()
	if code, _ := send(t, "POST", srv.URL+"/api/deals/refresh", nil); code != http.StatusAccepted {
		t.Errorf("refresh after: %d", code)
	}
	// ... and the lock is given back when the app's run ends.
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		if r, err := deals.AcquireRun(d, deals.HolderApp); err == nil {
			r()
			return
		}
	}
	t.Error("the lock was not released")
}

func TestDealsTelegramTest(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	n := &sinkNotifier{}
	s := &Server{DB: d, CoverDir: t.TempDir(), Deals: dealsFake{}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	// Not set up: 503.
	if code, _ := send(t, "POST", srv.URL+"/api/deals/telegram-test", nil); code != http.StatusServiceUnavailable {
		t.Errorf("not set up: %d", code)
	}
	s.Notify = n
	if _, out := send(t, "GET", srv.URL+"/api/deals", nil); out["telegram"] != true {
		t.Errorf("telegram flag: %v", out["telegram"])
	}
	if code, out := send(t, "POST", srv.URL+"/api/deals/telegram-test", nil); code != 200 || out["sent"] != true || len(n.sent) != 1 {
		t.Errorf("test message: %d %v %q", code, out, n.sent)
	}
	n.err = errors.New("Telegram answered HTTP 400: chat not found")
	code, out := send(t, "POST", srv.URL+"/api/deals/telegram-test", nil)
	if code != http.StatusBadGateway || out["error"] == nil {
		t.Errorf("failure: %d %v", code, out)
	}
}
