package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func get(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestIndexPreloadsEveryModule(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	_, body := get(t, srv, "GET", "/", nil)
	html := string(body)

	sub, _ := fs.Sub(files, "static")
	entries, _ := fs.ReadDir(sub, "js")
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".js") {
			n++
			if !strings.Contains(html, `<link rel="modulepreload" href="js/`+e.Name()+`">`) {
				t.Errorf("js/%s is not preloaded", e.Name())
			}
		}
	}
	if n < 20 {
		t.Errorf("only %d modules found: the embedded site looks incomplete", n)
	}
	if strings.Index(html, `modulepreload" href="js/main.js"`) > strings.Index(html, `modulepreload" href="js/add.js"`) {
		t.Error("main.js should be preloaded first")
	}
	if !strings.Contains(html, "</head>") || strings.Count(html, "</head>") != 1 {
		t.Error("the document head was damaged by the injection")
	}
}

func TestStaticFilesAreCompressedAndValidated(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	for _, p := range []string{"/", "/index.html", "/js/library.js", "/styles.css", "/sw.js", "/manifest.webmanifest", "/icon.svg"} {
		res, plain := get(t, srv, "GET", p, nil)
		if res.StatusCode != 200 || res.Header.Get("ETag") == "" || res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("Content-Encoding") != "" {
			t.Fatalf("%s plain: %d %v", p, res.StatusCode, res.Header)
		}
		res2, zipped := get(t, srv, "GET", p, map[string]string{"Accept-Encoding": "gzip"})
		if res2.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: not gzipped", p)
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(zipped))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		un, _ := io.ReadAll(zr)
		if !bytes.Equal(un, plain) || len(zipped) >= len(plain) {
			t.Errorf("%s: gzip body wrong or not smaller (%d vs %d)", p, len(zipped), len(plain))
		}
		if res2.Header.Get("ETag") != res.Header.Get("ETag") {
			t.Errorf("%s: validator differs between encodings", p)
		}
		// repeat visit: the browser sends the validator and gets no body
		res3, body := get(t, srv, "GET", p, map[string]string{"If-None-Match": res.Header.Get("ETag")})
		if res3.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Errorf("%s: conditional request gave %d with %d bytes", p, res3.StatusCode, len(body))
		}
	}
}

func TestContentTypesAndRouting(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	for p, want := range map[string]string{
		"/js/main.js": "text/javascript", "/styles.css": "text/css", "/": "text/html",
		"/manifest.webmanifest": "application/manifest+json", "/icon.svg": "image/svg+xml",
	} {
		res, _ := get(t, srv, "GET", p, nil)
		if !strings.HasPrefix(res.Header.Get("Content-Type"), want) {
			t.Errorf("%s content type %q, want %s", p, res.Header.Get("Content-Type"), want)
		}
		if res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", p)
		}
	}
	for _, p := range []string{"/nope", "/js/", "/js/../../go.mod", "/%2e%2e/go.mod", "/static/index.html"} {
		if res, _ := get(t, srv, "GET", p, nil); res.StatusCode == 200 {
			t.Errorf("%s must not be served (got 200)", p)
		}
	}
	res, body := get(t, srv, "HEAD", "/js/main.js", nil)
	if res.StatusCode != 200 || len(body) != 0 || res.Header.Get("Content-Length") == "" {
		t.Errorf("HEAD: %d, %d body bytes, length %q", res.StatusCode, len(body), res.Header.Get("Content-Length"))
	}
	if res, _ := get(t, srv, "POST", "/", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", res.StatusCode)
	}
}

// The service worker precaches the shell for offline use: a module missing from SHELL_FILES breaks the app offline.
func TestServiceWorkerCachesTheWholeShell(t *testing.T) {
	sw, err := fs.ReadFile(files, "static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(sw)
	start := strings.Index(src, "const SHELL_FILES = [")
	if start < 0 {
		t.Fatal("SHELL_FILES not found in sw.js")
	}
	list := src[start:]
	list = list[:strings.Index(list, "];")]
	if strings.Contains(list, "'/") {
		t.Error("sw.js SHELL_FILES must be relative (the app may be served under a sub-path)")
	}
	want := []string{"'./'", "'styles.css'", "'icon.svg'"}
	sub, _ := fs.Sub(files, "static")
	entries, _ := fs.ReadDir(sub, "js")
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".js") {
			want = append(want, "'js/"+e.Name()+"'")
		}
	}
	for _, w := range want {
		if !strings.Contains(list, w) {
			t.Errorf("sw.js SHELL_FILES is missing %s", w)
		}
	}
}

// A reverse proxy may serve the app under a sub-path (/backlog/): every URL the frontend builds must be relative to the
// page, so a root-relative "/api/...", "/covers/..." or "/sw.js" would escape the prefix.
func TestFrontendURLsAreRelative(t *testing.T) {
	sub, _ := fs.Sub(files, "static")
	abs := regexp.MustCompile(`["'\x60]/(api/|covers/|screenshots/|js/|sw\.js|styles\.css|icon\.svg|manifest\.webmanifest)`)
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := fs.ReadFile(sub, p)
		for i, line := range strings.Split(string(b), "\n") {
			if m := abs.FindString(line); m != "" {
				t.Errorf("%s:%d: root-relative URL %s", p, i+1, m)
			}
		}
		return nil
	})
}
