package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if *hits != 0 {
		t.Errorf("handler reached %d times", *hits)
	}
}

// The PWA shell (GET) must keep working, also with the headers a browser adds to navigations and the
// service worker's fetches.
func TestPWAGetsWork(t *testing.T) {
	h, _ := guarded("")
	for _, p := range []string{"/", "/sw.js", "/manifest.webmanifest", "/js/app.js", "/api/games", "/covers/1.jpg"} {
		for _, hdr := range []map[string]string{nil, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "cross-site"}} {
			if got := do(h, "GET", p, "deltabacklog.local", "", hdr); got != http.StatusNoContent {
				t.Errorf("GET %s %v: got %d", p, hdr, got)
			}
		}
	}
}
