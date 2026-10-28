// Package deals reads game prices from the IsThereAnyDeal (ITAD) API v2 and keeps them for the Wishlist, Backlog and Up
// next games. Only the one ITAD host is ever called, with the key in a header (never in a URL, so an error message cannot
// leak it).
package deals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured: there is no ITAD_API_KEY.
var ErrNotConfigured = errors.New("IsThereAnyDeal is not set up (ITAD_API_KEY is missing from .env)")

// RateLimitError is ITAD's 429: the caller should stop and try later.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("IsThereAnyDeal rate limit reached (retry in %s)", e.RetryAfter.Round(time.Second))
	}
	return "IsThereAnyDeal rate limit reached"
}

// Game is an ITAD game.
type Game struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// Money is an amount in the smallest unit (cents) of its currency.
type Money struct {
	Cents    int64
	Currency string
}

// Current is the best price a shop asks right now.
type Current struct {
	Shop    string
	Price   Money
	Regular Money
	Cut     int
	URL     string
	Expiry  string // RFC 3339, empty when ITAD gives none
}

// Low is the historical low.
type Low struct {
	Shop  string
	Price Money
	At    string // RFC 3339
}

// Overview is what ITAD knows about the price of one game. Current is nil when no shop sells it; Low is nil when
// there is no history.
type Overview struct {
	Current *Current
	Low     *Low
}

// Source is what the refresh needs from ITAD (the real Client, or a fake in tests).
type Source interface {
	Configured() bool
	// LookupAppID finds a game by its Steam app id; ok is false when ITAD does not know it.
	LookupAppID(ctx context.Context, appID int64) (g Game, ok bool, err error)
	// LookupTitle finds a game by exact title; ok is false when ITAD has no game of that name.
	LookupTitle(ctx context.Context, title string) (g Game, ok bool, err error)
	// Search lists games whose title looks like title (the owner picks one).
	Search(ctx context.Context, title string) ([]Game, error)
	// Overview reads the prices of up to MaxBatch games for a country.
	Overview(ctx context.Context, ids []string, country string) (map[string]Overview, error)
}

// MaxBatch is how many games one Overview call asks for.
const MaxBatch = 50

const (
	apiHost     = "api.isthereanydeal.com"
	maxResponse = 8 << 20
)

// Client is the real ITAD client.
type Client struct {
	key  string
	http *http.Client

	// BaseURL is overridable in tests; only the real host and loopback addresses are ever called.
	BaseURL string
	// Gap is the least time between two requests (the API allows 1000 per 5 minutes; this is far below).
	Gap time.Duration

	paceMu sync.Mutex
	next   time.Time
}

// New returns a client for the real API. An empty key disables it.
func New(key string) *Client {
	return &Client{
		key:     strings.TrimSpace(key),
		BaseURL: "https://" + apiHost,
		Gap:     400 * time.Millisecond,
		http: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *Client) Configured() bool { return c != nil && c.key != "" }

// pace waits for the next request slot.
func (c *Client) pace(ctx context.Context) error {
	c.paceMu.Lock()
	wait := time.Until(c.next)
	if wait < 0 {
		wait = 0
	}
	c.next = time.Now().Add(wait + c.Gap)
	c.paceMu.Unlock()
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

// call sends one request and decodes the JSON answer into out.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	if u.Hostname() != apiHost && !isLoopback(u.Hostname()) {
		return fmt.Errorf("ITAD host %q is not allowed", u.Host)
	}
	u = u.JoinPath(path)
	u.RawQuery = query.Encode()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	if err := c.pace(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	req.Header.Set("ITAD-API-Key", c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("IsThereAnyDeal could not be reached")
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
		return errors.New("IsThereAnyDeal refused the API key")
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("IsThereAnyDeal answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponse {
		return errors.New("IsThereAnyDeal answer is too large")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("IsThereAnyDeal answered something that is not JSON")
	}
	return nil
}

func isLoopback(host string) bool { return host == "127.0.0.1" || host == "::1" }

func (c *Client) LookupAppID(ctx context.Context, appID int64) (Game, bool, error) {
	return c.lookup(ctx, url.Values{"appid": {strconv.FormatInt(appID, 10)}})
}

func (c *Client) LookupTitle(ctx context.Context, title string) (Game, bool, error) {
	return c.lookup(ctx, url.Values{"title": {title}})
}

func (c *Client) lookup(ctx context.Context, q url.Values) (Game, bool, error) {
	var out struct {
		Found bool `json:"found"`
		Game  Game `json:"game"`
	}
	if err := c.call(ctx, http.MethodGet, "/games/lookup/v1", q, nil, &out); err != nil {
		return Game{}, false, err
	}
	if !out.Found || out.Game.ID == "" {
		return Game{}, false, nil
	}
	return out.Game, true, nil
}

func (c *Client) Search(ctx context.Context, title string) ([]Game, error) {
	var out []Game
	if err := c.call(ctx, http.MethodGet, "/games/search/v1", url.Values{"title": {title}, "results": {"12"}}, nil, &out); err != nil {
		return nil, err
	}
	keep := out[:0]
	for _, g := range out {
		if g.ID != "" && g.Title != "" {
			keep = append(keep, g)
		}
	}
	return keep, nil
}

type apiMoney struct {
	AmountInt int64  `json:"amountInt"`
	Currency  string `json:"currency"`
}

type apiShop struct {
	Name string `json:"name"`
}

func (c *Client) Overview(ctx context.Context, ids []string, country string) (map[string]Overview, error) {
	if len(ids) == 0 {
		return map[string]Overview{}, nil
	}
	if len(ids) > MaxBatch {
		return nil, fmt.Errorf("at most %d games per call", MaxBatch)
	}
	var out struct {
		Prices []struct {
			ID      string `json:"id"`
			Current *struct {
				Shop    apiShop  `json:"shop"`
				Price   apiMoney `json:"price"`
				Regular apiMoney `json:"regular"`
				Cut     int      `json:"cut"`
				URL     string   `json:"url"`
				Expiry  string   `json:"expiry"`
			} `json:"current"`
			Lowest *struct {
				Shop      apiShop  `json:"shop"`
				Price     apiMoney `json:"price"`
				Timestamp string   `json:"timestamp"`
			} `json:"lowest"`
		} `json:"prices"`
	}
	q := url.Values{"country": {country}, "vouchers": {"false"}}
	if err := c.call(ctx, http.MethodPost, "/games/overview/v2", q, ids, &out); err != nil {
		return nil, err
	}
	res := make(map[string]Overview, len(out.Prices))
	for _, p := range out.Prices {
		var o Overview
		if cur := p.Current; cur != nil && cur.Price.Currency != "" {
			o.Current = &Current{Shop: cur.Shop.Name, Price: Money{cur.Price.AmountInt, cur.Price.Currency},
				Regular: Money{cur.Regular.AmountInt, cur.Regular.Currency}, Cut: clampCut(cur.Cut), URL: webURL(cur.URL), Expiry: cur.Expiry}
		}
		if low := p.Lowest; low != nil && low.Price.Currency != "" {
			o.Low = &Low{Shop: low.Shop.Name, Price: Money{low.Price.AmountInt, low.Price.Currency}, At: low.Timestamp}
		}
		res[p.ID] = o
	}
	return res, nil
}

func clampCut(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// webURL keeps only http(s) links: the page opens them in a new tab.
func webURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return u.String()
}
