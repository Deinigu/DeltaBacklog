// Package covers downloads cover art once and stores a small JPEG so the Pi never hot-links CDNs.
package covers

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxWidth     = 480
	maxDownload  = 15 << 20
	jpegQuality  = 82
	fetchTimeout = 30 * time.Second
)

var client = &http.Client{Timeout: fetchTimeout}

// Fetch downloads url, scales it down to MaxWidth and writes dir/<gameID>.jpg.
// It returns the file name (relative to dir).
func Fetch(url, dir string, gameID int64) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return Save(io.LimitReader(resp.Body, maxDownload), dir, gameID)
}

// strictClient never follows redirects, so a URL that passed a host allow-list cannot bounce elsewhere.
var strictClient = &http.Client{
	Timeout:       fetchTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// FetchStrict is Fetch for URLs supplied by a client: it refuses redirects.
func FetchStrict(url, dir string, gameID int64) (string, error) {
	resp, err := strictClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return Save(io.LimitReader(resp.Body, maxDownload), dir, gameID)
}

// maxPixels bounds the memory a decode can use: a 15MB file can still describe a gigantic bitmap.
// 16 megapixels (4096x4096) is far more than any cover needs and about 64MB decoded. The product is computed in int64:
// on the Pi (32-bit ARM) a plain int overflows for a header that claims a huge image.
const maxPixels = 16_000_000

func tooLarge(w, h int) bool { return w <= 0 || h <= 0 || int64(w)*int64(h) > maxPixels }

// Save decodes an image from r, scales it down and writes dir/<gameID>.jpg.
func Save(r io.Reader, dir string, gameID int64) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if tooLarge(cfg.Width, cfg.Height) {
		return "", fmt.Errorf("image too large (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	if b.Dx() > MaxWidth {
		h := b.Dy() * MaxWidth / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, MaxWidth, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		img = dst
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d.jpg", gameID)
	tmp := filepath.Join(dir, name+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return name, os.Rename(tmp, filepath.Join(dir, name))
}
