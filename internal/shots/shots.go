// Package shots handles screenshot files: content sniffing, a bounded decode for the thumbnail, the few EXIF
// fields worth reading, and the incremental copy used by the nightly backup.
package shots

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
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
	ThumbWidth   = 480
	ThumbHeight  = 960
	thumbQuality = 80
	// MaxPixels is a sanity bound on the declared size; the decoded-memory budget is what really limits decoding.
	MaxPixels = 250_000_000
	// MaxClientThumb is the largest accepted thumbnail made by the browser.
	MaxClientThumb = 512 << 10
)

var (
	ErrUnsupported = errors.New("not a JPEG, PNG or WebP image")
	ErrTooLarge    = errors.New("image has too many pixels")
	ErrBadThumb    = errors.New("the preview image is not a small JPEG")
)

// Info describes a stored screenshot as the viewer shows it (EXIF rotation already applied to the size).
type Info struct {
	Ext         string // "jpg", "png" or "webp"
	Width       int
	Height      int
	Taken       string // EXIF DateTimeOriginal as 2006-01-02T15:04:05 (no zone), or ""
	Orientation int    // EXIF orientation 1-8; 1 when absent
	DecodeBytes int64  // estimated peak memory of decoding the whole image (see DecodeCost)
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

// Inspect reads the type, size, EXIF and decode cost of the file at path without decoding the pixels.
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
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels { // int64: 32-bit ARM
		return Info{}, ErrTooLarge
	}
	info := Info{Ext: ext, Width: cfg.Width, Height: cfg.Height, Orientation: 1}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Info{}, err
	}
	info.DecodeBytes = DecodeCost(ext, cfg, f)
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

// DecodeCost estimates the peak memory image.Decode needs for a file: the decoded bitmap, plus for a progressive
// JPEG the coefficient blocks (4 bytes per sample), plus the final RGBA/CMYK image of a 4-component JPEG. r is the
// file positioned at its start (only a JPEG's headers are read).
func DecodeCost(ext string, cfg image.Config, r io.Reader) int64 {
	px := int64(cfg.Width) * int64(cfg.Height)
	switch ext {
	case "jpg":
		progressive, rgb, comps := jpegLayout(r)
		if len(comps) == 0 {
			return px * 12 // unknown layout: assume the worst realistic case
		}
		maxH, maxV := 1, 1
		for _, c := range comps {
			maxH, maxV = max(maxH, c[0]), max(maxV, c[1])
		}
		var samples int64
		for _, c := range comps {
			samples += px * int64(c[0]*c[1]) / int64(maxH*maxV)
		}
		cost := samples
		if len(comps) >= 4 || rgb { // the decoder builds a separate RGBA (or CMYK) image
			cost += 4 * px
		}
		if progressive {
			cost += 4 * samples
		}
		return cost
	case "png":
		switch cfg.ColorModel {
		case color.GrayModel:
			return px
		case color.Gray16Model:
			return px * 2
		case color.NRGBA64Model, color.RGBA64Model:
			return px * 8
		}
		if _, ok := cfg.ColorModel.(color.Palette); ok {
			return px
		}
		return px * 4
	}
	return px * 4 // WebP: YCbCr or NRGBA, plus alpha
}

// jpegLayout reads the markers up to the frame header: whether the JPEG is progressive, whether its three components
// are stored as RGB (Adobe transform 0, or the ids R, G, B: the decoder then converts to a new RGBA image) and the
// (horizontal, vertical) sampling factor of each component. It returns no components when it cannot tell.
func jpegLayout(r io.Reader) (progressive, rgb bool, comps [][2]int) {
	adobe := -1 // the Adobe APP14 transform byte, when present
	br := bufio.NewReader(r)
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return false, false, nil
	}
	for i := 0; i < 1000; i++ {
		b, err := br.ReadByte()
		if err != nil {
			return false, false, nil
		}
		if b != 0xFF {
			continue
		}
		m, err := br.ReadByte()
		for err == nil && m == 0xFF {
			m, err = br.ReadByte()
		}
		if err != nil {
			return false, false, nil
		}
		if m == 0 || m == 0x01 || (m >= 0xD0 && m <= 0xD8) {
			continue
		}
		var l [2]byte
		if _, err := io.ReadFull(br, l[:]); err != nil {
			return false, false, nil
		}
		n := int(l[0])<<8 | int(l[1]) - 2
		if n < 0 {
			return false, false, nil
		}
		if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
			buf := make([]byte, n)
			if _, err := io.ReadFull(br, buf); err != nil || n < 6 {
				return false, false, nil
			}
			nc := int(buf[5])
			if nc < 1 || nc > 4 || n < 6+3*nc {
				return false, false, nil
			}
			for c := 0; c < nc; c++ {
				hv := buf[7+3*c]
				hs, vs := int(hv>>4), int(hv&15)
				if hs < 1 || vs < 1 {
					return false, false, nil
				}
				comps = append(comps, [2]int{hs, vs})
			}
			rgb = nc == 3 && ((adobe == 0) || (adobe < 0 && buf[6] == 'R' && buf[9] == 'G' && buf[12] == 'B'))
			return m == 0xC2 || m == 0xC6 || m == 0xCA || m == 0xCE, rgb, comps
		}
		if m == 0xEE && n >= 12 && n <= 64 { // APP14 "Adobe": the colour transform byte tells RGB from YCbCr
			buf := make([]byte, n)
			if _, err := io.ReadFull(br, buf); err != nil {
				return false, false, nil
			}
			if string(buf[:5]) == "Adobe" {
				adobe = int(buf[11])
			}
			continue
		}
		if m == 0xDA { // start of scan before any frame header: give up
			return false, false, nil
		}
		if _, err := br.Discard(n); err != nil {
			return false, false, nil
		}
	}
	return false, false, nil
}

