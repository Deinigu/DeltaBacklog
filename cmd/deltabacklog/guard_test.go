package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/api"
	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// guarded returns the protected handler around a stub that records whether it was reached.
func guarded(extra string) (http.Handler, *int) {
	hits := 0
	root := http.NewServeMux()
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			hits++
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return protect(root, allowedHosts(extra)), &hits
}

func do(h http.Handler, method, path, host, ctype string, hdr map[string]string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"title":"x"}`))
	req.Host = host
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestCrossOriginWriteRejected(t *testing.T) {
	h, hits := guarded("")
	cases := []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Origin": "http://evil.example"},
	}
	for _, hdr := range cases {
		if got := do(h, "POST", "/api/games", "127.0.0.1:8081", "text/plain", hdr); got != http.StatusForbidden {
			t.Errorf("%v: got %d, want 403", hdr, got)
		}
		if got := do(h, "DELETE", "/api/games/1", "127.0.0.1:8081", "", hdr); got != http.StatusForbidden {
			t.Errorf("DELETE %v: got %d, want 403", hdr, got)
		}
	}
	if *hits != 0 {
		t.Errorf("handler reached %d times by rejected requests", *hits)
	}
}

func TestSameOriginAndCLIWork(t *testing.T) {
	h, hits := guarded("")
	ok := []map[string]string{
		nil, // curl
		{"Sec-Fetch-Site": "same-origin"},
		{"Sec-Fetch-Site": "none"},
		{"Origin": "http://127.0.0.1:8081"},
	}
	for _, hdr := range ok {
		if got := do(h, "POST", "/api/games", "127.0.0.1:8081", "application/json", hdr); got != http.StatusNoContent {
			t.Errorf("%v: got %d, want 204", hdr, got)
		}
	}
	if got := do(h, "PATCH", "/api/games/1", "127.0.0.1:8081", "application/json; charset=utf-8", nil); got != http.StatusNoContent {
		t.Errorf("json with charset: got %d", got)
	}
	if *hits != len(ok)+1 {
		t.Errorf("hits = %d", *hits)
	}
}

func TestJSONContentTypeRequired(t *testing.T) {
	h, hits := guarded("")
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "application/jsonx"} {
		for _, m := range []string{"POST", "PUT", "PATCH"} {
			if got := do(h, m, "/api/games/reorder", "127.0.0.1:8081", ct, nil); got != http.StatusUnsupportedMediaType {
				t.Errorf("%s %q: got %d, want 415", m, ct, got)
			}
		}
	}
	if *hits != 0 {
		t.Errorf("handler reached %d times", *hits)
	}
	// DELETE has no body and GET is safe: neither needs a content type.
	if got := do(h, "DELETE", "/api/games/1", "127.0.0.1:8081", "", nil); got != http.StatusNoContent {
		t.Errorf("DELETE: got %d", got)
	}
}

func TestHostAllowList(t *testing.T) {
	h, _ := guarded("games.example.org, Other.Example:8081")
	good := []string{
		"127.0.0.1:18090", "127.0.0.1", "192.168.1.20:8081", "[::1]:8081", "[::1]", "[fe80::1]:8081",
		"localhost", "localhost:8081", "deltabacklog.local", "deltabacklog.local:8081", "DeltaBacklog.LOCAL",
		"games.example.org", "games.example.org:8081", "other.example",
	}
	for _, host := range good {
		if got := do(h, "GET", "/api/games", host, "", nil); got != http.StatusNoContent {
			t.Errorf("Host %q: got %d, want 204", host, got)
		}
	}
	for _, host := range []string{"evil.example", "evil.example:8081", "127.0.0.1.evil.example", "deltabacklog.local.evil.example", ""} {
		if got := do(h, "GET", "/api/games", host, "", nil); got != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: got %d, want 421", host, got)
		}
		if got := do(h, "GET", "/", host, "", nil); got != http.StatusMisdirectedRequest {
			t.Errorf("Host %q on /: got %d, want 421", host, got)
		}
	}
}

func TestOwnHostnameAllowed(t *testing.T) {
	m := allowedHosts("")
	if len(m) < 3 {
		t.Skip("no machine hostname")
	}
	for h := range m {
		if got := do(protect(http.NotFoundHandler(), m), "GET", "/x", h, "", nil); got != http.StatusNotFound {
			t.Errorf("%q: got %d", h, got)
		}
	}
}

func TestHealthzAnyHostReadOnly(t *testing.T) {
	h, hits := guarded("")
	for _, m := range []string{"GET", "HEAD"} {
		if got := do(h, m, "/healthz", "evil.example", "", nil); got != http.StatusNoContent {
			t.Errorf("%s /healthz: got %d", m, got)
		}
	}
	if got := do(h, "POST", "/healthz", "evil.example", "application/json", nil); got != http.StatusMisdirectedRequest {
		t.Errorf("POST /healthz with a foreign host: got %d, want 421", got)
	}
	for _, c := range [][2]string{{"GET", "/healthz/"}, {"GET", "//healthz"}, {"OPTIONS", "/healthz"}, {"GET", "/healthz/../api/games"}, {"GET", "/healthz2"}} {
		if got := do(h, c[0], c[1], "evil.example", "", nil); got != http.StatusMisdirectedRequest {
			t.Errorf("%s %s with a foreign host: got %d, want 421", c[0], c[1], got)
		}
	}
	if *hits != 0 {
		t.Errorf("handler reached %d times", *hits)
	}
}

// The PWA shell (GET) must keep working, also with the headers a browser adds to navigations and the
// service worker's fetches.
func TestPWAGetsWork(t *testing.T) {
	h, _ := guarded("")
	for _, p := range []string{"/", "/sw.js", "/manifest.webmanifest", "/js/app.js", "/covers/1.jpg"} {
		for _, hdr := range []map[string]string{nil, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "cross-site"}} {
			if got := do(h, "GET", p, "deltabacklog.local", "", hdr); got != http.StatusNoContent {
				t.Errorf("GET %s %v: got %d", p, hdr, got)
			}
		}
	}
}

func TestZoneIDHostAllowed(t *testing.T) {
	h, _ := guarded("")
	if got := do(h, "GET", "/", "[fe80::1%25eth0]:8081", "", nil); got == http.StatusMisdirectedRequest {
		t.Errorf("zone id host rejected")
	}
}

// Other sites must not trigger API reads (lookups spend the metadata quota); the app and typed addresses can.
func TestCrossSiteAPIReadRejected(t *testing.T) {
	h, _ := guarded("")
	for _, site := range []string{"cross-site", "same-site"} {
		if got := do(h, "GET", "/api/lookup?q=x", "127.0.0.1:8081", "", map[string]string{"Sec-Fetch-Site": site}); got != http.StatusForbidden {
			t.Errorf("GET /api %s: got %d, want 403", site, got)
		}
	}
	for _, site := range []string{"same-origin", "none", ""} {
		if got := do(h, "GET", "/api/lookup?q=x", "127.0.0.1:8081", "", map[string]string{"Sec-Fetch-Site": site}); got != http.StatusNoContent {
			t.Errorf("GET /api %q: got %d, want 204", site, got)
		}
	}
}

func TestErrorBodiesAreJSON(t *testing.T) {
	h, _ := guarded("")
	check := func(name string, req *http.Request, code int) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != code || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("%s: code %d type %q body %q", name, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/api/games", strings.NewReader("{}"))
	r.Host = "127.0.0.1:8081"
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	check("403", r, 403)
	r = httptest.NewRequest("POST", "/api/games", strings.NewReader("{}"))
	r.Host = "127.0.0.1:8081"
	check("415", r, 415)
	r = httptest.NewRequest("GET", "/api/games", nil)
	r.Host = "evil.example"
	check("421", r, 421)
}

// The real handler chain as main builds it, over a temp database: a cross-site text/plain POST is rejected and
// leaves no data; a same-origin JSON POST works.
func TestRealHandlerChain(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv := httptest.NewServer(newHandler(&api.Server{DB: d, CoverDir: t.TempDir()}, allowedHosts("")))
	defer srv.Close()
	count := func() int {
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM game`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	post := func(ctype string, hdr map[string]string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/games", strings.NewReader(`{"title":"Evil Game","status":"backlog"}`))
		req.Header.Set("Content-Type", ctype)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := post("text/plain", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"}); got != 403 {
		t.Errorf("cross-site text/plain: got %d, want 403", got)
	}
	if got := post("application/json", map[string]string{"Origin": "http://evil.example"}); got != 403 {
		t.Errorf("cross-origin json: got %d, want 403", got)
	}
	if n := count(); n != 0 {
		t.Fatalf("%d games after rejected requests", n)
	}
	if got := post("application/json", map[string]string{"Sec-Fetch-Site": "same-origin"}); got != 201 {
		t.Errorf("same-origin json: got %d, want 201", got)
	}
	if n := count(); n != 1 {
		t.Errorf("%d games after the same-origin POST, want 1", n)
	}
	res, err := http.Get(srv.URL + "/")
	if err != nil || res.StatusCode != 200 {
		t.Errorf("GET /: %v %v", res, err)
	}
}

