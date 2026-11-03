package covers

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"net/http"

	"github.com/Deinigu/DeltaBacklog/internal/shots"
)

// Uploads: an image the user sends from their own device. The limits protect the Pi: the body is capped by the caller
// (MaxUploadBytes), the pixel size is read from the header before anything is decoded, the memory a decode would need
// (shots.DecodeCost, the same estimate as for screenshots) must fit the budget, and decodes run one at a time under
// the lock screenshots use too.
const (
	MaxUploadBytes  = 10 << 20
	MaxUploadPixels = 40_000_000 // 40 megapixels: a hard ceiling for a header that claims nonsense
)

var (
	// ErrUnsupportedType is an upload that is not a JPEG, PNG or WebP (judged by its bytes, never by its name).
	ErrUnsupportedType = errors.New("not a JPEG, PNG or WebP image")
	// ErrTooManyPixels is an image with more than MaxUploadPixels (or a header that claims nonsense).
	ErrTooManyPixels = errors.New("image has too many pixels")
	// ErrTooCostly is an image that is within the pixel limit but needs more memory to decode than the budget allows.
	ErrTooCostly = errors.New("image needs too much memory to process here")
	// ErrCorrupt is an image that cannot be decoded.
	ErrCorrupt = errors.New("the image is damaged")
)

// CheckUpload tells what is wrong with an uploaded file before it is decoded: wrong type, damaged header, too many
// pixels, a decode that would not fit budget bytes. It returns the EXIF orientation (1 when none).
func CheckUpload(raw []byte, budget int64) (int, error) {
	switch http.DetectContentType(raw) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return 1, ErrUnsupportedType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 1, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if tooLargeFor(cfg.Width, cfg.Height, MaxUploadPixels) {
		return 1, fmt.Errorf("%w (%dx%d)", ErrTooManyPixels, cfg.Width, cfg.Height)
	}
	if ext, ok := shots.Sniff(raw); ok && budget > 0 {
		if cost := shots.DecodeCost(ext, cfg, bytes.NewReader(raw)); cost > budget {
			return 1, fmt.Errorf("%w (about %d MB, the limit is %d MB)", ErrTooCostly, cost>>20, budget>>20)
		}
	}
	return shots.Orientation(raw), nil
}

// SaveUpload is Save for an uploaded file: the same resize and the same dir/<gameID>.jpg, turned upright by its EXIF
// orientation. budget is the decode budget in bytes (0: none).
func SaveUpload(raw []byte, dir string, gameID int64, budget int64) (string, error) {
	o, err := CheckUpload(raw, budget)
	if err != nil {
		return "", err
	}
	unlock := shots.LockDecode()
	defer unlock()
	return saveCoverRaw(raw, dir, gameID, MaxUploadPixels, o)
}

// SavePosterUpload is SavePoster for an uploaded file: the same crop, size and dir/p<gameID>.jpg, turned upright.
func SavePosterUpload(raw []byte, dir string, gameID int64, budget int64) (string, error) {
	o, err := CheckUpload(raw, budget)
	if err != nil {
		return "", err
	}
	unlock := shots.LockDecode()
	defer unlock()
	return savePosterRaw(raw, dir, PosterName(gameID), MaxUploadPixels, o)
}