// decodeMu lets one image be decoded at a time: concurrent uploads must not add up to several decoded bitmaps
// on a machine with 160 MB.
var decodeMu sync.Mutex

// thumbPlan decides how a w x h picture (as stored, before the EXIF rotation) becomes a thumbnail: the centred part
// of the source to keep (cw x ch) and the size to draw it at (tw x th). The result fits in 480x960 once rotated;
// pictures taller than 1:2 or wider than 4:1 are centre-cropped to that ratio (the grid shows them with object-fit: cover).
func thumbPlan(w, h int, rotated bool) (cw, ch, tw, th int) {
	ow, oh := w, h
	if rotated {
		ow, oh = h, w
	}
	cw, ch = ow, oh
	if ch > 2*cw {
		ch = 2 * cw
	}
	if cw > 4*ch {
		cw = 4 * ch
	}
	tw, th = cw, ch
	if tw > ThumbWidth || th > ThumbHeight {
		f := min(float64(ThumbWidth)/float64(cw), float64(ThumbHeight)/float64(ch))
		tw, th = max(1, int(float64(cw)*f)), max(1, int(float64(ch)*f))
	}
	if rotated {
		cw, ch, tw, th = ch, cw, th, tw
	}
	return
}

// WriteThumb decodes src (already checked by Inspect and within the memory budget), draws a thumbnail that fits in
// 480x960 into a fresh small bitmap, applies the EXIF orientation and writes a JPEG to dst (through a temporary name).
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
	cw, ch, tw, th := thumbPlan(b.Dx(), b.Dy(), orientation >= 5)
	x0, y0 := b.Min.X+(b.Dx()-cw)/2, b.Min.Y+(b.Dy()-ch)/2
	small := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src) // transparent pixels go on black: JPEG has no alpha
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, image.Rect(x0, y0, x0+cw, y0+ch), draw.Over, nil)
	img = nil // the full-size bitmap is garbage from here on
	return writeJPEG(orient(small, orientation), dst)
}

func writeJPEG(img image.Image, dst string) error {
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: thumbQuality}); err != nil {
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

// WriteClientThumb stores a thumbnail the browser made (used when the original is too big to decode here). It must
// be a JPEG of at most MaxClientThumb bytes and 480x960 pixels, and it must decode, which is cheap at that size.
func WriteClientThumb(r io.Reader, dst string) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxClientThumb+1))
	if err != nil || len(data) > MaxClientThumb {
		return ErrBadThumb
	}
	if ext, ok := Sniff(data); !ok || ext != "jpg" {
		return ErrBadThumb
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > ThumbWidth || cfg.Height > ThumbHeight {
		return ErrBadThumb
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		return ErrBadThumb
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
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

// SyncResult is what SyncNew did.
type SyncResult struct {
	Copied  int   // files copied
	Skipped int   // files left because copying them would have crossed the free-space margin
	Bytes   int64 // bytes copied
}

// freeBytes is the free space of the filesystem holding path (it may not exist yet: the nearest existing parent is
// used); ok is false where that cannot be known. A variable so tests can fake it.
var freeBytes = FreeBytes

// SyncNew copies every regular file under src that is missing from dst (same relative path), keeping the folder
// layout. Files already in dst are never touched or removed, even when they are gone from src: the backup keeps
// screenshots the app has since deleted. Each copy goes through a temporary name, so an interrupted run leaves no
// half file that looks complete. When minFree > 0 the free space of dst is checked before every file and the run
// stops cleanly (the rest counts as Skipped) as soon as the next file would leave less than minFree bytes. A missing
// src is not an error (nothing to do).
func SyncNew(src, dst string, minFree int64) (SyncResult, error) {
	var res SyncResult
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
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
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if minFree > 0 {
			if free, ok := freeBytes(dst); ok && int64(free)-fi.Size() < minFree {
				res.Skipped++
				return nil // keep counting what is left; smaller files may still fit
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(p, target); err != nil {
			return err
		}
		res.Copied++
		res.Bytes += fi.Size()
		return nil
	})
	return res, err
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
