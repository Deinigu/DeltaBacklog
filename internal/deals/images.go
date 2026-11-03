package deals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "image/jpeg"
	_ "image/png"
)

// Wishlist games have no cover of their own: they get Steam's small capsule (about 231x87), downloaded once and served
// from /covers/steam/<appid>.jpg|png, so the browser never talks to Steam's CDN. Nothing here holds the SteamID.

const (
	maxImageBytes   = 512 << 10 // a capsule is a few KB; anything bigger is not one
	maxImageSide    = 1200
	imageRetryAfter = 7 * 24 * time.Hour // a game with no usable image is asked about again after this long
	imageSubdir     = "steam"
	maxImagesPerRun = 300
)

// imageHosts are the only hosts an image is downloaded from (Steam's own CDN names).
var imageHosts = map[string]bool{
	"shared.akamai.steamstatic.com":     true,
	"shared.fastly.steamstatic.com":     true,
	"shared.cloudflare.steamstatic.com": true,
	"cdn.akamai.steamstatic.com":        true,
	"cdn.fastly.steamstatic.com":        true,
	"cdn.cloudflare.steamstatic.com":    true,
	"steamcdn-a.akamaihd.net":           true,
}

// errBadImage: the image was refused (host, status, size or content); asking again will not help.
var errBadImage = errors.New("image refused")

// ImageCache keeps one small image per Steam app in Dir (the folder is created on the first download).
type ImageCache struct {
	Dir string

	client        *http.Client
	allowLoopback bool // tests only: a fake server on 127.0.0.1 stands for the CDN
}

// NewImageCache returns a cache that stores its files in dir.
func NewImageCache(dir string) *ImageCache {
	return &ImageCache{Dir: dir, client: &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // never bounce to another host
	}}
}

// ImageName is the file of an app under /covers/ ("steam/123.jpg"), or "" when the folder has none. dir is the cache folder.
func ImageName(dir string, appid int64) string {
	for _, ext := range []string{"jpg", "png"} {
		n := strconv.FormatInt(appid, 10) + "." + ext
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.Mode().IsRegular() {
			return imageSubdir + "/" + n
		}
	}
	return ""
}

// ImageSubdir is the folder inside the covers folder that holds the wishlist images.
func ImageSubdir(coverDir string) string { return filepath.Join(coverDir, imageSubdir) }

func (c *ImageCache) marker(appid int64) string {
	return filepath.Join(c.Dir, strconv.FormatInt(appid, 10)+".none")
}

// Need returns the apps that have no image yet and were not found imageless within the last week.
func (c *ImageCache) Need(appIDs []int64) []int64 {
	var out []int64
	for _, id := range appIDs {
		if ImageName(c.Dir, id) != "" {
			continue
		}
		if st, err := os.Stat(c.marker(id)); err == nil && time.Since(st.ModTime()) < imageRetryAfter {
			continue
		}
		out = append(out, id)
	}
	return out
}

// MarkNone records that an app has no usable image, so the next runs skip it for a week.
func (c *ImageCache) MarkNone(appid int64) {
	if os.MkdirAll(c.Dir, 0o755) == nil {
		_ = os.WriteFile(c.marker(appid), nil, 0o644)
	}
}

func (c *ImageCache) allowed(u *url.URL) bool {
	if c.allowLoopback && isLoopback(u.Hostname()) {
		return u.Scheme == "http" || u.Scheme == "https"
	}
	return u.Scheme == "https" && u.Port() == "" && u.User == nil && imageHosts[strings.ToLower(u.Hostname())]
}

