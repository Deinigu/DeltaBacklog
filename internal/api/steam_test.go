package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/steam"
)

// steamFake is an offline Steam account.
type steamFake struct {
	off  bool
	apps []steam.App
	err  error
}

func (f *steamFake) Configured() bool                           { return !f.off }
func (f *steamFake) Owned(context.Context) ([]steam.App, error) { return f.apps, f.err }

func steamServer(t *testing.T, src steam.Source) string {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for i, title := range []string{"Hollow Knight", "Portal 2"} {
		d.Exec(`INSERT INTO game(id, title, title_sort, status_id) VALUES (?, ?, lower(?), (SELECT id FROM status WHERE code = 'backlog'))`, i+1, title, title)
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), Steam: src}).Handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func waitSteam(t *testing.T, base string) map[string]any {
	t.Helper()
	for i := 0; i < 200; i++ {
		_, out := send(t, "GET", base+"/api/steam", nil)
		if r := out["refresh"].(map[string]any); r["running"] != true && r["finished_at"] != nil {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the sync did not finish")
	return nil
}

func TestSteamSyncAndHoursInTheLibrary(t *testing.T) {
	base := steamServer(t, &steamFake{apps: []steam.App{{ID: 367520, Name: "Hollow Knight", Playtime: 125, LastPlayed: 1700000000}, {ID: 5, Name: "Stray", Playtime: 0}}})
	code, out := send(t, "GET", base+"/api/steam", nil)
	if code != 200 || out["configured"] != true || len(out["apps"].([]any)) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = send(t, "POST", base+"/api/steam/sync", nil); code != http.StatusAccepted {
		t.Fatalf("sync %d", code)
	}
	out = waitSteam(t, base)
	if r := out["refresh"].(map[string]any); r["failed"] == true || !strings.Contains(r["report"].(string), "2 owned") {
		t.Fatalf("refresh %v", r)
	}
	if apps := out["apps"].([]any); len(apps) != 2 || out["synced_at"] == "" {
		t.Fatalf("apps %v", out)
	}
	// The hours reach the library list.
	res, err := http.Get(base + "/api/games")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var games []map[string]any
	if err := jsonDecode(res.Body, &games); err != nil {
		t.Fatal(err)
	}
	for _, g := range games {
		switch g["title"] {
		case "Hollow Knight":
			if g["playtime_min"] != 125.0 || g["steam_last_played"] != 1700000000.0 {
				t.Errorf("Hollow Knight %v", g)
			}
		case "Portal 2":
			if g["playtime_min"] != nil {
				t.Errorf("Portal 2 %v", g)
			}
		}
	}
}

func TestSteamFailuresAreSaidPlainly(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{steam.ErrPrivate, "Public"},
		{errors.New("Steam refused the API key (check STEAM_API_KEY in .env)"), "API key"},
		{&steam.RateLimitError{}, "rate limit"},
		{context.DeadlineExceeded, "too long"},
	} {
		base := steamServer(t, &steamFake{err: tc.err})
		if code, _ := send(t, "POST", base+"/api/steam/sync", nil); code != http.StatusAccepted {
			t.Fatalf("sync %d", code)
		}
		out := waitSteam(t, base)
		r := out["refresh"].(map[string]any)
		if r["failed"] != true || !strings.Contains(r["report"].(string), tc.want) {
			t.Errorf("%v: %v", tc.err, r)
		}
	}
}

func TestSteamNotSetUp(t *testing.T) {
	for _, src := range []steam.Source{nil, &steamFake{off: true}} {
		base := steamServer(t, src)
		if _, out := send(t, "GET", base+"/api/steam", nil); out["configured"] != false {
			t.Errorf("configured: %v", out)
		}
		if code, _ := send(t, "POST", base+"/api/steam/sync", nil); code != http.StatusServiceUnavailable {
			t.Errorf("sync: %d", code)
		}
	}
}

func TestSteamMatchAndIgnoreEndpoints(t *testing.T) {
	base := steamServer(t, &steamFake{apps: []steam.App{{ID: 367520, Name: "Hollow Knight", Playtime: 125}, {ID: 5, Name: "Stray", Playtime: 30}}})
	send(t, "POST", base+"/api/steam/sync", nil)
	waitSteam(t, base)
	app := func(out map[string]any, id float64) map[string]any {
		for _, a := range out["apps"].([]any) {
			if m := a.(map[string]any); m["app_id"] == id {
				return m
			}
		}
		t.Fatalf("no app %v in %v", id, out)
		return nil
	}
	code, out := send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"game_id": 2})
	if a := app(out, 5); code != 200 || a["game_id"] != 2.0 || a["source"] != "user" {
		t.Fatalf("match %d %v", code, out)
	}
	if code, out = send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"none": true}); code != 200 || app(out, 5)["game_id"] != nil || app(out, 5)["source"] != "none" {
		t.Errorf("none %d %v", code, out)
	}
	if code, out = send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"reset": true}); code != 200 || app(out, 5)["source"] != nil {
		t.Errorf("reset %d %v", code, out)
	}
	if code, out = send(t, "PUT", base+"/api/steam/apps/5/ignore", map[string]any{"ignored": true}); code != 200 || app(out, 5)["ignored"] != true {
		t.Errorf("ignore %d %v", code, out)
	}
	if code, out = send(t, "PUT", base+"/api/steam/apps/5/ignore", map[string]any{"ignored": false}); code != 200 || app(out, 5)["ignored"] != false {
		t.Errorf("unignore %d %v", code, out)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"none": true, "reset": true}, map[string]any{"game_id": 1, "none": true}, map[string]any{"game_id": -3}} {
		if code, _ = send(t, "PUT", base+"/api/steam/apps/5/match", body); code != 400 {
			t.Errorf("%v: %d", body, code)
		}
	}
	for _, c := range []struct{ path, kind string }{{"/api/steam/apps/999/match", "match"}, {"/api/steam/apps/999/ignore", "ignore"}, {"/api/steam/apps/x/ignore", "ignore"}} {
		if code, _ = send(t, "PUT", base+c.path, map[string]any{"none": true, "ignored": true}); code != 404 {
			t.Errorf("%s: %d", c.path, code)
		}
	}
	if code, _ = send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"game_id": 77}); code != 404 {
		t.Errorf("unknown game: %d", code)
	}
}

