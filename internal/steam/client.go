// Package steam reads the games an account owns, with the hours played, from the Steam Web API, and keeps them for the
// review list and the hours shown in the library. Only api.steampowered.com is ever called. The API takes the key as a
// query parameter, so no error from here ever carries a URL: every message is fixed text.
package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured: STEAM_API_KEY or STEAM_ID is missing.
var ErrNotConfigured = errors.New("Steam is not set up (STEAM_API_KEY and STEAM_ID are missing from .env)")

// ErrPrivate: the account's game details are not public, so Steam answers with nothing.
var ErrPrivate = errors.New("Steam returned no game list: set “Game details” to Public in the Steam profile's privacy settings")

// RateLimitError is Steam's 429: the caller should stop and try later.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("Steam rate limit reached (retry in %s)", e.RetryAfter.Round(time.Second))
	}
	return "Steam rate limit reached"
}

// App is one owned game. Playtime is minutes; LastPlayed is unix seconds, 0 when never played or unknown.
type App struct {
	ID         int64
	Name       string
	Playtime   int64
	LastPlayed int64
}

// Source is what a sync needs from Steam (the real Client, or a fake in tests).
type Source interface {
	Configured() bool
	// Owned lists every game of the account. It returns an error, never a partial list.
	Owned(ctx context.Context) ([]App, error)
}

const (
	apiHost     = "api.steampowered.com"
	maxResponse = 16 << 20
)

var steamID64 = regexp.MustCompile(`^7656\d{13}$`)

// Client is the real Steam Web API client.
type Client struct {
	key     string
	account string // STEAM_ID: a SteamID64 or a vanity name
	http    *http.Client

	// BaseURL is overridable in tests; only the real host and loopback addresses are ever called.
	BaseURL string
	// Gap is the least time between two requests.
	Gap time.Duration

	mu       sync.Mutex
	resolved string // the SteamID64 of a vanity name, once resolved
	next     time.Time
}

// New returns a client for the real API. An empty key or id disables it.
func New(key, account string) *Client {
	return &Client{
		key:     strings.TrimSpace(key),
		account: cleanAccount(account),
		BaseURL: "https://" + apiHost,
		Gap:     time.Second,
		http: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// cleanAccount accepts a SteamID64, a vanity name, or a profile address (steamcommunity.com/id/<name> or /profiles/<id>).
func cleanAccount(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "steamcommunity.com/"); i >= 0 {
		parts := strings.Split(strings.Trim(s[i+len("steamcommunity.com/"):], "/"), "/")
		if len(parts) >= 2 && (parts[0] == "id" || parts[0] == "profiles") {
			return parts[1]
		}
	}
	return s
}

func (c *Client) Configured() bool { return c != nil && c.key != "" && c.account != "" }

// pace waits for the next request slot.
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	wait := time.Until(c.next)
	if wait < 0 {
		wait = 0
	}
	c.next = time.Now().Add(wait + c.Gap)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// call sends one GET and decodes the JSON answer into out. The key is added here and never appears in an error.
func (c *Client) call(ctx context.Context, path string, query url.Values, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Hostname() != apiHost && !isLoopback(u.Hostname())) {
		return errors.New("Steam host is not allowed")
	}
	u = u.JoinPath(path)
	q := url.Values{"key": {c.key}, "format": {"json"}}
	for k, v := range query {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	if err := c.pace(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("Steam request could not be built")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req) // err is a *url.Error holding the full URL, key included: it is never returned
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Steam could not be reached")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		e := &RateLimitError{}
		if n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && n > 0 {
			e.RetryAfter = time.Duration(n) * time.Second
		}
		return e
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return errors.New("Steam refused the API key (check STEAM_API_KEY in .env)")
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("Steam answered %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return errors.New("Steam answer could not be read")
	}
	if len(data) > maxResponse {
		return errors.New("Steam answer is too large")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("Steam answered something that is not JSON")
	}
	return nil
}

func isLoopback(host string) bool { return host == "127.0.0.1" || host == "::1" }

// SteamID returns the account's SteamID64, resolving a vanity name the first time.
func (c *Client) SteamID(ctx context.Context) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	if steamID64.MatchString(c.account) {
		return c.account, nil
	}
	c.mu.Lock()
	id := c.resolved
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	var out struct {
		Response struct {
			Success int    `json:"success"`
			SteamID string `json:"steamid"`
		} `json:"response"`
	}
	if err := c.call(ctx, "/ISteamUser/ResolveVanityURL/v1/", url.Values{"vanityurl": {c.account}}, &out); err != nil {
		return "", err
	}
	if out.Response.Success != 1 || !steamID64.MatchString(out.Response.SteamID) {
		return "", errors.New("STEAM_ID is neither a SteamID64 nor a profile name Steam knows")
	}
	c.mu.Lock()
	c.resolved = out.Response.SteamID
	c.mu.Unlock()
	return out.Response.SteamID, nil
}

// Owned lists the games of the account with their hours. A private profile answers an empty object: that is ErrPrivate.
func (c *Client) Owned(ctx context.Context) ([]App, error) {
	id, err := c.SteamID(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Response struct {
			Count *int `json:"game_count"`
			Games []struct {
				AppID      int64  `json:"appid"`
				Name       string `json:"name"`
				Playtime   int64  `json:"playtime_forever"`
				LastPlayed int64  `json:"rtime_last_played"`
			} `json:"games"`
		} `json:"response"`
	}
	q := url.Values{"steamid": {id}, "include_appinfo": {"1"}, "include_played_free_games": {"1"}}
	if err := c.call(ctx, "/IPlayerService/GetOwnedGames/v1/", q, &out); err != nil {
		return nil, err
	}
	r := out.Response
	if r.Count == nil {
		return nil, ErrPrivate
	}
	if *r.Count != len(r.Games) {
		return nil, errors.New("Steam sent an incomplete game list; nothing was changed")
	}
	apps := make([]App, 0, len(r.Games))
	for _, g := range r.Games {
		name := strings.TrimSpace(g.Name)
		if g.AppID <= 0 || name == "" {
			continue
		}
		apps = append(apps, App{ID: g.AppID, Name: name, Playtime: max(g.Playtime, 0), LastPlayed: max(g.LastPlayed, 0)})
	}
	return apps, nil
}
