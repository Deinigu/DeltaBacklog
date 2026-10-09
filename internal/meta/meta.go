// Package meta looks games up on IGDB (details), SteamGridDB (cover art) and Steam (link resolution).
// All hosts are overridable so tests can point it at local fake servers.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds the API credentials. Empty values disable the matching feature.
type Config struct {
	IGDBClientID     string
	IGDBClientSecret string
	SteamGridDBKey   string
}

// Client talks to the metadata services.
type Client struct {
	cfg  Config
	http *http.Client

	// Base URLs, overridable in tests.
	TokenURL string
	IGDBURL  string
	SGDBURL  string
	SteamURL string

	mu    sync.Mutex
	token string
	exp   time.Time
}

// New returns a client with the real service URLs.
func New(cfg Config) *Client {
	return &Client{
		cfg:      cfg,
		http:     &http.Client{Timeout: 12 * time.Second},
		TokenURL: "https://id.twitch.tv/oauth2/token",
		IGDBURL:  "https://api.igdb.com/v4",
		SGDBURL:  "https://www.steamgriddb.com/api/v2",
		SteamURL: "https://store.steampowered.com/api/appdetails",
	}
}

// Configured reports which services have credentials.
func (c *Client) Configured() (igdb, sgdb bool) {
	return c.cfg.IGDBClientID != "" && c.cfg.IGDBClientSecret != "", c.cfg.SteamGridDBKey != ""
}

// ErrNotConfigured is returned when a service has no credentials.
var ErrNotConfigured = errors.New("service not configured")

// Candidate is one IGDB search hit with the details quick-add pre-fills.
type Candidate struct {
	IGDBID    int64    `json:"igdb_id"`
	Name      string   `json:"name"`
	Year      int      `json:"year,omitempty"`
	Platforms []string `json:"platforms"` // shown as a hint only: the user picks their own platform
	Genres    []string `json:"genres"`
	Themes    []string `json:"themes"`
	Developer string   `json:"developer,omitempty"`
	Series    string   `json:"series,omitempty"`
	CoverURL  string   `json:"cover_url,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	AltNames  []string `json:"-"` // IGDB's localized and alternative names; the IGDB sync scores against them too
}

// CoverOption is one SteamGridDB image.
type CoverOption struct {
	URL    string `json:"url"`
	Thumb  string `json:"thumb"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Source string `json:"source"`
	Kind   string `json:"kind,omitempty"` // IGDB only: "Cover", "Artwork" or "Screenshot"
}

func (c *Client) do(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		he := &HTTPError{Method: req.Method, Host: req.URL.Host, Status: res.StatusCode, Text: res.Status}
		if n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && n > 0 {
			he.RetryAfter = time.Duration(n) * time.Second
		}
		return he
	}
	return json.Unmarshal(body, out)
}

// HTTPError is a non-200 answer from a service. Callers that pace themselves (the IGDB sync) look at Status:
// 429 means "slow down".
type HTTPError struct {
	Method, Host, Text string
	Status             int
	RetryAfter         time.Duration // the Retry-After header, when the service sent one in seconds
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s %s: %s", e.Method, e.Host, e.Text) }

// ---------------- IGDB ----------------

func (c *Client) igdbToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp) {
		return c.token, nil
	}
	q := url.Values{"client_id": {c.cfg.IGDBClientID}, "client_secret": {c.cfg.IGDBClientSecret}, "grant_type": {"client_credentials"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL+"?"+q.Encode(), nil)
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.do(req, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("igdb: empty access token")
	}
	c.token = out.AccessToken
	c.exp = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return c.token, nil
}

type igdbGame struct {
	ID               int64                   `json:"id"`
	Name             string                  `json:"name"`
	FirstReleaseDate int64                   `json:"first_release_date"`
	Genres           []struct{ Name string } `json:"genres"`
	Themes           []struct{ Name string } `json:"themes"`
	Platforms        []struct {
		Abbreviation string
		Name         string
	} `json:"platforms"`
	InvolvedCompanies []struct {
		Developer bool
		Company   struct{ Name string }
	} `json:"involved_companies"`
	Collections      []struct{ Name string } `json:"collections"`
	Franchises       []struct{ Name string } `json:"franchises"`
	Slug             string                  `json:"slug"`
	AlternativeNames []struct{ Name string } `json:"alternative_names"`
	Cover            struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
}

