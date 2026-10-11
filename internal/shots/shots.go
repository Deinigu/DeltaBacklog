// Package shots handles screenshot files: content sniffing, a bounded decode for the thumbnail, the few EXIF
// fields worth reading, and the incremental copy used by the nightly backup.
package shots

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	ThumbWidth     = 480
	thumbQuality   = 80
	MaxPixelsJPEG  = 40_000_000 // a JPEG decodes to about 1.5 bytes per pixel (YCbCr)
	MaxPixelsOther = 12_000_000 // PNG and WebP decode to 4 bytes per pixel
)

var (
	ErrUnsupported = errors.New("not a JPEG, PNG or WebP image")
	ErrTooLarge    = errors.New("image has too many pixels")
)

// Info describes a stored screenshot as the viewer shows it (EXIF rotation already applied to the size).
type Info struct {
	Ext         string // "jpg", "png" or "webp"
	Width       int
	Height      int
	Taken       string // EXIF DateTimeOriginal as 2006-01-02T15:04:05 (no zone), or ""
	Orientation int    // EXIF orientation 1-8; 1 when absent
}

// SniffLen is how many leading bytes Sniff needs.
const SniffLen = 12

// Sniff decides the type from the first bytes (never from a name or a Content-Type the client sent). SVG, HTML,
// GIF and everything else are refused.
func Sniff(head []byte) (string, bool) {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpg", true
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png", true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp", true
	}
	return "", false
}

// NewName returns a random 32-hex-digit base name. Stored names are never derived from what the client sent.
func NewName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// NameRE matches the file names this package creates: the original (<hex>.<ext>) and its thumbnail (<hex>_t.jpg).
var NameRE = regexp.MustCompile(`^[0-9a-f]{32}(_t\.jpg|\.(jpg|png|webp))$`)

// ThumbName is the thumbnail file name of a stored original base name.
func ThumbName(base string) string { return base + "_t.jpg" }

// Inspect reads the type, size and EXIF of the file at path without decoding the pixels.
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	head := make([]byte, SniffLen)
	if _, err := io.ReadFull(f, head); err != nil {
		return Info{}, ErrUnsupported
	}
	ext, ok := Sniff(head)
	if !ok {
		return Info{}, ErrUnsupported
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Info{}, err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return Info{}, fmt.Errorf("unreadable image: %w", err)
	}
	limit := int64(MaxPixelsOther)
	if ext == "jpg" {
		limit = MaxPixelsJPEG
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > limit { // int64: 32-bit ARM
		return Info{}, ErrTooLarge
	}
	info := Info{Ext: ext, Width: cfg.Width, Height: cfg.Height, Orientation: 1}
	if ext == "jpg" {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			info.Orientation, info.Taken = readEXIF(f)
		}
		if info.Orientation >= 5 {
			info.Width, info.Height = info.Height, info.Width
		}
	}
	return info, nil
}

// decodeMu lets one image be decoded at a time: concurrent uploads must not add up to several decoded bitmaps
// on a machine with 160 MB.
var decodeMu sync.Mutex

// WriteThumb decodes src (already checked by Inspect), scales it to ThumbWidth, applies the EXIF orientation and
// writes a JPEG to dst (through a temporary name).
func WriteThumb(src, dst string, orientation int) error {
	decodeMu.Lock()
	defer decodeMu.Unlock()
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	small := image.Image(img)
	if b.Dx() > ThumbWidth {
		h := max(1, b.Dy()*ThumbWidth/b.Dx())
		scaled := image.NewRGBA(image.Rect(0, 0, ThumbWidth, h))
		draw.BiLinear.Scale(scaled, scaled.Bounds(), img, b, draw.Src, nil)
		small = scaled
	}
	img = nil // the full-size bitmap is garbage from here on
	small = orient(small, orientation)
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(out, flatten(small), &jpeg.Options{Quality: thumbQuality}); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// flatten puts transparent pixels (PNG, WebP) on black: JPEG has no alpha.
func flatten(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, image.NewUniform(image.Black), image.Point{}, draw.Src)
	draw.Draw(dst, b, img, b.Min, draw.Over)
	return dst
}

// orient rotates or mirrors a (small) image so it shows upright. o is the EXIF orientation 1-8.
func orient(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if o >= 5 {
		nw, nh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// SyncNew copies every regular file under src that is missing from dst (same relative path), keeping the folder
// layout. Files already in dst are never touched or removed, even when they are gone from src: the backup keeps
// screenshots the app has since deleted. Each copy goes through a temporary name, so an interrupted run leaves no
// half file that looks complete. It returns how many files it copied. A missing src is not an error (nothing to do).
func SyncNew(src, dst string) (int, error) {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	copied := 0
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || strings.HasSuffix(p, ".tmp") || strings.HasSuffix(p, ".partial") || strings.HasPrefix(d.Name(), ".probe-") {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(p, target); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
