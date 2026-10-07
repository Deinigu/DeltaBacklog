package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cachedResponse is one fully encoded response: the JSON, its gzip form and a validator.
type cachedResponse struct {
	body []byte
	gz   []byte
	etag string
}

// respCache keeps the two big read responses (all games, filter facets) in memory. Building them takes
// ~150ms of a Raspberry Pi's CPU (several queries plus JSON), and they only change when a game changes,
// so they are built once, compressed once, and thrown away by invalidate() after any write.
type respCache struct {
	mu      sync.Mutex
	entries map[string]*cachedResponse
	builds  int // how many times something was built (tests)

	// warm, when set, rebuilds the entries in the background warmDelay after the last invalidate(), so the next
	// reader finds them ready instead of paying for the build (a burst of writes, such as dragging a game
	// up a list, triggers one rebuild, not one per write).
	warm      func()
	warmDelay time.Duration
	timer     *time.Timer
}

// get returns the cached response for key, building it with build() when missing. The lock is held while
// building, so concurrent first requests share one build instead of each doing the work, and an
// invalidate() issued after a write waits for a running build and then clears it.
func (c *respCache) get(key string, build func() (any, error)) (*cachedResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		return e, nil
	}
	v, err := build()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	var gz bytes.Buffer
	// Default level, not Best: the best level costs ~3x the CPU on a Pi for a few percent smaller output.
	zw, _ := gzip.NewWriterLevel(&gz, gzip.DefaultCompression)
	zw.Write(body)
	zw.Close()
	sum := sha256.Sum256(body)
	e := &cachedResponse{body: body, gz: gz.Bytes(), etag: `W/"` + hex.EncodeToString(sum[:8]) + `"`}
	if c.entries == nil {
		c.entries = map[string]*cachedResponse{}
	}
	c.entries[key] = e
	c.builds++
	return e, nil
}

// invalidate drops every cached response; the next read rebuilds from the database.
func (c *respCache) invalidate() {
	c.mu.Lock()
	c.entries = nil
	if c.warm != nil {
		if c.timer != nil {
			c.timer.Stop()
		}
		c.timer = time.AfterFunc(c.warmDelay, c.warm)
	}
	c.mu.Unlock()
}

// acceptsGzip reports whether the client listed gzip in Accept-Encoding.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			return !strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0") || strings.Contains(params, "q=0.")
		}
	}
	return false
}

// etagMatches reports whether an If-None-Match header lists the given (weak) validator.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// serveCached answers a GET from the cache: 304 when the client already has this version, gzip when it
// accepts it. no-cache + ETag makes the browser ask every time, which costs a few bytes when nothing changed.
func (s *Server) serveCached(w http.ResponseWriter, r *http.Request, key string, build func() (any, error)) {
	e, err := s.cache.get(key, build)
	if err != nil {
		fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", e.etag)
	h.Add("Vary", "Accept-Encoding")
	if etagMatches(r.Header.Get("If-None-Match"), e.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := e.body
	if acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		body = e.gz
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

// invalidating wraps the router: any request that can change games (a write under /api/games) clears the
// cache once it has finished, i.e. after its transaction committed.
func (s *Server) invalidating(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/games") {
			s.cache.invalidate()
		}
	})
}
