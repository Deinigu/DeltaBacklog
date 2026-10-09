package main

import (
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
)

// allowedHosts builds the Host allow-list used against DNS rebinding: localhost, deltabacklog.local, the
// machine hostname and <hostname>.local, plus the comma separated extra names (ALLOWED_HOSTS). IP literals
// are always accepted (see hostAllowed).
func allowedHosts(extra string) map[string]bool {
	m := map[string]bool{"localhost": true, "deltabacklog.local": true}
	if h, err := os.Hostname(); err == nil && h != "" {
		h = strings.ToLower(h)
		m[h] = true
		m[h+".local"] = true
	}
	for _, h := range strings.Split(extra, ",") {
		if h = normalizeHost(h); h != "" {
			m[h] = true
		}
	}
	return m
}

// normalizeHost lowercases a host or host:port and drops the port, brackets and a trailing dot.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	return strings.TrimSuffix(h, ".")
}

func hostAllowed(allowed map[string]bool, hostHeader string) bool {
	h := normalizeHost(hostHeader)
	return h != "" && (net.ParseIP(h) != nil || allowed[h])
}

// protect wraps the root handler against requests forged by other web pages:
//   - Host allow-list (421 otherwise) against DNS rebinding; /healthz is exempt for GET and HEAD only;
//   - http.CrossOriginProtection rejects cross-origin unsafe requests (403) via Sec-Fetch-Site / Origin,
//     while same-origin browsers and non-browser clients (curl, backups) pass;
//   - POST, PUT and PATCH under /api/ must be application/json (415), so a simple cross-site form or
//     text/plain request can never be accepted as a JSON body.
func protect(next http.Handler, allowed map[string]bool) http.Handler {
	cop := http.NewCrossOriginProtection()
	checked := cop.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if strings.HasPrefix(r.URL.Path, "/api/") {
				mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mt != "application/json" {
					http.Error(w, `{"error":"Content-Type must be application/json"}`, http.StatusUnsupportedMediaType)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		health := r.URL.Path == "/healthz" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
		if !health && !hostAllowed(allowed, r.Host) {
			http.Error(w, "unknown host (see ALLOWED_HOSTS)", http.StatusMisdirectedRequest)
			return
		}
		checked.ServeHTTP(w, r)
	})
}
