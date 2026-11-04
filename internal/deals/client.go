// Package deals reads game prices from the IsThereAnyDeal (ITAD) API v2 and keeps them for the Backlog and Up
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
	"sort"
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
	Alt     *Current // a clearly cheaper offer at a shop the owner ranks lower (see Choose); nil without a ranking
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

// BulkSource is the optional batch lookup: many Steam app ids in one request. The real Client has it; the wishlist uses it
// so that matching 130 games costs one or two requests, not 130 (which used up the key's rate limit before any price was read).
type BulkSource interface {
	// LookupAppIDs maps Steam app ids to ITAD game ids; an id ITAD does not know is absent from the result. At most MaxLookupBatch ids.
	LookupAppIDs(ctx context.Context, appIDs []int64) (map[int64]string, error)
}

// MaxLookupBatch is how many app ids one bulk lookup asks for (the API takes up to 200).
const MaxLookupBatch = 200

// steamShopID is IsThereAnyDeal's id of the Steam shop.
const steamShopID = 61

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

	shopsMu  sync.Mutex
	shops    []string
	shopsFor string
	shopsAt  time.Time

	// sleep is time.After; tests replace it so a Retry-After does not make them wait.
	sleep func(time.Duration) <-chan time.Time
}

// maxRetryWait is the longest Retry-After the client waits out (once) before trying a request again; a longer one stops
// the run, which carries on at the next refresh.
const maxRetryWait = 15 * time.Second

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

// call sends one request and decodes the JSON answer into out. A 429 whose Retry-After is short (maxRetryWait or less) is
// waited out once, respecting ctx, and the request is sent again; otherwise the RateLimitError is returned.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	err := c.callOnce(ctx, method, path, query, body, out)
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > maxRetryWait {
		return err
	}
	sleep := c.sleep
	if sleep == nil {
		sleep = time.After
	}
	select {
	case <-sleep(rl.RetryAfter):
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.callOnce(ctx, method, path, query, body, out)
}

// retryAfter reads a Retry-After header: seconds, or an HTTP date. 0 when absent, invalid or in the past.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if n, err := strconv.Atoi(v); err == nil {
		if n > 0 {
			return time.Duration(n) * time.Second
		}
		return 0
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return (d + time.Second - 1).Truncate(time.Second) // rounded up to whole seconds
		}
	}
	return 0
}

