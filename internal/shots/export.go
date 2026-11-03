package shots

import (
	"bytes"
	"image"
	"runtime"
)

// LockDecode takes the one decode lock of the program: screenshots and uploaded covers and posters all decode under
// it, so they never add up to several decoded bitmaps on a machine with 160 MB. The returned function runs a garbage
// collection (the next decode must not start while this bitmap is uncollected garbage) and releases the lock.
func LockDecode() (unlock func()) {
	decodeMu.Lock()
	return func() {
		runtime.GC()
		decodeMu.Unlock()
	}
}

// Orientation is the EXIF orientation (1-8, 1 when absent) of a JPEG held in memory; other formats give 1.
func Orientation(raw []byte) int {
	if ext, ok := Sniff(raw); !ok || ext != "jpg" {
		return 1
	}
	o, _ := readEXIF(bytes.NewReader(raw))
	return o
}

// Orient rotates or mirrors a (small) image so it shows upright; o is the EXIF orientation. See orient.
func Orient(img image.Image, o int) image.Image { return orient(img, o) }
