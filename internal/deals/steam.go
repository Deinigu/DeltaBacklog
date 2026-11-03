package deals

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
	"time"
)

// WishEntry is one game on the Steam wishlist.
type WishEntry struct {
	AppID   int64
	AddedAt string // RFC 3339, empty when Steam gives none
}

// WishlistSource reads the owner's Steam wishlist (the real Steam client, or a fake in tests).
type WishlistSource interface {
	// Wishlist lists the games on the wishlist. ErrWishlistEmpty means Steam returned none (private or empty).
	Wishlist(ctx context.Context) ([]WishEntry, error)
}

var (
	// ErrBadSteamID: STEAM_ID is not a 17-digit SteamID64. The message never holds the value.
	ErrBadSteamID = errors.New("STEAM_ID must be a 17-digit SteamID64")
	// ErrWishlistEmpty: Steam sent no games. A private wishlist looks the same as an empty one.
	ErrWishlistEmpty = errors.New("Steam sent no wishlist games: the wishlist is empty or not public (Steam: Profile > Edit Profile > Privacy Settings > Game details: Public)")
)

var steamIDRe = regexp.MustCompile(`^[0-9]{17}$`)

// ValidSteamID reports whether s is a 17-digit SteamID64.
func ValidSteamID(s string) bool { return steamIDRe.MatchString(s) }

const steamHost = "api.steampowered.com"

// Steam reads the public wishlist of one account from Steam's keyless IWishlistService. The id is part of the request
// address, so no error from here ever carries that address, and the id is never logged.
type Steam struct {
	id   string
	http *http.Client

	// BaseURL is overridable in tests; only the real host and loopback addresses are ever called.
	BaseURL string
}

// NewSteam returns a client for the wishlist of a SteamID64. An empty id gives (nil, nil): the wishlist is off.
func NewSteam(steamID string) (*Steam, error) {
	steamID = strings.TrimSpace(steamID)
	if steamID == "" {
		return nil, nil
	}
	if !ValidSteamID(steamID) {
		return nil, ErrBadSteamID
	}
	return &Steam{
		id:      steamID,
		BaseURL: "https://" + steamHost,
		http: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *Steam) Wishlist(ctx context.Context) ([]WishEntry, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Hostname() != steamHost && !isLoopback(u.Hostname())) {
		return nil, errors.New("Steam host is not allowed")
	}
	u = u.JoinPath("IWishlistService/GetWishlist/v1/")
	u.RawQuery = url.Values{"steamid": {c.id}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("could not build the Steam request")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil { // a *url.Error would carry the address, and with it the id
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Steam could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Steam answered %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
	if err != nil {
		return nil, errors.New("Steam answer could not be read")
	}
	if len(data) > 4<<20 {
		return nil, errors.New("Steam answer is too large")
	}
	var out struct {
		Response struct {
			Items []struct {
				AppID     int64 `json:"appid"`
				DateAdded int64 `json:"date_added"`
			} `json:"items"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.New("Steam answered something that is not JSON")
	}
	seen := map[int64]bool{}
	var list []WishEntry
	for _, it := range out.Response.Items {
		if it.AppID <= 0 || seen[it.AppID] {
			continue
		}
		seen[it.AppID] = true
		e := WishEntry{AppID: it.AppID}
		if it.DateAdded > 0 {
			e.AddedAt = time.Unix(it.DateAdded, 0).UTC().Format(timeFmt)
		}
		list = append(list, e)
	}
	if len(list) == 0 {
		return nil, ErrWishlistEmpty
	}
	return list, nil
}

// placeholderTitle is what a wishlist game is called until ITAD tells its name.
func placeholderTitle(appid int64) string { return "Steam app " + strconv.FormatInt(appid, 10) }