// SearchIGDB returns up to 8 candidates for a title, in IGDB's relevance order.
func (c *Client) SearchIGDB(ctx context.Context, title string) ([]Candidate, error) {
	if ok, _ := c.Configured(); !ok {
		return nil, ErrNotConfigured
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}
	token, err := c.igdbToken(ctx)
	if err != nil {
		return nil, err
	}
	// IGDB's search ignores alternative names and wants close spelling. When it finds nothing, try the localized
	// names, then the title before its subtitle, then the title with its spaces removed ("Little Big Planet").
	queries := []string{searchQuery(title), altNameQuery(title)}
	if i := strings.IndexAny(title, ":-"); i >= 3 {
		queries = append(queries, searchQuery(strings.TrimSpace(title[:i])))
	}
	if joined := strings.Join(strings.Fields(title), ""); joined != title && len(joined) >= 4 {
		queries = append(queries, searchQuery(joined))
	}
	for _, q := range queries {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.IGDBURL+"/games", strings.NewReader(q))
		req.Header.Set("Client-ID", c.cfg.IGDBClientID)
		req.Header.Set("Authorization", "Bearer "+token)
		var games []igdbGame
		if err := c.do(req, &games); err != nil {
			return nil, err
		}
		if len(games) == 0 {
			continue
		}
		out := make([]Candidate, 0, len(games))
		for _, g := range games {
			out = append(out, g.candidate())
		}
		return out, nil
	}
	return []Candidate{}, nil
}

const searchFields = `fields name,first_release_date,genres.name,themes.name,platforms.abbreviation,platforms.name,` +
	`involved_companies.developer,involved_companies.company.name,collections.name,franchises.name,cover.image_id,slug,alternative_names.name; limit 8;`

var igdbEscape = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func searchQuery(title string) string {
	return fmt.Sprintf(`search "%s"; `, igdbEscape.Replace(title)) + searchFields
}

func altNameQuery(title string) string {
	return fmt.Sprintf(`where alternative_names.name ~ *"%s"*; `, igdbEscape.Replace(title)) + searchFields
}

func (g igdbGame) candidate() Candidate {
	cand := Candidate{IGDBID: g.ID, Name: g.Name, Slug: g.Slug, Platforms: []string{}, Genres: []string{}, Themes: []string{}}
	if g.FirstReleaseDate > 0 {
		cand.Year = time.Unix(g.FirstReleaseDate, 0).UTC().Year()
	}
	for _, x := range g.AlternativeNames {
		cand.AltNames = append(cand.AltNames, x.Name)
	}
	for _, x := range g.Genres {
		cand.Genres = append(cand.Genres, x.Name)
	}
	for _, x := range g.Themes {
		cand.Themes = append(cand.Themes, x.Name)
	}
	for _, p := range g.Platforms {
		if p.Abbreviation != "" {
			cand.Platforms = append(cand.Platforms, p.Abbreviation)
		} else if p.Name != "" {
			cand.Platforms = append(cand.Platforms, p.Name)
		}
	}
	for _, ic := range g.InvolvedCompanies {
		if ic.Developer && ic.Company.Name != "" {
			cand.Developer = ic.Company.Name
			break
		}
	}
	if len(g.Collections) > 0 {
		cand.Series = g.Collections[0].Name
	} else if len(g.Franchises) > 0 {
		cand.Series = g.Franchises[0].Name
	}
	if g.Cover.ImageID != "" {
		cand.CoverURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + g.Cover.ImageID + ".jpg"
	}
	return cand
}

// ---------------- SteamGridDB ----------------

func (c *Client) sgdbGet(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.SGDBURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.cfg.SteamGridDBKey)
	return c.do(req, out)
}