func jsonDecode(r interface{ Read([]byte) (int, error) }, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// /api/games is cached: the hours written by a sync, a changed match or an ignore must show in the next read.
func TestSteamChangesReachTheCachedGameList(t *testing.T) {
	base := steamServer(t, &steamFake{apps: []steam.App{{ID: 367520, Name: "Hollow Knight", Playtime: 125}, {ID: 5, Name: "Stray", Playtime: 30}}})
	hours := func(title string) any {
		t.Helper()
		res, err := http.Get(base + "/api/games")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var games []map[string]any
		if err := jsonDecode(res.Body, &games); err != nil {
			t.Fatal(err)
		}
		for _, g := range games {
			if g["title"] == title {
				return g["playtime_min"]
			}
		}
		t.Fatalf("no game %q", title)
		return nil
	}
	if hours("Hollow Knight") != nil { // fills the cache
		t.Fatal("hours before any sync")
	}
	send(t, "POST", base+"/api/steam/sync", nil)
	waitSteam(t, base)
	if h := hours("Hollow Knight"); h != 125.0 {
		t.Fatalf("after the sync: %v", h)
	}
	if code, _ := send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"game_id": 1}); code != 200 {
		t.Fatal(code)
	}
	if h := hours("Hollow Knight"); h != 155.0 {
		t.Errorf("after a match: %v", h)
	}
	send(t, "PUT", base+"/api/steam/apps/5/match", map[string]any{"none": true})
	if h := hours("Hollow Knight"); h != 125.0 {
		t.Errorf("after unmatching: %v", h)
	}
}