// The screenshot upload is the one API route that takes multipart/form-data; it still passes the cross-origin check,
// and no other route accepts that Content-Type.
func TestMultipartOnlyForScreenshotUpload(t *testing.T) {
	h, hits := guarded("")
	ct := "multipart/form-data; boundary=x"
	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	if got := do(h, "POST", "/api/games/12/screenshots", "127.0.0.1:8081", ct, same); got != 204 {
		t.Errorf("upload: %d", got)
	}
	if got := do(h, "POST", "/api/games/12/screenshots", "127.0.0.1:8081", ct, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"}); got != 403 {
		t.Errorf("cross-site upload: %d", got)
	}
	for _, p := range []string{"/api/games", "/api/games/12", "/api/games/12/cover", "/api/games/12/screenshots/order", "/api/games/1/2/screenshots", "/api/views"} {
		if got := do(h, "POST", p, "127.0.0.1:8081", ct, same); got != 415 {
			t.Errorf("multipart to %s: %d, want 415", p, got)
		}
	}
	if got := do(h, "PATCH", "/api/games/12/screenshots", "127.0.0.1:8081", ct, same); got != 415 {
		t.Errorf("PATCH multipart: %d", got)
	}
	if *hits != 1 {
		t.Errorf("%d requests reached the handler, want 1", *hits)
	}
}