// SearchCovers returns wide grid images (the shape the library already uses) for the best title match,
// falling back to any grid shape when there are no wide ones.
func (c *Client) SearchCovers(ctx context.Context, title string) ([]CoverOption, error) {
	if _, ok := c.Configured(); !ok {
		return nil, ErrNotConfigured
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}
	var found struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.sgdbGet(ctx, "/search/autocomplete/"+url.PathEscape(title), &found); err != nil {
		return nil, err
	}
	if len(found.Data) == 0 {
		return []CoverOption{}, nil
	}
	best := found.Data[0]
	for _, d := range found.Data { // an exact (case-insensitive) title match beats the first hit
		if strings.EqualFold(d.Name, title) {
			best = d
			break
		}
	}
	grids := func(dims string) ([]CoverOption, error) {
		var res struct {
			Data []struct {
				URL    string `json:"url"`
				Thumb  string `json:"thumb"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"data"`
		}
		q := url.Values{"types": {"static"}, "limit": {"18"}}
		if dims != "" {
			q.Set("dimensions", dims)
		}
		if err := c.sgdbGet(ctx, "/grids/game/"+strconv.FormatInt(best.ID, 10)+"?"+q.Encode(), &res); err != nil {
			return nil, err
		}
		out := make([]CoverOption, 0, len(res.Data))
		for _, g := range res.Data {
			out = append(out, CoverOption{URL: g.URL, Thumb: g.Thumb, Width: g.Width, Height: g.Height, Source: "steamgriddb"})
		}
		return out, nil
	}
	wide, err := grids("460x215,920x430")
	if err != nil {
		return nil, err
	}
	if len(wide) > 0 {
		return wide, nil
	}
	return grids("")
}

// ---------------- link resolution ----------------

var (
	steamApp = regexp.MustCompile(`^https?://store\.steampowered\.com/app/(\d+)`)
	titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	ogTitle  = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	suffixes = regexp.MustCompile(`(?i)\s*(on steam|[-–|]\s*gog\.com|[-–|]\s*epic games store|[-–|]\s*igdb\.com|[-–|]\s*buy.*)$`)
)

// LooksLikeLink reports whether the search box holds a URL rather than a title.
func LooksLikeLink(q string) bool {
	q = strings.TrimSpace(q)
	return strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://")
}

// allowedLinkHosts are the only sites a pasted link may point at: the server never fetches anything else.
func allowedLinkHost(host string) bool {
	host = strings.ToLower(host)
	for _, h := range []string{"store.steampowered.com", "gog.com", "epicgames.com", "igdb.com"} {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// linkClient is the HTTP client for fetching a pasted link's page. It follows redirects only between allowed
// hosts, so an allowed page cannot bounce the server to an address inside the user's network.
func (c *Client) linkClient() *http.Client {
	return &http.Client{
		Timeout: c.http.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("too many redirects")
			}
			if !allowedLinkHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to %s is not allowed", req.URL.Hostname())
			}
			return nil
		},
	}
}

// ResolveLink turns a store or IGDB link into a game title to search for.
func (c *Client) ResolveLink(ctx context.Context, link string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !allowedLinkHost(u.Hostname()) {
		return "", errors.New("unsupported link: use a Steam, GOG, Epic or IGDB game page")
	}
	// IGDB pages carry the title in the slug, so no request is needed.
	if strings.HasSuffix(strings.ToLower(u.Hostname()), "igdb.com") {
		if parts := strings.Split(strings.Trim(u.Path, "/"), "/"); len(parts) >= 2 && parts[0] == "games" {
			return strings.Join(strings.Fields(strings.ReplaceAll(parts[1], "-", " ")), " "), nil
		}
		return "", errors.New("not an IGDB game page")
	}
	if m := steamApp.FindStringSubmatch(u.String()); m != nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.SteamURL+"?appids="+m[1]+"&filters=basic", nil)
		var out map[string]struct {
			Success bool `json:"success"`
			Data    struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := c.do(req, &out); err != nil {
			return "", err
		}
		if e, ok := out[m[1]]; ok && e.Success && e.Data.Name != "" {
			return e.Data.Name, nil
		}
		return "", errors.New("Steam does not know that app")
	}
	// GOG / Epic: read the page title.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (DeltaBacklog link lookup)")
	res, err := c.linkClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("that page answered %s", res.Status)
	}
	html, _ := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	title := ""
	if m := ogTitle.FindSubmatch(html); m != nil {
		title = string(m[1])
	} else if m := titleTag.FindSubmatch(html); m != nil {
		title = string(m[1])
	}
	title = strings.TrimSpace(suffixes.ReplaceAllString(strings.TrimSpace(title), ""))
	if title == "" {
		return "", errors.New("could not read a game title from that page")
	}
	return title, nil
}

