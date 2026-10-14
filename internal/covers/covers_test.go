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

func TestPosterIsACentredPortraitCrop(t *testing.T) {
	dir := t.TempDir()
	// a wide cover: cut to 2:3 from the middle, never upscaled
	name, err := SavePoster(bytes.NewReader(pngBytes(t, 460, 215)), dir, 7)
	if err != nil || name != "p7.jpg" {
		t.Fatalf("%q %v", name, err)
	}
	f, _ := os.Open(filepath.Join(dir, name))
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 143 || cfg.Height != 214 { // 215 * 2/3 = 143 wide
		t.Errorf("wide cover -> %dx%d, %v", cfg.Width, cfg.Height, err)
	}
	// a big portrait: scaled to 300 wide, exactly 2:3
	if _, err := SavePoster(bytes.NewReader(pngBytes(t, 600, 900)), dir, 8); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Open(filepath.Join(dir, "p8.jpg"))
	cfg, _, _ = image.DecodeConfig(f)
	f.Close()
	if cfg.Width != PosterWidth || cfg.Height != 450 {
		t.Errorf("portrait -> %dx%d", cfg.Width, cfg.Height)
	}
	// a tall image is cut at the top and bottom
	if _, err := SavePoster(bytes.NewReader(pngBytes(t, 200, 600)), dir, 9); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Open(filepath.Join(dir, "p9.jpg"))
	cfg, _, _ = image.DecodeConfig(f)
	f.Close()
	if cfg.Width != 200 || cfg.Height != 300 {
		t.Errorf("tall -> %dx%d", cfg.Width, cfg.Height)
	}
	// limits: a header that claims too many pixels and a thumbnail-sized image are refused, junk is not an image
	if _, err := SavePoster(bytes.NewReader(pngBytes(t, 5000, 4000)), dir, 10); err == nil {
		t.Error("an image over the pixel cap must be refused")
	}
	if _, err := SavePoster(bytes.NewReader(pngBytes(t, 8, 8)), dir, 11); err == nil {
		t.Error("a tiny image must be refused")
	}
	if _, err := SavePoster(bytes.NewReader([]byte("not an image")), dir, 12); err == nil {
		t.Error("junk must be refused")
	}
	for _, id := range []int{10, 11, 12} {
		if _, err := os.Stat(filepath.Join(dir, PosterName(int64(id)))); err == nil {
			t.Errorf("poster %d was written", id)
		}
	}
	// the crop of an existing cover file
	if err := os.WriteFile(filepath.Join(dir, "5.jpg"), pngBytes(t, 480, 225), 0o644); err != nil {
		t.Fatal(err)
	}
	if name, err := CropPoster(filepath.Join(dir, "5.jpg"), dir, 5); err != nil || name != "p5.jpg" {
		t.Errorf("CropPoster: %q %v", name, err)
	}
}