func (c *Client) callOnce(ctx context.Context, method, path string, query url.Values, body any, out any) error {
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
		return &RateLimitError{RetryAfter: retryAfter(res.Header.Get("Retry-After"), time.Now())}
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

// LookupAppIDs is POST /lookup/id/shop/61/v1: the body is ["app/<id>", ...], the answer maps each to an ITAD game id or null.
func (c *Client) LookupAppIDs(ctx context.Context, appIDs []int64) (map[int64]string, error) {
	if len(appIDs) == 0 {
		return map[int64]string{}, nil
	}
	if len(appIDs) > MaxLookupBatch {
		return nil, fmt.Errorf("at most %d apps per call", MaxLookupBatch)
	}
	body := make([]string, len(appIDs))
	for i, id := range appIDs {
		body[i] = "app/" + strconv.FormatInt(id, 10)
	}
	var out map[string]*string
	if err := c.call(ctx, http.MethodPost, "/lookup/id/shop/"+strconv.Itoa(steamShopID)+"/v1", nil, body, &out); err != nil {
		return nil, err
	}
	res := make(map[int64]string, len(out))
	for _, id := range appIDs {
		if v := out["app/"+strconv.FormatInt(id, 10)]; v != nil && *v != "" {
			res[id] = *v
		}
	}
	return res, nil
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

// HistorySource is what the alerts need beyond Source: the lowest price a game had since a date. The real Client has it;
// ITAD's overview only knows the all-time low, so the two-year low comes from the price history (one call per game, made
// only for the few games that need it).
type HistorySource interface {
	// LowSince is the lowest price any shop asked since the given time; ok is false when the history has none.
	LowSince(ctx context.Context, id, country string, since time.Time) (low Money, ok bool, err error)
}

// lowLookback is how far before the window LowSince also reads, to find the price in effect when the window opens.
const lowLookback = 8 * 7 * 24 * time.Hour

// LowSince reads GET /games/history/v2 (every price change since a date) and keeps the lowest price in effect since
// `since`: the changes after it and the last one before it (a low price set a few weeks earlier and still running when the
// window opens counts). It reads a few weeks earlier than `since` to find that one.
func (c *Client) LowSince(ctx context.Context, id, country string, since time.Time) (Money, bool, error) {
	var out []struct {
		Timestamp string `json:"timestamp"`
		Deal      *struct {
			Price struct {
				Amount    float64 `json:"amount"`
				AmountInt int64   `json:"amountInt"`
				Currency  string  `json:"currency"`
			} `json:"price"`
		} `json:"deal"`
	}
	q := url.Values{"id": {id}, "country": {country}, "since": {since.Add(-lowLookback).UTC().Format("2006-01-02T15:04:05Z")}}
	if err := c.call(ctx, http.MethodGet, "/games/history/v2", q, nil, &out); err != nil {
		return Money{}, false, err
	}
	var low Money
	ok := false
	var startAt time.Time // the latest change before the window, and its price (no deal = not sold then)
	var start *Money
	take := func(m Money) {
		if !ok || m.Cents < low.Cents {
			low, ok = m, true
		}
	}
	for _, e := range out {
		var m *Money
		if e.Deal != nil && e.Deal.Price.Currency != "" {
			cents := e.Deal.Price.AmountInt
			if cents == 0 && e.Deal.Price.Amount > 0 {
				cents = int64(e.Deal.Price.Amount*100 + 0.5)
			}
			if cents >= 0 {
				m = &Money{cents, e.Deal.Price.Currency}
			}
		}
		at, err := time.Parse(time.RFC3339, e.Timestamp)
		if err == nil && at.Before(since) {
			if startAt.IsZero() || at.After(startAt) {
				startAt, start = at, m
			}
			continue
		}
		if m != nil {
			take(*m)
		}
	}
	if start != nil {
		take(*start)
	}
	return low, ok, nil
}

// Offers is POST /games/prices/v3: every shop's current offer (its regular price when not on sale) for up to
// MaxOffersBatch games, in one request.
func (c *Client) Offers(ctx context.Context, ids []string, country string) (map[string][]Current, error) {
	if len(ids) == 0 {
		return map[string][]Current{}, nil
	}
	if len(ids) > MaxOffersBatch {
		return nil, fmt.Errorf("at most %d games per call", MaxOffersBatch)
	}
	var out []struct {
		ID    string `json:"id"`
		Deals []struct {
			Shop    apiShop  `json:"shop"`
			Price   apiMoney `json:"price"`
			Regular apiMoney `json:"regular"`
			Cut     int      `json:"cut"`
			URL     string   `json:"url"`
			Expiry  string   `json:"expiry"`
		} `json:"deals"`
	}
	q := url.Values{"country": {country}, "vouchers": {"false"}}
	if err := c.call(ctx, http.MethodPost, "/games/prices/v3", q, ids, &out); err != nil {
		return nil, err
	}
	res := make(map[string][]Current, len(out))
	for _, g := range out {
		for _, d := range g.Deals {
			if d.Price.Currency == "" || d.Shop.Name == "" {
				continue
			}
			res[g.ID] = append(res[g.ID], Current{Shop: d.Shop.Name, Price: Money{d.Price.AmountInt, d.Price.Currency},
				Regular: Money{d.Regular.AmountInt, d.Regular.Currency}, Cut: clampCut(d.Cut), URL: webURL(d.URL), Expiry: d.Expiry})
		}
	}
	return res, nil
}

// shopsTTL: the list of shops hardly changes, so one read serves a day.
const shopsTTL = 24 * time.Hour

// Shops is GET /service/shops/v1: the names of the shops ITAD tracks in a country, sorted. Cached for a day.
func (c *Client) Shops(ctx context.Context, country string) ([]string, error) {
	c.shopsMu.Lock()
	if c.shopsFor == country && time.Since(c.shopsAt) < shopsTTL && c.shops != nil {
		out := append([]string(nil), c.shops...)
		c.shopsMu.Unlock()
		return out, nil
	}
	c.shopsMu.Unlock()
	var list []struct {
		Title string `json:"title"`
	}
	if err := c.call(ctx, http.MethodGet, "/service/shops/v1", url.Values{"country": {country}}, nil, &list); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list))
	for _, s := range list {
		if ValidShopName(s.Title) {
			names = append(names, s.Title)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	c.shopsMu.Lock()
	c.shops, c.shopsFor, c.shopsAt = names, country, time.Now()
	c.shopsMu.Unlock()
	return append([]string(nil), names...), nil
}
