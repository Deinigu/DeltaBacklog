// Package hltb looks up how long a game takes on HowLongToBeat.
//
// HowLongToBeat has no official API. This client calls the same two endpoints the site's own search box uses
// (GET /api/search/site/init for a short-lived token, then POST /api/search/site), which have changed several
// times before and will change again. Everything is therefore defensive: a failure is reported as one of two
// errors (ErrUnavailable, ErrChanged) so the drawer can say so, and nothing else in the app depends on it.
// The host is fixed; only tests override BaseURL.
package hltb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnavailable: the site could not be reached in time, or refused the request (blocked, rate limited, down).
	ErrUnavailable = errors.New("HowLongToBeat can't be reached")
	// ErrChanged: the site answered, but not in the shape this client knows: it changed its API.
	ErrChanged = errors.New("HowLongToBeat seems to have changed its site")
)

// Candidate is one game on HowLongToBeat. Times are minutes; 0 means the site has no time for that kind.
// Scores, review and "polled" counts are deliberately not kept.
type Candidate struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Year       int    `json:"year,omitempty"`
	Hastily    int    `json:"hastily"`    // Main story
	Normally   int    `json:"normally"`   // Main + extras
	Completely int    `json:"completely"` // Completionist
}

// Client is safe for concurrent use. Searches are spaced at least Gap apart.
type Client struct {
	BaseURL string // https://howlongtobeat.com; overridden by tests only
	Gap     time.Duration
	http    *http.Client

	mu    sync.Mutex
	token string
	next  time.Time
}

// New returns a client for the real site with a short timeout, so a blocked site fails fast.
func New() *Client {
	return &Client{BaseURL: "https://howlongtobeat.com", Gap: time.Second, http: &http.Client{Timeout: 8 * time.Second}}
}

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// Search returns candidates for title, best first: an exact title (and the given release year, when non-zero)
// ahead of the rest, then the site's own popularity order. Entries without any time are dropped.
func (c *Client) Search(ctx context.Context, title string, year int) ([]Candidate, error) {
	terms := strings.Fields(title)
	if len(terms) == 0 {
		return []Candidate{}, nil
	}
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.getToken(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(searchBody(terms))
		if err != nil {
			return nil, err
		}
		req, err := c.request(ctx, http.MethodPost, "/api/search/site", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-auth-token", tok)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusForbidden && attempt == 0:
			continue // the token expired: get a new one once
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusMethodNotAllowed:
			return nil, fmt.Errorf("%w (search answered %d)", ErrChanged, resp.StatusCode)
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("%w (search answered %d)", ErrUnavailable, resp.StatusCode)
		case rerr != nil:
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, rerr)
		}
		return parse(raw, title, year)
	}
}

// searchBody is the request the site's search box sends (a games search sorted by popularity).
func searchBody(terms []string) map[string]any {
	return map[string]any{
		"searchType": "games", "searchTerms": terms, "searchPage": 1, "size": 20,
		"searchOptions": map[string]any{
			"games": map[string]any{
				"userId": 0, "platform": "", "sortCategory": "popular", "rangeCategory": "main",
				"rangeTime": map[string]any{"min": 0, "max": 0},
				"gameplay":  map[string]any{"perspective": "", "flow": "", "genre": ""},
				"rangeYear": map[string]any{"min": "", "max": ""}, "modifier": "",
			},
			"users":  map[string]any{"sortCategory": "postcount"},
			"lists":  map[string]any{"sortCategory": "follows"},
			"filter": "", "sort": 0, "randomizer": 0,
		},
		"useCache": true,
	}
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", c.BaseURL+"/")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// pace waits until at least Gap has passed since the previous search started.
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	wait := max(c.next.Sub(now), 0)
	c.next = now.Add(wait + c.Gap)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrUnavailable, ctx.Err())
	}
}

// getToken returns the cached search token, fetching a new one when asked or when there is none. The request runs
// outside c.mu, so a slow token fetch never blocks (or delays the cancellation of) another search waiting in pace.
func (c *Client) getToken(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" && !fresh {
		return tok, nil
	}
	req, err := c.request(ctx, http.MethodGet, "/api/search/site/init?t="+strconv.FormatInt(time.Now().UnixMilli(), 10), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", fmt.Errorf("%w (token answered %d)", ErrChanged, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w (token answered %d)", ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.Token == "" {
		return "", fmt.Errorf("%w (no search token)", ErrChanged)
	}
	c.mu.Lock()
	c.token = out.Token
	c.mu.Unlock()
	return out.Token, nil
}

type rawGame struct {
	ID   int64  `json:"game_id"`
	Name string `json:"game_name"`
	Type string `json:"game_type"`
	Main int    `json:"comp_main"`
	Plus int    `json:"comp_plus"`
	Full int    `json:"comp_100"`
	Year int    `json:"release_world"`
}

func parse(raw []byte, title string, year int) ([]Candidate, error) {
	var out struct {
		Data *[]rawGame `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Data == nil {
		return nil, fmt.Errorf("%w (unexpected answer)", ErrChanged)
	}
	res := []Candidate{}
	for _, g := range *out.Data {
		if g.ID == 0 || g.Name == "" || (g.Type != "" && g.Type != "game") {
			continue
		}
		cand := Candidate{ID: g.ID, Name: g.Name, Year: g.Year, Hastily: minutes(g.Main), Normally: minutes(g.Plus), Completely: minutes(g.Full)}
		if cand.Hastily == 0 && cand.Normally == 0 && cand.Completely == 0 {
			continue
		}
		res = append(res, cand)
	}
	want := norm(title)
	score := func(c Candidate) int {
		s := 0
		if norm(c.Name) == want {
			s += 2
		}
		if year != 0 && c.Year == year {
			s++
		}
		return s
	}
	sort.SliceStable(res, func(i, j int) bool { return score(res[i]) > score(res[j]) })
	if len(res) > 8 {
		res = res[:8]
	}
	return res, nil
}

// minutes converts the site's seconds to whole minutes (0 stays 0, anything shorter than a minute is 1).
func minutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return max(1, (seconds+30)/60)
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