// RankByTitle puts exact (case-insensitive) title matches first, otherwise keeps IGDB's order.
func RankByTitle(cands []Candidate, title string) []Candidate {
	out := append([]Candidate(nil), cands...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.EqualFold(out[i].Name, title) && !strings.EqualFold(out[j].Name, title)
	})
	return out
}

// ---------------- IGDB images ----------------

const igdbImg = "https://images.igdb.com/igdb/image/upload/"

// IGDBImages returns the pictures IGDB holds for a game: wide artwork and screenshots first (the library's cards
// are wide), then the portrait cover. igdbID may be 0, in which case the best title match is used.
func (c *Client) IGDBImages(ctx context.Context, igdbID int64, title string) ([]CoverOption, error) {
	if ok, _ := c.Configured(); !ok {
		return nil, ErrNotConfigured
	}
	if igdbID == 0 {
		cands, err := c.SearchIGDB(ctx, title)
		if err != nil || len(cands) == 0 {
			return []CoverOption{}, err
		}
		igdbID = RankByTitle(cands, title)[0].IGDBID
	}
	token, err := c.igdbToken(ctx)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("fields name,cover.image_id,artworks.image_id,artworks.width,artworks.height,screenshots.image_id,screenshots.width,screenshots.height; where id = %d;", igdbID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.IGDBURL+"/games", strings.NewReader(query))
	req.Header.Set("Client-ID", c.cfg.IGDBClientID)
	req.Header.Set("Authorization", "Bearer "+token)
	type picture struct {
		ImageID string `json:"image_id"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
	}
	var games []struct {
		Cover struct {
			ImageID string `json:"image_id"`
		} `json:"cover"`
		Artworks    []picture `json:"artworks"`
		Screenshots []picture `json:"screenshots"`
	}
	if err := c.do(req, &games); err != nil {
		return nil, err
	}
	out := []CoverOption{}
	if len(games) == 0 {
		return out, nil
	}
	g := games[0]
	add := func(id, size, thumb, kind string, w, h int) {
		if w <= 0 || h <= 0 { // IGDB did not say: assume widescreen
			w, h = 1920, 1080
		}
		if id != "" {
			out = append(out, CoverOption{URL: igdbImg + size + "/" + id + ".jpg", Thumb: igdbImg + thumb + "/" + id + ".jpg", Width: w, Height: h, Source: "igdb", Kind: kind})
		}
	}
	for _, a := range g.Artworks {
		add(a.ImageID, "t_1080p", "t_screenshot_med", "Artwork", a.Width, a.Height)
	}
	for i, s := range g.Screenshots {
		if i == 8 { // screenshots are plentiful; the first few are enough
			break
		}
		add(s.ImageID, "t_1080p", "t_screenshot_med", "Screenshot", s.Width, s.Height)
	}
	add(g.Cover.ImageID, "t_cover_big", "t_cover_small", "Cover", 264, 374)
	return out, nil
}

// ---------------- IGDB enrichment ----------------

// StoreLink is a store or official-site page of a game. Kind is "steam", "gog", "epic" or "official".
type StoreLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// Details is what the IGDB enrichment reads for an already linked game. IGDB's critic and user scores
// (aggregated_rating, total_rating, hypes) are deliberately not part of it and are never requested.
type Details struct {
	IGDBID       int64
	Year         int
	Platforms    []string
	Genres       []string
	Themes       []string
	Developer    string
	Series       string   // IGDB's collection only (never a franchise: those are far too broad to fill a series from)
	Modes        []string // game modes: Single player, Co-operative...
	Perspectives []string // player perspectives: First person, Third person...
	Links        []StoreLink
}

// TimeToBeat is IGDB's crowd-sourced playing time, in seconds; 0 means IGDB has no value.
type TimeToBeat struct{ Hastily, Normally, Completely int }

type igdbDetail struct {
	igdbGame
	GameModes         []struct{ Name string } `json:"game_modes"`
	PlayerPerspective []struct{ Name string } `json:"player_perspectives"`
	Websites          []struct {
		URL  string `json:"url"`
		Type int    `json:"type"`
	} `json:"websites"`
}

const detailFields = `fields id,first_release_date,genres.name,themes.name,platforms.abbreviation,platforms.name,` +
	`involved_companies.developer,involved_companies.company.name,collections.name,` +
	`game_modes.name,player_perspectives.name,websites.url,websites.type;`

func (c *Client) igdbPost(ctx context.Context, endpoint, query string, out any) error {
	if ok, _ := c.Configured(); !ok {
		return ErrNotConfigured
	}
	token, err := c.igdbToken(ctx)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.IGDBURL+endpoint, strings.NewReader(query))
	req.Header.Set("Client-ID", c.cfg.IGDBClientID)
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, out)
}

func idList(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// GameDetails reads the facts of up to ~50 games in one request. Games IGDB no longer knows are absent from the result.
func (c *Client) GameDetails(ctx context.Context, ids []int64) ([]Details, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var games []igdbDetail
	q := fmt.Sprintf("%s where id = (%s); limit %d;", detailFields, idList(ids), len(ids))
	if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	out := make([]Details, 0, len(games))
	for _, g := range games {
		cand := g.igdbGame.candidate()
		d := Details{IGDBID: g.ID, Year: cand.Year, Platforms: cand.Platforms, Genres: cand.Genres, Themes: cand.Themes, Developer: cand.Developer}
		if len(g.Collections) > 0 {
			d.Series = g.Collections[0].Name
		}
		for _, m := range g.GameModes {
			d.Modes = append(d.Modes, m.Name)
		}
		for _, p := range g.PlayerPerspective {
			d.Perspectives = append(d.Perspectives, p.Name)
		}
		d.Links = storeLinks(g)
		out = append(out, d)
	}
	return out, nil
}

// storeLinks keeps at most one link per kind (Steam, GOG, Epic, official site), recognised by host.
func storeLinks(g igdbDetail) []StoreLink {
	seen := map[string]bool{}
	var out []StoreLink
	for _, w := range g.Websites {
		u, err := url.Parse(strings.TrimSpace(w.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		kind := ""
		switch {
		case host == "store.steampowered.com":
			kind = "steam"
		case host == "gog.com" || strings.HasSuffix(host, ".gog.com"):
			kind = "gog"
		case host == "epicgames.com" || strings.HasSuffix(host, ".epicgames.com"):
			kind = "epic"
		case w.Type == 1: // IGDB website type "official"
			kind = "official"
		}
		if kind != "" && !seen[kind] {
			seen[kind] = true
			out = append(out, StoreLink{Kind: kind, URL: u.String()})
		}
	}
	return out
}

// TimeToBeat reads IGDB's time to beat for up to ~50 games in one request. Games without one are absent.
func (c *Client) TimeToBeat(ctx context.Context, ids []int64) (map[int64]TimeToBeat, error) {
	out := map[int64]TimeToBeat{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		GameID     int64 `json:"game_id"`
		Hastily    int   `json:"hastily"`
		Normally   int   `json:"normally"`
		Completely int   `json:"completely"`
	}
	q := fmt.Sprintf("fields game_id,hastily,normally,completely; where game_id = (%s); limit %d;", idList(ids), len(ids))
	if err := c.igdbPost(ctx, "/game_time_to_beats", q, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.GameID] = TimeToBeat{Hastily: r.Hastily, Normally: r.Normally, Completely: r.Completely}
	}
	return out, nil
}
