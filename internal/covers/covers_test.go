package covers

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTooLarge(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want bool
	}{
		{1920, 1080, false},
		{4000, 4000, false},  // exactly the cap
		{4001, 4000, true},   // one column over
		{65535, 65535, true}, // a header claiming 4.3 billion pixels
		{46341, 46341, true}, // 2,147,488,281: overflows a 32-bit int (the Pi) and would wrap negative
		{0, 100, true},
		{100, -5, true},
	} {
		if got := tooLarge(c.w, c.h); got != c.want {
			t.Errorf("tooLarge(%d, %d) = %v, want %v", c.w, c.h, got, c.want)
		}
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSaveShrinksAndRejects(t *testing.T) {
	dir := t.TempDir()
	name, err := Save(bytes.NewReader(pngBytes(t, 1920, 620)), dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width != MaxWidth || cfg.Height != 155 {
		t.Errorf("stored image %+v %v, want %dx155", cfg, err, MaxWidth)
	}
	if _, err := Save(bytes.NewReader(pngBytes(t, 4500, 4500)), dir, 8); err == nil {
		t.Error("a 20 megapixel image must be rejected")
	}
	if _, err := Save(bytes.NewReader([]byte("not an image")), dir, 9); err == nil {
		t.Error("garbage must be rejected")
	}
}
