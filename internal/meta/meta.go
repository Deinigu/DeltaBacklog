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

	// IGDBGap is the least time between two IGDB requests (IGDB allows 4 per second). The pacing is shared by
	// everything that uses the client: quick-add, posters and the sync.
	IGDBGap time.Duration

	mu    sync.Mutex
	token string
	exp   time.Time

	paceMu sync.Mutex
	next   time.Time // when the next IGDB request may start
}

// New returns a client with the real service URLs.
func New(cfg Config) *Client {
	return &Client{
		cfg:      cfg,
		IGDBGap:  250 * time.Millisecond,
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
	ParentID  int64    `json:"igdb_parent_id,omitempty"` // GameCandidate only: the game this one is another version of
	AltNames  []string `json:"-"`                        // IGDB's localized and alternative names; the IGDB sync scores against them too

	gameType int   // IGDB game_type: 0 main game, 1 DLC, 3 bundle, 8 remake, 9 remaster...
	released int64 // first release, unix seconds; 0 when unknown
	stores   int   // number of external ids (Steam, GOG, PSN...): a cheap sign of the game people mean; never shown
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

// pace waits for the caller's turn to send an IGDB request, at least IGDBGap after the previous one.
func (c *Client) pace(ctx context.Context) error {
	c.paceMu.Lock()
	now := time.Now()
	at := c.next
	if at.Before(now) {
		at = now
	}
	c.next = at.Add(c.IGDBGap)
	c.paceMu.Unlock()
	if d := at.Sub(now); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// igdbCollection is the collection reference IGDB nests in a game.
type igdbCollection struct {
	ID   int64
	Name string
}

// igdbReleaseDate is one release of a game on one platform; DateFormat is how exact it is (see Release).
type igdbReleaseDate struct {
	Date       int64 `json:"date"`
	DateFormat int   `json:"date_format"`
}

type igdbGame struct {
	ID               int64                   `json:"id"`
	Name             string                  `json:"name"`
	FirstReleaseDate int64                   `json:"first_release_date"`
	ReleaseDates     []igdbReleaseDate       `json:"release_dates"`
	GameType         int                     `json:"game_type"`
	ExternalGames    []int64                 `json:"external_games"` // store ids; only their number is used, to rank namesakes
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
	Collections      []igdbCollection        `json:"collections"`
	SimilarGames     []igdbGame              `json:"similar_games"`  // expanded by SimilarGames only
	ParentGame       int64                   `json:"parent_game"`    // set by the detail and collection queries only
	VersionParent    int64                   `json:"version_parent"` // ditto
	Franchises       []struct{ Name string } `json:"franchises"`
	Slug             string                  `json:"slug"`
	AlternativeNames []struct{ Name string } `json:"alternative_names"`
	Cover            struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
}

// versionTypes are the IGDB game_type values whose parent_game is the same game in another form: 9 remaster, 10 expanded
// game, 11 port. For the other types parent_game means something else (a standalone expansion, type 4, belongs to the game
// it expands, a remake, type 8, to the game it remakes), and those are games of their own.
var versionTypes = map[int]bool{9: true, 10: true, 11: true}

// parent is the IGDB game this one is another version of: its version_parent (editions), else its parent_game when the
// game is a remaster, an expanded game or a port; 0 when it has none (also for expansions and remakes).
func (g igdbGame) parent() int64 {
	if g.VersionParent > 0 {
		return g.VersionParent
	}
	if g.ParentGame > 0 && versionTypes[g.GameType] {
		return g.ParentGame
	}
	return 0
}

// searchLimit is how many hits IGDB is asked for: its relevance order buries the original of a long-running
// series (God of War 2005 is 17th for "God of War"), so we fetch plenty and rank them ourselves.
const searchLimit = 50

// searchCap bounds the non-exact hits SearchIGDB returns. Games named exactly like the query are never cut: the
// IGDB sync classifies against the whole list, and a safe link needs to see every namesake. Only quick-add
// shortens the list (TopResults).
const searchCap = 30

// exactLimit is how many games the exact-name query may return (IGDB allows 500; "Pac-Man" has over 50). The
// response stays small: a few hundred kilobytes at most.
const exactLimit = 200

// maxResults is how many candidates quick-add shows.
const maxResults = 12

// SearchIGDB returns up to 30 ranked candidates for a title. A year in the query ("God of War 2005", "Portal (2007)")
// is a hint, not part of the search text. IGDB's own search results are merged with the games named exactly like
// the query, then ranked by RankCandidates.
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
	text, year := SplitYear(title)
	names := []string{title} // the whole title first: "Madden NFL 2005" may be the real name
	if text != title {
		names = append(names, text)
	}
	// IGDB's search ignores alternative names and wants close spelling. The main pass searches and also asks for
	// the games named exactly like the query. When it finds nothing, try the localized names, then the title
	// before its subtitle, then the title with its spaces removed ("Little Big Planet").
	passes := [][]string{{searchQuery(text)}}
	if text != title { // "FIFA 2003" is a name IGDB spells "FIFA Soccer 2003": search the year too
		passes[0] = []string{searchQuery(title), searchQuery(text)}
	}
	for _, n := range names {
		passes[0] = append(passes[0], exactQuery(n))
	}
	passes = append(passes, []string{altNameQuery(text)})
	if i := strings.IndexAny(text, ":-"); i >= 3 {
		passes = append(passes, []string{searchQuery(strings.TrimSpace(text[:i]))})
	}
	if joined := strings.Join(strings.Fields(text), ""); joined != text && len(joined) >= 4 {
		passes = append(passes, []string{searchQuery(joined)})
	}
	for _, pass := range passes {
		var all []Candidate
		seen := map[int64]bool{}
		for qi, q := range pass {
			if err := c.pace(ctx); err != nil {
				return nil, err
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.IGDBURL+"/games", strings.NewReader(q))
			req.Header.Set("Client-ID", c.cfg.IGDBClientID)
			req.Header.Set("Authorization", "Bearer "+token)
			var games []igdbGame
			if err := c.do(req, &games); err != nil {
				// An extra query that fails (not 429, not a cancelled request) must not throw away good hits.
				var he *HTTPError
				if qi > 0 && len(all) > 0 && ctx.Err() == nil && !(errors.As(err, &he) && he.Status == 429) {
					continue
				}
				return nil, err
			}
			for _, g := range games {
				if !seen[g.ID] {
					seen[g.ID] = true
					all = append(all, g.candidate())
				}
			}
		}
		if len(all) == 0 {
			continue
		}
		all = RankCandidates(all, names, year)
		namesakes := 0
		for _, c := range all {
			if exactLevel(c, names) < len(names) {
				namesakes++
			}
		}
		return all[:min(len(all), namesakes+searchCap)], nil
	}
	return []Candidate{}, nil
}

const searchFields = `fields name,first_release_date,game_type,genres.name,themes.name,platforms.abbreviation,platforms.name,` +
	`involved_companies.developer,involved_companies.company.name,collections.name,franchises.name,cover.image_id,slug,alternative_names.name,external_games; `

var igdbEscape = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func searchQuery(title string) string {
	return fmt.Sprintf(`search "%s"; `, igdbEscape.Replace(title)) + searchFields + "limit 50;"
}

// exactQuery asks for the games whose name is the title (IGDB's `~` without wildcards: case-insensitive equality).
func exactQuery(title string) string {
	return fmt.Sprintf(`where name ~ "%s"; `, igdbEscape.Replace(title)) + searchFields + fmt.Sprintf("limit %d;", exactLimit)
}

func altNameQuery(title string) string {
	return fmt.Sprintf(`where alternative_names.name ~ *"%s"*; `, igdbEscape.Replace(title)) + searchFields + "limit 50;"
}

var yearHint = regexp.MustCompile(`^(.*?\S)\s*(?:\((\d{4})\)|(\d{4}))$`)

// SplitYear separates a trailing release year ("God of War 2005", "Portal (2007)") from a title. The year must be
// plausible (1970 to next year) and something must remain of the title; otherwise the title is returned whole.
func SplitYear(title string) (string, int) {
	m := yearHint.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return title, 0
	}
	y, _ := strconv.Atoi(m[2] + m[3])
	if y < 1970 || y > time.Now().Year()+1 {
		return title, 0
	}
	return strings.TrimRight(m[1], " ,-:"), y
}

// demotedTypes are IGDB game types that are rarely what a player means: DLC, expansion, bundle, mod, episode,
// season, pack, update.
var demotedTypes = map[int]bool{1: true, 2: true, 3: true, 5: true, 6: true, 7: true, 12: true, 13: true, 14: true}

// typeTier orders IGDB game types for games with the same name: main games, standalone expansions and expanded
// games (0), remakes and remasters (1), ports (2), then DLC, bundles, mods, episodes, packs and updates (3).
func typeTier(t int) int {
	switch {
	case demotedTypes[t]:
		return 3
	case t == 11:
		return 2
	case t == 8 || t == 9:
		return 1
	}
	return 0
}

// RankCandidates orders search hits: games named like one of names first, then games released in year (when
// non-zero). Among namesakes DLC, bundles and mods come last; the rest go by number of store ids (Steam, GOG, PSN...,
// the game people mean has the most), then main game before remake, remaster and port, then older first. Other hits
// keep IGDB's order.
func RankCandidates(cands []Candidate, names []string, year int) []Candidate {
	type ranked struct {
		c      Candidate
		exact  int // 0 when the name is the first of names, 1 the second, len(names) when no name matches
		yearOK bool
		order  int
	}
	rs := make([]ranked, len(cands))
	for i, c := range cands {
		r := ranked{c: c, order: i, yearOK: year != 0 && c.Year == year, exact: exactLevel(c, names)}
		rs[i] = r
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.exact != b.exact {
			return a.exact < b.exact
		}
		if a.yearOK != b.yearOK {
			return a.yearOK
		}
		ta, tb := typeTier(a.c.gameType), typeTier(b.c.gameType)
		if a.exact < len(names) {
			if (ta == 3) != (tb == 3) {
				return ta != 3
			}
			if a.c.stores != b.c.stores {
				return a.c.stores > b.c.stores
			}
			if ta != tb {
				return ta < tb
			}
			if a.c.released != b.c.released && a.c.released != 0 && b.c.released != 0 {
				return a.c.released < b.c.released
			}
		} else if (ta == 3) != (tb == 3) {
			return ta != 3
		}
		return a.order < b.order
	})
	out := make([]Candidate, len(rs))
	for i, r := range rs {
		out[i] = r.c
	}
	return out
}