// Fetch downloads raw into Dir/<appid>.jpg|png. The host must be on the allow-list, redirects are refused, the size is
// capped and the bytes must decode as a JPEG or PNG of a sane size. A refusal wraps errBadImage; any other error is the
// network's.
func (c *ImageCache) Fetch(ctx context.Context, appid int64, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !c.allowed(u) {
		return fmt.Errorf("%w: host not allowed", errBadImage)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: bad address", errBadImage)
	}
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("image host could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("image host answered %d", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", errBadImage, res.StatusCode)
	}
	if res.ContentLength > maxImageBytes {
		return fmt.Errorf("%w: too large", errBadImage)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil {
		return errors.New("image could not be read")
	}
	if len(data) > maxImageBytes {
		return fmt.Errorf("%w: too large", errBadImage)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data)) // the real type, whatever the server or the name says
	if err != nil || (format != "jpeg" && format != "png") {
		return fmt.Errorf("%w: not a JPEG or PNG", errBadImage)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		return fmt.Errorf("%w: odd size", errBadImage)
	}
	ext := "jpg"
	if format == "png" {
		ext = "png"
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	name := strconv.FormatInt(appid, 10) + "." + ext
	tmp := filepath.Join(c.Dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(c.Dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(c.marker(appid))
	return nil
}

// ImageSource is the optional part of a WishlistSource that brings the games' pictures into the local cache.
type ImageSource interface {
	// SyncImages downloads the images the cache does not have yet. It is best effort: errors only stop it.
	SyncImages(ctx context.Context, appIDs []int64)
}

// DefaultImageBase is where Steam serves the files named by GetItems assets (store_item_assets).
const DefaultImageBase = "https://shared.akamai.steamstatic.com/store_item_assets/"

var (
	assetFileRe   = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,100}$`)
	assetFormatRe = regexp.MustCompile(`^[A-Za-z0-9_./\-]*\$\{FILENAME\}[A-Za-z0-9_./\-]*(\?t=[0-9]{1,12})?$`)
)

// assetURLs builds the candidate image addresses of an item, best first (the small capsule fits a list row; the header is
// the fallback). Names come from Steam's answer and are checked here; the cache checks the host again.
func assetURLs(base string, it storeItem) []string {
	a := it.Assets
	if a == nil || !assetFormatRe.MatchString(a.URLFormat) {
		return nil
	}
	var out []string
	for _, f := range []string{a.SmallCapsule, a.Header, a.MainCapsule} {
		if !assetFileRe.MatchString(f) {
			continue
		}
		out = append(out, base+strings.ReplaceAll(a.URLFormat, "${FILENAME}", f))
	}
	return out
}

// Assets asks Steam (one request per MaxTitleBatch apps) for the image addresses of apps. An app Steam does not know, or
// that has no assets, is absent from the result.
func (c *Steam) Assets(ctx context.Context, appIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	for len(appIDs) > 0 {
		n := min(len(appIDs), MaxTitleBatch)
		items, err := c.getItems(ctx, appIDs[:n], map[string]any{"include_assets": true})
		appIDs = appIDs[n:]
		if err != nil {
			return out, err
		}
		base := c.ImageBase
		if base == "" {
			base = DefaultImageBase
		}
		for _, it := range items {
			if it.Success != 1 {
				continue
			}
			if urls := assetURLs(base, it); len(urls) > 0 {
				out[it.ID] = urls
			}
		}
	}
	return out, nil
}

// SyncImages implements ImageSource: the apps without an image (and not found imageless this week) are looked up in
// batches, then each gets one download. Several network failures in a row end the run; nothing is asked per game on every
// run, because a game with an image, or known to have none, is skipped.
func (c *Steam) SyncImages(ctx context.Context, appIDs []int64) {
	if c.Images == nil {
		return
	}
	need := c.Images.Need(appIDs)
	if len(need) > maxImagesPerRun {
		need = need[:maxImagesPerRun]
	}
	for len(need) > 0 {
		n := min(len(need), MaxTitleBatch)
		batch := need[:n]
		need = need[n:]
		assets, err := c.Assets(ctx, batch)
		if err != nil {
			return
		}
		fails := 0
		for _, id := range batch {
			urls := assets[id]
			if len(urls) == 0 {
				c.Images.MarkNone(id)
				continue
			}
			bad, netErr := 0, false
			got := false
			for _, u := range urls {
				err := c.Images.Fetch(ctx, id, u)
				if err == nil {
					got = true
					break
				}
				if errors.Is(err, errBadImage) {
					bad++
				} else {
					netErr = true
				}
			}
			switch {
			case got:
				fails = 0
			case ctx.Err() != nil:
				return
			case netErr:
				if fails++; fails >= 3 {
					return
				}
			case bad > 0:
				c.Images.MarkNone(id)
			}
		}
	}
}
