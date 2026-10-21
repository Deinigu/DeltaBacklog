package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/hltb"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

type fakeHLTB struct {
	res        []hltb.Candidate
	err        error
	title      string
	year, hits int
}

func (f *fakeHLTB) Search(_ context.Context, title string, year int) ([]hltb.Candidate, error) {
	f.title, f.year = title, year
	f.hits++
	return f.res, f.err
}

func ttbServer(t *testing.T, h HLTB) (*httptest.Server, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir(), HLTB: h}).Handler())
	t.Cleanup(srv.Close)
	return srv, d
}

type times struct {
	H, N, C *int
	Src     *string
}

func readTimes(t *testing.T, d *sql.DB, title string) times {
	t.Helper()
	var x times
	if err := d.QueryRow(`SELECT time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely, time_to_beat_src FROM game WHERE title = ?`, title).
		Scan(&x.H, &x.N, &x.C, &x.Src); err != nil {
		t.Fatal(err)
	}
	return x
}

func gameID(t *testing.T, d *sql.DB, title string) string {
	t.Helper()
	var id int64
	if err := d.QueryRow(`SELECT id FROM game WHERE title = ?`, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return itoa64(id)
}

func eq(p *int, want int) bool { return p != nil && *p == want }

func TestPatchTimesToBeat(t *testing.T) {
	srv, d := ttbServer(t, nil)
	url := srv.URL + "/api/games/" + gameID(t, d, "Hades II")

	// typed by the owner: marked manual, shown in the response
	res, body := do(t, "PATCH", url, `{"ttb_hastily":480,"ttb_normally":720,"ttb_completely":null}`)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var g map[string]any
	json.Unmarshal(body, &g)
	if g["ttb_hastily"] != float64(480) || g["ttb_completely"] != nil || g["ttb_source"] != "manual" {
		t.Errorf("response: %v", g)
	}
	// one time changes, the others stay; the source can be HowLongToBeat
	do(t, "PATCH", url, `{"ttb_completely":1800,"ttb_source":"hltb"}`)
	x := readTimes(t, d, "Hades II")
	if !eq(x.H, 480) || !eq(x.N, 720) || !eq(x.C, 1800) || x.Src == nil || *x.Src != "hltb" {
		t.Errorf("after hltb: %+v", x)
	}
	// 0 clears one; clearing every time clears the source too
	do(t, "PATCH", url, `{"ttb_normally":0}`)
	if x = readTimes(t, d, "Hades II"); x.N != nil || !eq(x.H, 480) {
		t.Errorf("0 clears one: %+v", x)
	}
	do(t, "PATCH", url, `{"ttb_hastily":null,"ttb_completely":null}`)
	if x = readTimes(t, d, "Hades II"); x.H != nil || x.N != nil || x.C != nil || x.Src != nil {
		t.Errorf("all cleared: %+v", x)
	}
	// a patch without times leaves them alone
	do(t, "PATCH", url, `{"ttb_normally":90}`)
	do(t, "PATCH", url, `{"notes":"hi"}`)
	if x = readTimes(t, d, "Hades II"); !eq(x.N, 90) || x.Src == nil || *x.Src != "manual" {
		t.Errorf("untouched: %+v", x)
	}
}

func TestPatchTimesValidation(t *testing.T) {
	srv, d := ttbServer(t, nil)
	url := srv.URL + "/api/games/" + gameID(t, d, "Hades II")
	for _, body := range []string{
		`{"ttb_normally":-5}`, `{"ttb_normally":100001}`, `{"ttb_normally":1.5}`, `{"ttb_normally":"120"}`, `{"ttb_hastily":true}`,
		`{"ttb_normally":60,"ttb_source":"igdb"}`, `{"ttb_normally":60,"ttb_source":"nope"}`,
	} {
		if res, _ := do(t, "PATCH", url, body); res.StatusCode != 400 {
			t.Errorf("%s -> %d, want 400", body, res.StatusCode)
		}
	}
	if x := readTimes(t, d, "Hades II"); x.N != nil || x.Src != nil {
		t.Errorf("a rejected patch changed the game: %+v", x)
	}
	if res, _ := do(t, "PATCH", url, `{"ttb_normally":100000}`); res.StatusCode != 200 {
		t.Errorf("the largest time was rejected: %d", res.StatusCode)
	}
}

func TestCreateGameWithTimes(t *testing.T) {
	srv, d := ttbServer(t, nil)
	res, body := do(t, "POST", srv.URL+"/api/games", `{"title":"Timed","ttb_normally":300}`)
	if res.StatusCode != 200 && res.StatusCode != 201 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if x := readTimes(t, d, "Timed"); !eq(x.N, 300) || x.Src == nil || *x.Src != "manual" {
		t.Errorf("%+v", x)
	}
	if res, _ := do(t, "POST", srv.URL+"/api/games", `{"title":"Bad","ttb_normally":-1}`); res.StatusCode != 400 {
		t.Errorf("negative time on create -> %d", res.StatusCode)
	}
}

// Only IGDB's own times go when the IGDB link changes; the owner's and HowLongToBeat's stay.
func TestChangingTheIGDBLinkKeepsOwnTimes(t *testing.T) {
	srv, d := ttbServer(t, nil)
	for _, c := range []struct{ src, want string }{{"igdb", ""}, {"hltb", "hltb"}, {"manual", "manual"}} {
		d.Exec(`UPDATE game SET igdb_id = 5, time_to_beat_normally = 120, time_to_beat_src = ? WHERE title = 'Hades II'`, c.src)
		do(t, "PATCH", srv.URL+"/api/games/"+gameID(t, d, "Hades II"), `{"igdb_id":6}`)
		x := readTimes(t, d, "Hades II")
		if c.want == "" && (x.N != nil || x.Src != nil) {
			t.Errorf("igdb times survived the relink: %+v", x)
		}
		if c.want != "" && (!eq(x.N, 120) || x.Src == nil || *x.Src != c.want) {
			t.Errorf("%s times were cleared by the relink: %+v", c.src, x)
		}
	}
}

func TestHLTBSearchEndpoint(t *testing.T) {
	f := &fakeHLTB{res: []hltb.Candidate{{ID: 7, Name: "Portal", Year: 2007, Hastily: 187, Normally: 316, Completely: 632}}}
	srv, _ := ttbServer(t, f)
	res, body := do(t, "GET", srv.URL+"/api/hltb/search?q=Portal&year=2007", "")
	if res.StatusCode != 200 || f.title != "Portal" || f.year != 2007 {
		t.Fatalf("%d %s (asked %q %d)", res.StatusCode, body, f.title, f.year)
	}
	var out struct {
		Results []hltb.Candidate `json:"results"`
	}
	json.Unmarshal(body, &out)
	if len(out.Results) != 1 || out.Results[0].Normally != 316 {
		t.Errorf("results: %s", body)
	}
	if strings.Contains(string(body), "score") || strings.Contains(string(body), "count") {
		t.Errorf("scores or counts leaked: %s", body)
	}
	if res, _ := do(t, "GET", srv.URL+"/api/hltb/search?q=P", ""); res.StatusCode != 400 || f.hits != 1 {
		t.Errorf("a one-letter query must not reach the site: %d, %d hits", res.StatusCode, f.hits)
	}
	f.res = nil
	if _, body = do(t, "GET", srv.URL+"/api/hltb/search?q=Nothing", ""); !strings.Contains(string(body), `"results":[]`) {
		t.Errorf("no match must be an empty list: %s", body)
	}
}

func TestHLTBSearchFailureIsAMessageNotACrash(t *testing.T) {
	f := &fakeHLTB{err: errors.Join(hltb.ErrUnavailable, errors.New("dial tcp 1.2.3.4:443: i/o timeout"))}
	srv, _ := ttbServer(t, f)
	res, body := do(t, "GET", srv.URL+"/api/hltb/search?q=Portal", "")
	if res.StatusCode != 502 || !strings.Contains(string(body), "can't be reached") || strings.Contains(string(body), "1.2.3.4") {
		t.Errorf("%d %s", res.StatusCode, body)
	}
	f.err = hltb.ErrChanged
	if res, body = do(t, "GET", srv.URL+"/api/hltb/search?q=Portal", ""); res.StatusCode != 502 || !strings.Contains(string(body), "changed its site") {
		t.Errorf("%d %s", res.StatusCode, body)
	}
}
