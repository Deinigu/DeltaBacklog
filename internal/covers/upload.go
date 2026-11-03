package covers

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"net/http"
)

// Uploads: an image the user sends from their own device. The limits protect the Pi: the body is capped by the caller
// (MaxUploadBytes), and the pixel size is read from the header before anything is decoded.
const (
	MaxUploadBytes  = 10 << 20
	MaxUploadPixels = 40_000_000 // 40 megapixels: a phone photo fits, a decompression bomb does not
)

var (
	// ErrUnsupportedType is an upload that is not a JPEG, PNG or WebP (judged by its bytes, never by its name).
	ErrUnsupportedType = errors.New("not a JPEG, PNG or WebP image")
	// ErrTooManyPixels is an image with more than MaxUploadPixels (or a header that claims nonsense).
	ErrTooManyPixels = errors.New("image has too many pixels")
	// ErrCorrupt is an image that cannot be decoded.
	ErrCorrupt = errors.New("the image is damaged")
)

// CheckUpload tells what is wrong with an uploaded file before it is decoded: wrong type, damaged header, too many pixels.
func CheckUpload(raw []byte) error {
	switch http.DetectContentType(raw) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return ErrUnsupportedType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if tooLargeFor(cfg.Width, cfg.Height, MaxUploadPixels) {
		return fmt.Errorf("%w (%dx%d)", ErrTooManyPixels, cfg.Width, cfg.Height)
	}
	return nil
}

// SaveUpload is Save for an uploaded file: the same resize and the same dir/<gameID>.jpg.
func SaveUpload(raw []byte, dir string, gameID int64) (string, error) {
	if err := CheckUpload(raw); err != nil {
		return "", err
	}
	return saveCover(bytes.NewReader(raw), dir, gameID, MaxUploadPixels)
}

// SavePosterUpload is SavePoster for an uploaded file: the same crop, size and dir/p<gameID>.jpg.
func SavePosterUpload(raw []byte, dir string, gameID int64) (string, error) {
	if err := CheckUpload(raw); err != nil {
		return "", err
	}
	return savePosterLimit(bytes.NewReader(raw), dir, PosterName(gameID), MaxUploadPixels)
}
