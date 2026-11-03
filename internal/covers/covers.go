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

	"github.com/Deinigu/DeltaBacklog/internal/shots"
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

func tooLarge(w, h int) bool { return tooLargeFor(w, h, maxPixels) }

func tooLargeFor(w, h int, limit int64) bool { return w <= 0 || h <= 0 || int64(w)*int64(h) > limit }

// Save decodes an image from r, scales it down and writes dir/<gameID>.jpg.
func Save(r io.Reader, dir string, gameID int64) (string, error) {
	return saveCover(r, dir, gameID, maxPixels)
}

func saveCover(r io.Reader, dir string, gameID int64, limit int64) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return saveCoverRaw(raw, dir, gameID, limit, 1)
}

// saveCoverRaw is saveCover for an image already in memory (no second copy is made) whose EXIF orientation is o
// (1 = upright): the picture is turned upright after it was scaled down, so only a small bitmap is rotated.
func saveCoverRaw(raw []byte, dir string, gameID int64, limit int64, o int) (string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode: %v", ErrCorrupt, err)
	}
	if tooLargeFor(cfg.Width, cfg.Height, limit) {
		return "", fmt.Errorf("%w (%dx%d)", ErrTooManyPixels, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode: %v", ErrCorrupt, err)
	}
	if o >= 2 {
		img = prescaleOrient(img, o, func(ow, oh int) float64 { return min(1, float64(MaxWidth)/float64(ow)) })
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

// prescaleOrient scales img down by the factor want(w, h) asks for (w x h is the size once upright), then turns it
// upright (EXIF orientation o), so the rotation only ever touches a small bitmap.
func prescaleOrient(img image.Image, o int, want func(w, h int) float64) image.Image {
	b := img.Bounds()
	ow, oh := b.Dx(), b.Dy()
	if o >= 5 {
		ow, oh = oh, ow
	}
	if f := want(ow, oh); f < 1 {
		w, h := max(1, int(float64(b.Dx())*f)), max(1, int(float64(b.Dy())*f))
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	return shots.Orient(img, o)
}
