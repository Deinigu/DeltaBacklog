// Package web embeds the single-page frontend and serves it efficiently.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed static
var files embed.FS

// asset is one file prepared at startup: its bytes, a gzip copy (when worthwhile) and a validator.
type asset struct {
	body  []byte
	gz    []byte
	etag  string
	ctype string
}

var textual = map[string]bool{".html": true, ".js": true, ".css": true, ".svg": true, ".webmanifest": true, ".json": true}

func contentType(ext string) string {
	if ext == ".webmanifest" {
		return "application/manifest+json"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// load reads every embedded file once. The whole site is a few hundred KB, so it lives in memory, already
// compressed: serving a request costs no CPU on the Raspberry Pi beyond copying bytes.
func load() map[string]*asset {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	out := map[string]*asset{}
	var modules []string
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		out[p] = &asset{body: b, ctype: contentType(path.Ext(p))}
		if strings.HasPrefix(p, "js/") && strings.HasSuffix(p, ".js") {
			modules = append(modules, p)
		}
		return nil
	})

	// Tell the browser about every module up front. Without this it discovers them one import level at a
	// time (main.js, then the modules it imports, then theirs...), each level a round trip on a slow link.
	if idx := out["index.html"]; idx != nil {
		sort.Slice(modules, func(i, j int) bool { return modules[i] == "js/main.js" || (modules[j] != "js/main.js" && modules[i] < modules[j]) })
		var links strings.Builder
		for _, m := range modules {
			links.WriteString(`<link rel="modulepreload" href="` + m + `">` + "\n")
		}
		idx.body = bytes.Replace(idx.body, []byte("</head>"), []byte(links.String()+"</head>"), 1)
	}

	for p, a := range out {
		sum := sha256.Sum256(a.body)
		a.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
		if textual[path.Ext(p)] {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(a.body)
			zw.Close()
			if buf.Len() < len(a.body) {
				a.gz = buf.Bytes()
			}
		}
	}
	return out
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			return !strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0") || strings.Contains(params, "q=0.")
		}
	}
	return false
}

// Handler serves the embedded static site. Unknown paths return 404 (routing is hash-based).
//
// Every file is sent with `Cache-Control: no-cache` and an ETag: the browser asks each time, and gets a
// body-less 304 when nothing changed, so a new deploy shows up at once yet a repeat visit costs almost nothing.
func Handler() http.Handler {
	assets := load()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		a := assets[p]
		if a == nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", a.ctype)
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", a.etag)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Add("Vary", "Accept-Encoding")
		for _, t := range strings.Split(r.Header.Get("If-None-Match"), ",") {
			if t = strings.TrimSpace(t); t == a.etag || t == "W/"+a.etag || t == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		body := a.body
		if a.gz != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			w.Write(body)
		}
	})
}
