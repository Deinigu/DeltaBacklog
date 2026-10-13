package covers

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// Posters are the portrait box art of the rankings: a centre-cropped 2:3 JPEG, at most PosterWidth wide, stored next
// to the covers as p<gameID>.jpg. They are never scaled up.
const (
	PosterWidth = 300
	posterQ     = 80
)

// ErrBadImage marks an image that will never make a poster (not an image, too large, too small): retrying cannot help.
var ErrBadImage = errors.New("bad image")

// StatusError is a download that did not answer 200.
type StatusError struct {
	URL  string
	Code int
	Text string
}

func (e *StatusError) Error() string { return fmt.Sprintf("GET %s: %s", e.URL, e.Text) }

// Permanent reports whether asking again will not help: a client error, except "request timeout" and "too many requests".
func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != 408 && e.Code != 429
}

// PosterName is the file name of a game's poster.
func PosterName(gameID int64) string { return fmt.Sprintf("p%d.jpg", gameID) }

// FetchPosterStrict downloads url (already host-checked), refusing redirects, and writes the poster.
func FetchPosterStrict(url, dir string, gameID int64) (string, error) {
	resp, err := strictClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", &StatusError{URL: url, Code: resp.StatusCode, Text: resp.Status}
	}
	return SavePoster(io.LimitReader(resp.Body, maxDownload), dir, gameID)
}

// CropPoster makes the poster from the game's existing wide cover file (no network): the last resort.
func CropPoster(coverPath, dir string, gameID int64) (string, error) {
	f, err := os.Open(coverPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return SavePoster(io.LimitReader(f, maxDownload), dir, gameID)
}

// SavePoster decodes an image, crops its centre to 2:3, scales it down to PosterWidth and writes dir/p<gameID>.jpg.
// It returns the file name. The same pixel limit as for covers applies, checked before the image is decoded.
func SavePoster(r io.Reader, dir string, gameID int64) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode: %v", ErrBadImage, err)
	}
	if tooLarge(cfg.Width, cfg.Height) {
		return "", fmt.Errorf("%w: too large (%dx%d)", ErrBadImage, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	raw = nil
	if err != nil {
		return "", fmt.Errorf("%w: decode: %v", ErrBadImage, err)
	}
	src := posterCrop(img.Bounds())
	if src.Dx() < 16 || src.Dy() < 16 {
		return "", fmt.Errorf("%w: too small", ErrBadImage)
	}
	w := src.Dx()
	if w > PosterWidth {
		w = PosterWidth
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, w*3/2))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, src, draw.Src, nil)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := PosterName(gameID)
	tmp := filepath.Join(dir, name+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if err := jpeg.Encode(f, dst, &jpeg.Options{Quality: posterQ}); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return name, os.Rename(tmp, filepath.Join(dir, name))
}

// posterCrop is the largest centred 2:3 rectangle inside b.
func posterCrop(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	if w*3 > h*2 { // wider than 2:3: cut the sides
		cw := h * 2 / 3
		x := b.Min.X + (w-cw)/2
		return image.Rect(x, b.Min.Y, x+cw, b.Max.Y)
	}
	ch := w * 3 / 2 // taller: cut top and bottom
	y := b.Min.Y + (h-ch)/2
	return image.Rect(b.Min.X, y, b.Max.X, y+ch)
}
