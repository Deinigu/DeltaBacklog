package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

type fakeExpansions struct {
	mu    sync.Mutex
	lists map[int64][]meta.Expansion
	calls int
	err   error
	block chan struct{}
}

func (f *fakeExpansions) Expansions(ctx context.Context, ids []int64) (map[int64][]meta.Expansion, error) {
	f.mu.Lock()
	f.calls++
	block, err := f.block, f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	out := map[int64][]meta.Expansion{}
	for _, id := range ids {
		out[id] = f.lists[id]
	}
	return out, nil
}

func (f *fakeExpansions) TimeToBeat(ctx context.Context, ids []int64) (map[int64]meta.TimeToBeat, error) {
	return map[int64]meta.TimeToBeat{}, nil
}

const expCSV = `id,Title,Platform,Status,Style,VR,Genre,Boxart,Completed,Years_played,Series,Notas,Ownership,Played_with,Favorito,nc_order
1,The Witcher 3,PC,Finished,,f,,,f,,,,Owned,,f,1
2,Skyrim,PC,Backlog,,f,,,f,,,,Owned,,f,2
3,Unlinked,PC,Backlog,,f,,,f,,,,Owned,,f,3
`

func expServer(t *testing.T, f *fakeExpansions) (*Server, *httptest.Server, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(expCSV), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	d.Exec("UPDATE game SET igdb_id = 1942 WHERE title = 'The Witcher 3'")
	d.Exec("UPDATE game SET igdb_id = 2 WHERE title = 'Skyrim'")
	s := &Server{DB: d, CoverDir: t.TempDir(), ExpansionPosterFetch: func(string, string, int64) (string, error) { return "", context.Canceled }}
	if f != nil {
		s.Expansions = f
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv, d
}

var witcherExp = []meta.Expansion{
	{IGDBID: 100, Name: "Hearts of Stone", Release: 1444694400, Year: 2015, SteamAppID: 378648},
	{IGDBID: 101, Name: "Blood and Wine", Release: 1464134400, Year: 2016, SteamAppID: 378649},
}

func gamesOf(t *testing.T, srv *httptest.Server) map[string]Game {
	t.Helper()
	res, body := do(t, "GET", srv.URL+"/api/games", "")
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var list []Game
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]Game{}
	for _, g := range list {
		out[g.Title] = g
	}
	return out
}

func waitExp(t *testing.T, s *Server) expStatus {
	t.Helper()
	for i := 0; i < 300; i++ {
		if st := s.expJob.expStatus(); !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
	return expStatus{}
}

func TestRefreshExpansionsAndLibraryPayload(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp}}
	_, srv, _ := expServer(t, f)
	g := gamesOf(t, srv)["The Witcher 3"]
	if g.Expansions == nil || len(g.Expansions) != 0 || g.ExpansionsFetched != "" {
		t.Fatalf("before the first read: %+v", g)
	}
	res, body := do(t, "POST", srv.URL+"/api/games/"+itoa(g.ID)+"/expansions/refresh", `{}`)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var got Game
	json.Unmarshal(body, &got)
	if len(got.Expansions) != 2 || got.ExpansionsFetched == "" {
		t.Fatalf("%+v", got)
	}
	x := got.Expansions[0]
	if x.Title != "Hearts of Stone" || x.Status != nil || x.Ownership != nil || x.Date == nil || *x.Date != 1444694400 || *x.Year != 2015 || len(x.Years) != 0 || x.GameID != g.ID {
		t.Errorf("%+v", x)
	}
	// the cached library list sees them at once, and the raw JSON stays compact
	if n := len(gamesOf(t, srv)["The Witcher 3"].Expansions); n != 2 {
		t.Errorf("library list has %d", n)
	}
	_, raw := do(t, "GET", srv.URL+"/api/games", "")
	if strings.Contains(string(raw), `"notes":""`) && strings.Contains(string(raw), `"hidden"`) {
		t.Error("empty optional expansion fields are left out")
	}
	// a fresh game is not asked again when the drawer says only_if_stale; an explicit refresh asks again
	do(t, "POST", srv.URL+"/api/games/"+itoa(g.ID)+"/expansions/refresh", `{"only_if_stale":true}`)
	if f.calls != 1 {
		t.Errorf("only_if_stale asked IGDB again: %d", f.calls)
	}
	do(t, "POST", srv.URL+"/api/games/"+itoa(g.ID)+"/expansions/refresh", ``)
	if f.calls != 2 {
		t.Errorf("an explicit refresh asks IGDB: %d", f.calls)
	}
}

