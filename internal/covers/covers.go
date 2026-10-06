// Package covers downloads cover art once and stores a small JPEG so the Pi never hot-links CDNs.
package covers

import (
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

// Save decodes an image from r, scales it down and writes dir/<gameID>.jpg.
func Save(r io.Reader, dir string, gameID int64) (string, error) {
	img, _, err := image.Decode(r)
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