// exactLevel is the index in names of the first name the candidate is called, or len(names) when none.
func exactLevel(c Candidate, names []string) int {
	for k, n := range names {
		if SameName(c.Name, n) || TitleScore(c.Name, n) == 100 {
			return k
		}
	}
	return len(names)
}

// maxNamesakes is how many of the results may be games named exactly like the query, so that a franchise name
// ("Doom", "Tetris") still shows its sequels and relatives next to the many games of that name.
const maxNamesakes = 6

// TopResults shortens a SearchIGDB list for display: the first maxResults, with at most maxNamesakes games named
// exactly like title (a trailing year is ignored too); leftover namesakes only fill slots nothing else wants. Only
// quick-add uses it: the sync needs the whole list.
func TopResults(ranked []Candidate, title string) []Candidate {
	names := []string{title}
	if text, _ := SplitYear(title); text != title {
		names = append(names, text)
	}
	var out, spare []Candidate
	namesakes := 0
	for _, c := range ranked {
		if exactLevel(c, names) < len(names) {
			if namesakes >= maxNamesakes {
				spare = append(spare, c)
				continue
			}
			namesakes++
		}
		out = append(out, c)
	}
	out = append(out, spare...)
	return out[:min(len(out), maxResults)]
}

func (g igdbGame) candidate() Candidate {
	cand := Candidate{IGDBID: g.ID, Name: g.Name, Slug: g.Slug, gameType: g.GameType, released: g.FirstReleaseDate, stores: len(g.ExternalGames), Platforms: []string{}, Genres: []string{}, Themes: []string{}}
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
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
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
	Release      Release // the full first release date; the zero value when IGDB has none
	Platforms    []string
	Genres       []string
	Themes       []string
	Developer    string
	Series       string          // IGDB's collection only (never a franchise: those are far too broad to fill a series from)
	Collections  []CollectionRef // every IGDB collection of the game
	Modes        []string        // game modes: Single player, Co-operative...
	Perspectives []string        // player perspectives: First person, Third person...
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

const detailFields = `fields id,first_release_date,release_dates.date,release_dates.date_format,genres.name,themes.name,platforms.abbreviation,platforms.name,` +
	`involved_companies.developer,involved_companies.company.name,collections.id,collections.name,` +
	`game_modes.name,player_perspectives.name,websites.url,websites.type;`

func (c *Client) igdbPost(ctx context.Context, endpoint, query string, out any) error {
	if ok, _ := c.Configured(); !ok {
		return ErrNotConfigured
	}
	token, err := c.igdbToken(ctx)
	if err != nil {
		return err
	}
	if err := c.pace(ctx); err != nil {
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
		d := Details{IGDBID: g.ID, Year: cand.Year, Release: g.igdbGame.release(), Platforms: cand.Platforms, Genres: cand.Genres, Themes: cand.Themes, Developer: cand.Developer}
		if len(g.Collections) > 0 {
			d.Series = g.Collections[0].Name
		}
		for _, c := range g.Collections {
			d.Collections = append(d.Collections, CollectionRef{ID: c.ID, Name: c.Name})
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

// ---------------- portrait posters (rankings) ----------------

// IGDBCoverURL returns the portrait cover of an IGDB game at twice the "big" size (528x748), or "" when IGDB holds none.
func (c *Client) IGDBCoverURL(ctx context.Context, igdbID int64) (string, error) {
	if ok, _ := c.Configured(); !ok {
		return "", ErrNotConfigured
	}
	token, err := c.igdbToken(ctx)
	if err != nil {
		return "", err
	}
	query := fmt.Sprintf("fields cover.image_id; where id = %d;", igdbID)
	if err := c.pace(ctx); err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.IGDBURL+"/games", strings.NewReader(query))
	req.Header.Set("Client-ID", c.cfg.IGDBClientID)
	req.Header.Set("Authorization", "Bearer "+token)
	var games []struct {
		Cover struct {
			ImageID string `json:"image_id"`
		} `json:"cover"`
	}
	if err := c.do(req, &games); err != nil {
		return "", err
	}
	if len(games) == 0 || games[0].Cover.ImageID == "" {
		return "", nil
	}
	return igdbImg + "t_cover_big_2x/" + games[0].Cover.ImageID + ".jpg", nil
}

// SteamGridPortrait returns the URL of a 600x900 grid of the best SteamGridDB match for title, or "" when there is none.
func (c *Client) SteamGridPortrait(ctx context.Context, title string) (string, error) {
	if _, ok := c.Configured(); !ok {
		return "", ErrNotConfigured
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", nil
	}
	var found struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.sgdbGet(ctx, "/search/autocomplete/"+url.PathEscape(title), &found); err != nil {
		return "", err
	}
	if len(found.Data) == 0 {
		return "", nil
	}
	best := found.Data[0]
	for _, d := range found.Data {
		if strings.EqualFold(d.Name, title) {
			best = d
			break
		}
	}
	var res struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	q := url.Values{"types": {"static"}, "dimensions": {"600x900"}, "limit": {"1"}}
	if err := c.sgdbGet(ctx, "/grids/game/"+strconv.FormatInt(best.ID, 10)+"?"+q.Encode(), &res); err != nil {
		return "", err
	}
	if len(res.Data) == 0 {
		return "", nil
	}
	return res.Data[0].URL, nil
}

// ---------------- poster options (the portrait picker) ----------------

// PosterOption sizes: SteamGridDB's portrait grids (600x900 is the one the automatic poster uses).
const sgdbPortraitDims = "600x900,660x930,342x482"

// SearchPosters returns portrait grids of the best SteamGridDB match for title, several of them, not just the first.
func (c *Client) SearchPosters(ctx context.Context, title string) ([]CoverOption, error) {
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
	for _, d := range found.Data {
		if strings.EqualFold(d.Name, title) {
			best = d
			break
		}
	}
	var res struct {
		Data []struct {
			URL    string `json:"url"`
			Thumb  string `json:"thumb"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"data"`
	}
	q := url.Values{"types": {"static"}, "dimensions": {sgdbPortraitDims}, "limit": {"18"}}
	if err := c.sgdbGet(ctx, "/grids/game/"+strconv.FormatInt(best.ID, 10)+"?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	out := make([]CoverOption, 0, len(res.Data))
	for _, g := range res.Data {
		out = append(out, CoverOption{URL: g.URL, Thumb: g.Thumb, Width: g.Width, Height: g.Height, Source: "steamgriddb"})
	}
	return out, nil
}

// IGDBPosters returns the portrait cover IGDB holds for a game (igdbID, or the best title match when it is 0) at the
// size the automatic poster uses.
func (c *Client) IGDBPosters(ctx context.Context, igdbID int64, title string) ([]CoverOption, error) {
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
	u, err := c.IGDBCoverURL(ctx, igdbID)
	if err != nil {
		return nil, err
	}
	if u == "" {
		return []CoverOption{}, nil
	}
	thumb := strings.Replace(u, "t_cover_big_2x", "t_cover_big", 1)
	return []CoverOption{{URL: u, Thumb: thumb, Width: 528, Height: 748, Source: "igdb", Kind: "Cover"}}, nil
}
