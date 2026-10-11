package main

import (
	"encoding/json"
	"mime"
	"net"
	"net/http"
	"net/netip"
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

// isIP reports whether h is an IPv4 or IPv6 literal (zone IDs allowed).
func isIP(h string) bool {
	_, err := netip.ParseAddr(h)
	return err == nil
}

// isShotUpload is the one route that takes a multipart body: POST /api/games/<id>/screenshots.
func isShotUpload(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodPost && strings.HasPrefix(p, "/api/games/") && strings.HasSuffix(p, "/screenshots") &&
		strings.Count(p, "/") == 4
}

// jsonErr answers in the API's error shape, {"error": "..."}.
func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func hostAllowed(allowed map[string]bool, hostHeader string) bool {
	h := normalizeHost(hostHeader)
	return h != "" && (isIP(h) || allowed[h])
}

// protect wraps the root handler against requests forged by other web pages:
//   - Host allow-list (421 otherwise) against DNS rebinding; /healthz is exempt for GET and HEAD only;
//   - http.CrossOriginProtection rejects cross-origin unsafe requests (403) via Sec-Fetch-Site / Origin,
//     while same-origin browsers and non-browser clients (curl, backups) pass;
//   - GET under /api/ with Sec-Fetch-Site cross-site or same-site is rejected (403): other sites cannot trigger lookups;
//   - POST, PUT and PATCH under /api/ must be application/json (415; the screenshot upload may be multipart/form-data), so a simple cross-site form or
//     text/plain request can never be accepted as a JSON body.
func protect(next http.Handler, allowed map[string]bool) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonErr(w, http.StatusForbidden, "cross-origin request rejected")
	}))
	checked := cop.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Other sites must not even trigger reads (lookups spend the IGDB / SteamGridDB quota). The app's own
		// fetches are same-origin; a typed address or bookmark sends "none".
		if sfs := r.Header.Get("Sec-Fetch-Site"); (sfs == "cross-site" || sfs == "same-site") && strings.HasPrefix(r.URL.Path, "/api/") {
			jsonErr(w, http.StatusForbidden, "cross-origin request rejected")
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if strings.HasPrefix(r.URL.Path, "/api/") {
				mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err == nil && mt == "multipart/form-data" && isShotUpload(r) {
					// the screenshot upload; the cross-origin check above already refused other sites
				} else if err != nil || mt != "application/json" {
					jsonErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		health := r.URL.Path == "/healthz" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
		if !health && !hostAllowed(allowed, r.Host) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonErr(w, http.StatusMisdirectedRequest, "unknown host (see ALLOWED_HOSTS)")
				return
			}
			http.Error(w, "unknown host (see ALLOWED_HOSTS)", http.StatusMisdirectedRequest)
			return
		}
		checked.ServeHTTP(w, r)
	})
}