func TestRefreshExpansionsErrors(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{}}
	_, srv, _ := expServer(t, f)
	games := gamesOf(t, srv)
	if res, _ := do(t, "POST", srv.URL+"/api/games/999/expansions/refresh", `{}`); res.StatusCode != 404 {
		t.Errorf("unknown game: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/games/abc/expansions/refresh", `{}`); res.StatusCode != 404 {
		t.Errorf("bad id: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/games/"+itoa(games["Unlinked"].ID)+"/expansions/refresh", `{}`); res.StatusCode != 409 {
		t.Errorf("not linked: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/games/"+itoa(games["Skyrim"].ID)+"/expansions/refresh", `{bad`); res.StatusCode != 400 {
		t.Errorf("bad body: %d", res.StatusCode)
	}
	f.err = context.DeadlineExceeded
	if res, _ := do(t, "POST", srv.URL+"/api/games/"+itoa(games["Skyrim"].ID)+"/expansions/refresh", `{}`); res.StatusCode != 502 {
		t.Errorf("IGDB down: %d", res.StatusCode)
	}
	_, srv2, _ := expServer(t, nil)
	if res, _ := do(t, "POST", srv2.URL+"/api/games/"+itoa(games["Skyrim"].ID)+"/expansions/refresh", `{}`); res.StatusCode != 503 {
		t.Errorf("no IGDB keys: %d", res.StatusCode)
	}
}

func loadedExpansion(t *testing.T, srv *httptest.Server) (Expansion, int64) {
	t.Helper()
	g := gamesOf(t, srv)["The Witcher 3"]
	do(t, "POST", srv.URL+"/api/games/"+itoa(g.ID)+"/expansions/refresh", `{}`)
	g = gamesOf(t, srv)["The Witcher 3"]
	return g.Expansions[0], g.ID
}

func TestPatchExpansion(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp}}
	_, srv, d := expServer(t, f)
	x, _ := loadedExpansion(t, srv)
	url := srv.URL + "/api/expansions/" + itoa(x.ID)

	for name, body := range map[string]string{
		"unknown status": `{"status":"nope"}`, "bad year": `{"years":[1969]}`, "bad period": `{"periods":["sometimes"]}`,
		"negative minutes": `{"ttb_normally":-5}`, "bad source": `{"ttb_source":"guess"}`, "not json": `{`, "long ownership": `{"ownership":"` + strings.Repeat("a", 121) + `"}`,
	} {
		if res, _ := do(t, "PATCH", url, body); res.StatusCode != 400 {
			t.Errorf("%s: %d", name, res.StatusCode)
		}
	}
	if res, _ := do(t, "PATCH", srv.URL+"/api/expansions/9999", `{"notes":"x"}`); res.StatusCode != 404 {
		t.Errorf("unknown: %d", res.StatusCode)
	}
	if res, _ := do(t, "PATCH", srv.URL+"/api/expansions/abc", `{"notes":"x"}`); res.StatusCode != 404 {
		t.Errorf("bad id: %d", res.StatusCode)
	}

	// ownership, notes, hidden, the three times
	res, body := do(t, "PATCH", url, `{"ownership":"Owned","notes":"Great","hidden":true,"ttb_hastily":600,"ttb_normally":900,"ttb_source":"hltb","years":[2016],"periods":["childhood"]}`)
	var got Expansion
	json.Unmarshal(body, &got)
	if res.StatusCode != 200 || got.Ownership == nil || *got.Ownership != "Owned" || got.Notes != "Great" || !got.Hidden ||
		*got.TTBNormally != 900 || *got.TTBSource != "hltb" || len(got.Years) != 1 || got.Years[0] != 2016 || got.Periods[0] != "childhood" || got.Status != nil {
		t.Fatalf("%d %+v", res.StatusCode, got)
	}
	// the library list (cached) follows the write
	if n := gamesOf(t, srv)["The Witcher 3"].Expansions[0]; n.Notes != "Great" || !n.Hidden {
		t.Errorf("stale cache: %+v", n)
	}
	// an edit of one time keeps the others, and typing makes the owner the source
	_, body = do(t, "PATCH", url, `{"ttb_completely":1500}`)
	json.Unmarshal(body, &got)
	if *got.TTBHastily != 600 || *got.TTBCompletely != 1500 || *got.TTBSource != "manual" {
		t.Errorf("%+v", got)
	}
	_, body = do(t, "PATCH", url, `{"ttb_hastily":null,"ttb_normally":0,"ttb_completely":null}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if got.TTBHastily != nil || got.TTBNormally != nil || got.TTBSource != nil {
		t.Errorf("times cleared: %+v", got)
	}
	// ownership clears with ""
	_, body = do(t, "PATCH", url, `{"ownership":""}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if got.Ownership != nil {
		t.Error("ownership clears")
	}

	// the same rule as games: Playing or Finished records this year; Backlog does not; "" is Not tracked again
	year := time.Now().UTC().Year()
	do(t, "PATCH", url, `{"years":[]}`)
	_, body = do(t, "PATCH", url, `{"status":"backlog"}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if got.Status == nil || *got.Status != "backlog" || len(got.Years) != 0 {
		t.Errorf("backlog records no year: %+v", got)
	}
	_, body = do(t, "PATCH", url, `{"status":"playing"}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if *got.Status != "playing" || len(got.Years) != 1 || got.Years[0] != year {
		t.Errorf("playing records this year: %+v", got)
	}
	// an explicit list of years wins over the year the status change records
	_, body = do(t, "PATCH", url, `{"years":[2001],"status":"finished"}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if *got.Status != "finished" || len(got.Years) != 2 {
		t.Errorf("%+v", got)
	}
	_, body = do(t, "PATCH", url, `{"status":""}`)
	got = Expansion{}
	json.Unmarshal(body, &got)
	if got.Status != nil {
		t.Error("not tracked again")
	}
	// the base game was never touched
	var n int
	d.QueryRow(`SELECT count(*) FROM play WHERE game_id = ?`, x.GameID).Scan(&n)
	if n != 0 {
		t.Errorf("the base game got %d plays", n)
	}
	var st string
	d.QueryRow(`SELECT s.code FROM game g JOIN status s ON s.id = g.status_id WHERE g.id = ?`, x.GameID).Scan(&st)
	if st != "finished" {
		t.Errorf("game status %s", st)
	}
}

func TestExpansionSyncJob(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp, 2: {{IGDBID: 200, Name: "Dawnguard", SteamAppID: 211720}}}, block: make(chan struct{})}
	s, srv, _ := expServer(t, f)
	res, body := do(t, "POST", srv.URL+"/api/expansions/sync", `{}`)
	if res.StatusCode != 202 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/expansions/sync", `{}`); res.StatusCode != 409 {
		t.Errorf("one run at a time: %d", res.StatusCode)
	}
	var st expStatus
	_, body = do(t, "GET", srv.URL+"/api/expansions/sync", "")
	json.Unmarshal(body, &st)
	if !st.Running || st.Total != 2 || st.Kind != "find" {
		t.Errorf("%+v", st)
	}
	close(f.block)
	st = waitExp(t, s)
	if st.Failed || st.Report != "Found 3 expansions for 2 games." || st.Found != 3 || st.Games != 2 || st.Done != 2 || st.Finished == "" {
		t.Errorf("%+v", st)
	}
	if n := len(gamesOf(t, srv)["Skyrim"].Expansions); n != 1 {
		t.Errorf("the list is fresh when the run reads as finished: %d", n)
	}
	// nothing is due now: skipped within 30 days
	f.calls = 0
	do(t, "POST", srv.URL+"/api/expansions/sync", `{}`)
	st = waitExp(t, s)
	if f.calls != 0 || st.Total != 0 || st.Report != nothingDue {
		t.Errorf("fresh games are skipped: calls %d %+v", f.calls, st)
	}
}

func TestExpansionSyncCancelAndFailure(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp}, block: make(chan struct{})}
	s, srv, _ := expServer(t, f)
	do(t, "POST", srv.URL+"/api/expansions/sync", `{}`)
	res, _ := do(t, "DELETE", srv.URL+"/api/expansions/sync", "")
	if res.StatusCode != 200 {
		t.Errorf("cancel: %d", res.StatusCode)
	}
	st := waitExp(t, s)
	if st.Failed || !strings.HasPrefix(st.Report, "Stopped.") {
		t.Errorf("a stop is not an error: %+v", st)
	}
	f.block = nil
	f.err = context.DeadlineExceeded
	do(t, "POST", srv.URL+"/api/expansions/sync", `{}`)
	st = waitExp(t, s)
	if !st.Failed || !strings.Contains(st.Report, "IGDB could not be reached") {
		t.Errorf("%+v", st)
	}
	_, srv2, _ := expServer(t, nil)
	if res, _ := do(t, "POST", srv2.URL+"/api/expansions/sync", `{}`); res.StatusCode != 503 {
		t.Errorf("no keys: %d", res.StatusCode)
	}
}

func steamUpload(t *testing.T, srv *httptest.Server, field, content string) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile(field, "userdata.json")
	fw.Write([]byte(content))
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/expansions/steam-import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out bytes.Buffer
	out.ReadFrom(res.Body)
	return res, out.Bytes()
}

func TestSteamImport(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp, 2: {{IGDBID: 200, Name: "Dawnguard", SteamAppID: 211720}}}}
	s, srv, d := expServer(t, f)
	// refused before anything starts
	if res, body := steamUpload(t, srv, "file", `<html>`); res.StatusCode != 400 || !strings.Contains(string(body), "rgOwnedApps") {
		t.Errorf("not json: %d %s", res.StatusCode, body)
	}
	if res, _ := steamUpload(t, srv, "file", `{"rgWishlist":[1]}`); res.StatusCode != 400 {
		t.Errorf("no list: %d", res.StatusCode)
	}
	if res, _ := steamUpload(t, srv, "other", `{"rgOwnedApps":[1]}`); res.StatusCode != 400 {
		t.Errorf("no file field: %d", res.StatusCode)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/expansions/steam-import", `{"rgOwnedApps":[1]}`); res.StatusCode != 400 {
		t.Errorf("not multipart: %d", res.StatusCode)
	}
	big := `{"pad":"` + strings.Repeat("a", 6<<20) + `","rgOwnedApps":[1]}`
	if res, _ := steamUpload(t, srv, "file", big); res.StatusCode != 413 {
		t.Errorf("over 5 MB: %d", res.StatusCode)
	}
	if f.calls != 0 {
		t.Error("a refused file must not start anything")
	}
	// one owner-set ownership stays
	d.Exec(`INSERT INTO ownership(name) VALUES ('Borrowed')`)

	res, body := steamUpload(t, srv, "file", `{"rgOwnedApps":[378648,211720,5,6,7],"rgWishlist":[9],"secret":"never stored"}`)
	if res.StatusCode != 202 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	st := waitExp(t, s)
	if st.Failed || st.Kind != "steam" || !strings.Contains(st.Report, "Marked 2 expansions as owned; 3 other owned Steam items are not expansions") {
		t.Fatalf("%+v", st)
	}
	if !strings.Contains(st.Report, "Found 3 expansions for 2 games.") {
		t.Errorf("the games never read were read first: %s", st.Report)
	}
	owner := func(igdb int64) string {
		var n sql.NullString
		d.QueryRow(`SELECT o.name FROM expansion e LEFT JOIN ownership o ON o.id = e.ownership_id WHERE e.igdb_id = ?`, igdb).Scan(&n)
		return n.String
	}
	if owner(100) != "Owned" || owner(200) != "Owned" || owner(101) != "" {
		t.Errorf("%q %q %q", owner(100), owner(200), owner(101))
	}
	// run again with a game already read: nothing is fetched, nothing changes, the report still says so
	f.calls = 0
	res, _ = steamUpload(t, srv, "file", `{"rgOwnedApps":[378648]}`)
	st = waitExp(t, s)
	if res.StatusCode != 202 || f.calls != 0 || !strings.Contains(st.Report, "Marked 0 expansions as owned") {
		t.Errorf("%d calls %d %+v", res.StatusCode, f.calls, st)
	}
	// the uploaded content is nowhere in the database or the status
	var leaked int
	d.QueryRow(`SELECT count(*) FROM setting WHERE value LIKE '%never stored%'`).Scan(&leaked)
	if leaked != 0 || strings.Contains(st.Report, "never stored") {
		t.Error("the file must not be kept")
	}
}

func TestSteamImportWithoutIGDBStillMarksWhatIsKnown(t *testing.T) {
	_, srv, d := expServer(t, nil)
	g := gamesOf(t, srv)["The Witcher 3"]
	d.Exec(`INSERT INTO expansion(game_id, igdb_id, title, steam_app_id) VALUES (?, 100, 'Hearts', 378648)`, g.ID)
	res, body := steamUpload(t, srv, "file", `{"rgOwnedApps":[378648]}`)
	if res.StatusCode != 202 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var n int
	for i := 0; i < 300; i++ {
		d.QueryRow(`SELECT count(*) FROM expansion WHERE ownership_id IS NOT NULL`).Scan(&n)
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n != 1 {
		t.Error("the known expansion was not marked")
	}
}

func TestRelinkReplacesUntouchedExpansionsOnly(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp}}
	_, srv, d := expServer(t, f)
	x, gid := loadedExpansion(t, srv)
	do(t, "PATCH", srv.URL+"/api/expansions/"+itoa(x.ID), `{"status":"finished"}`)
	res, body := do(t, "PATCH", srv.URL+"/api/games/"+itoa(gid), `{"igdb_id":7777}`)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var got Game
	json.Unmarshal(body, &got)
	if len(got.Expansions) != 1 || got.Expansions[0].ID != x.ID || got.ExpansionsFetched != "" {
		t.Errorf("the tracked one stays, the other goes, the game is asked again: %+v", got)
	}
	// the same link again changes nothing
	do(t, "POST", srv.URL+"/api/games/"+itoa(gid)+"/expansions/refresh", `{}`)
	var n int
	d.QueryRow(`SELECT count(*) FROM expansion_fetch WHERE game_id = ?`, gid).Scan(&n)
	do(t, "PATCH", srv.URL+"/api/games/"+itoa(gid), `{"igdb_id":7777,"notes":"x"}`)
	var m int
	d.QueryRow(`SELECT count(*) FROM expansion_fetch WHERE game_id = ?`, gid).Scan(&m)
	if n != 1 || m != 1 {
		t.Errorf("an unchanged link keeps the read mark: %d %d", n, m)
	}
	// deleting the game takes its expansions
	do(t, "DELETE", srv.URL+"/api/games/"+itoa(gid), "")
	d.QueryRow(`SELECT count(*) FROM expansion`).Scan(&n)
	if n != 0 {
		t.Errorf("%d expansions left", n)
	}
}

// The Needs attention group exp_left lists expansions, so its ignore list holds expansion ids (and forgets deleted ones).
func TestAttentionIgnoreExpansionGroup(t *testing.T) {
	f := &fakeExpansions{lists: map[int64][]meta.Expansion{1942: witcherExp}}
	_, srv, d := expServer(t, f)
	x, _ := loadedExpansion(t, srv)
	if res, body := do(t, "POST", srv.URL+"/api/attention/ignore", `{"group":"exp_left","game_id":`+itoa(x.ID)+`,"ignore":true}`); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/attention/ignore", `{"group":"exp_nope","game_id":1,"ignore":true}`); res.StatusCode != 400 {
		t.Errorf("unknown group: %d", res.StatusCode)
	}
	_, body := do(t, "GET", srv.URL+"/api/attention/ignored", "")
	var got struct {
		Ignored map[string][]int64 `json:"ignored"`
	}
	json.Unmarshal(body, &got)
	if len(got.Ignored["exp_left"]) != 1 || got.Ignored["exp_left"][0] != x.ID {
		t.Errorf("%s", body)
	}
	d.Exec(`DELETE FROM expansion WHERE id = ?`, x.ID)
	_, body = do(t, "GET", srv.URL+"/api/attention/ignored", "")
	got.Ignored = nil
	json.Unmarshal(body, &got)
	if len(got.Ignored["exp_left"]) != 0 {
		t.Errorf("a deleted expansion is forgotten: %s", body)
	}
}
