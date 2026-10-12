package shots

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSniff(t *testing.T) {
	for in, want := range map[string]string{
		"\xff\xd8\xff\xe0abcdefgh": "jpg", "\x89PNG\r\n\x1a\nabcd": "png", "RIFF\x00\x00\x00\x00WEBPVP8 ": "webp",
		"GIF89a......": "", "<svg xmlns=...>": "", "<!DOCTYPE html>": "", "": "", "\xff\xd8": "",
	} {
		if got, _ := Sniff([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestNameRE(t *testing.T) {
	good := NewName()
	for _, n := range []string{good + ".png", good + ".jpg", good + ".webp", ThumbName(good)} {
		if !NameRE.MatchString(n) {
			t.Errorf("%s should match", n)
		}
	}
	for _, n := range []string{"../" + good + ".png", good + ".svg", good + ".png/x", "x.png", good + ".PNG", good[:31] + ".png", ".." + good + ".png"} {
		if NameRE.MatchString(n) {
			t.Errorf("%s should not match", n)
		}
	}
}

// exifJPEG inserts an APP1 segment with the given orientation and DateTimeOriginal after the SOI of a JPEG.
func exifJPEG(t *testing.T, w, h, orientation int, bigEndian bool, date string) []byte {
	t.Helper()
	var jb bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{255, 0, 0, 255})
	}
	jpeg.Encode(&jb, img, nil)
	var bo binary.AppendByteOrder = binary.LittleEndian
	hdr := "II"
	if bigEndian {
		bo, hdr = binary.BigEndian, "MM"
	}
	tiff := []byte(hdr)
	tiff = bo.AppendUint16(tiff, 42)
	tiff = bo.AppendUint32(tiff, 8)
	// IFD0: orientation, exif pointer
	tiff = bo.AppendUint16(tiff, 2)
	ent := func(tag, typ uint16, count uint32, val func([]byte) []byte) {
		tiff = bo.AppendUint16(tiff, tag)
		tiff = bo.AppendUint16(tiff, typ)
		tiff = bo.AppendUint32(tiff, count)
		tiff = val(tiff)
	}
	ent(0x0112, 3, 1, func(b []byte) []byte { b = bo.AppendUint16(b, uint16(orientation)); return append(b, 0, 0) })
	ent(0x8769, 4, 1, func(b []byte) []byte { return bo.AppendUint32(b, 38) }) // the Exif IFD starts right after IFD0
	tiff = bo.AppendUint32(tiff, 0)                                            // next IFD
	// Exif IFD at that offset
	tiff = bo.AppendUint16(tiff, 1)
	ent(0x9003, 2, 20, func(b []byte) []byte { return bo.AppendUint32(b, 56) }) // the date follows the IFD (2 + 12 + 4 bytes)
	tiff = bo.AppendUint32(tiff, 0)
	tiff = append(tiff, []byte(date)...)
	tiff = append(tiff, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, jb.Bytes()[2:]...)
}

func TestInspectEXIF(t *testing.T) {
	for _, be := range []bool{false, true} {
		p := filepath.Join(t.TempDir(), "a.jpg")
		os.WriteFile(p, exifJPEG(t, 200, 100, 6, be, "2024:05:06 07:08:09"), 0o644)
		info, err := Inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Orientation != 6 || info.Width != 100 || info.Height != 200 || info.Taken != "2024-05-06T07:08:09" || info.Ext != "jpg" {
			t.Errorf("bigEndian=%v: %+v", be, info)
		}
		th := filepath.Join(t.TempDir(), "t.jpg")
		if err := WriteThumb(p, th, info.Orientation); err != nil {
			t.Fatal(err)
		}
		f, _ := os.Open(th)
		cfg, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width != 100 || cfg.Height != 200 {
			t.Errorf("rotated thumbnail %+v %v", cfg, err)
		}
	}
}

func TestInspectWithoutEXIFAndGarbage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 30, 20)), nil)
	p := filepath.Join(dir, "plain.jpg")
	os.WriteFile(p, b.Bytes(), 0o644)
	if info, err := Inspect(p); err != nil || info.Orientation != 1 || info.Taken != "" || info.Width != 30 {
		t.Errorf("%+v %v", info, err)
	}
	// truncated / hostile EXIF must not panic
	for n := 4; n < 60; n++ {
		d := exifJPEG(t, 20, 10, 3, false, "2024:05:06 07:08:09")
		os.WriteFile(filepath.Join(dir, "x.jpg"), d[:n], 0o644)
		Inspect(filepath.Join(dir, "x.jpg"))
	}
}

func TestSyncNewIsIncrementalAndKeepsDeleted(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write := func(root, rel, data string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(data), 0o644)
	}
	write(src, "1/a.png", "A")
	write(src, "1/a_t.jpg", "T")
	write(src, "2/b.png", "B")
	write(src, "2/up-123.tmp", "half")
	r, err := SyncNew(src, dst, 0)
	n := r.Copied
	if err != nil || n != 3 {
		t.Fatalf("first run: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "2", "up-123.tmp")); err == nil {
		t.Error("temp file copied")
	}
	if r, _ := SyncNew(src, dst, 0); r.Copied != 0 {
		t.Errorf("second run copied %d", r.Copied)
	}
	// a changed copy in dst is not overwritten; a new file is added; a file deleted in src stays in dst
	write(dst, "1/a.png", "KEEP")
	write(src, "3/c.png", "C")
	os.Remove(filepath.Join(src, "2", "b.png"))
	if r, err := SyncNew(src, dst, 0); err != nil || r.Copied != 1 {
		t.Errorf("third run: %d %v", r.Copied, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "1", "a.png")); string(b) != "KEEP" {
		t.Errorf("existing file overwritten: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "2", "b.png")); err != nil {
		t.Error("deleted file removed from the backup")
	}
	// no source folder (storage absent) is not an error and copies nothing
	if r, err := SyncNew(filepath.Join(src, "nope"), dst, 0); r.Copied != 0 || err != nil {
		t.Errorf("missing source: %d %v", r.Copied, err)
	}
}

func TestThumbFitsTheBoxAndCropsExtremeRatios(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ w, h, o, wantW, wantH int }{
		{400, 20000, 1, 400, 800},  // tall strip: centre-cropped to 1:2
		{1080, 20000, 1, 480, 960}, // phone "scrolling screenshot"
		{20000, 100, 1, 400, 100},  // wide strip: cropped to 4:1
		{3000, 2000, 1, 480, 320},  // ordinary
		{100, 50, 1, 100, 50},      // small stays small
		{2000, 4000, 6, 0, 0},      // rotated: only the box is checked
	} {
		img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "s.png")
		os.WriteFile(src, b.Bytes(), 0o644)
		dst := filepath.Join(dir, "t.jpg")
		if err := WriteThumb(src, dst, c.o); err != nil {
			t.Fatal(err)
		}
		f, _ := os.Open(dst)
		cfg, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width > ThumbWidth || cfg.Height > ThumbHeight {
			t.Errorf("%dx%d o=%d: thumbnail %dx%d (%v) is outside 480x960", c.w, c.h, c.o, cfg.Width, cfg.Height, err)
		}
		if c.o == 1 && (cfg.Width != c.wantW || cfg.Height != c.wantH) {
			t.Errorf("%dx%d: thumbnail %dx%d, want %dx%d", c.w, c.h, cfg.Width, cfg.Height, c.wantW, c.wantH)
		}
	}
}

// craftedJPEG is a header-only JPEG (JFIF + frame header) that DecodeConfig accepts: enough to test the memory estimate.
func craftedJPEG(w, h int, sof byte, comps [][2]int) []byte {
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	n := len(comps)
	b = append(b, 0xFF, sof, 0, byte(8+3*n), 8, byte(h>>8), byte(h), byte(w>>8), byte(w), byte(n))
	for i, c := range comps {
		b = append(b, byte(i+1), byte(c[0]<<4|c[1]), 0)
	}
	return append(b, 0xFF, 0xDA, 0, 2)
}

func TestDecodeCostMatchesMeasuredPeaks(t *testing.T) {
	const mp40 = 7000 * 5700
	yuv420 := [][2]int{{2, 2}, {1, 1}, {1, 1}}
	yuv444 := [][2]int{{1, 1}, {1, 1}, {1, 1}}
	for _, c := range []struct {
		name     string
		data     []byte
		min, max int64
	}{
		{"baseline 4:2:0", craftedJPEG(7000, 5700, 0xC0, yuv420), 55 << 20, 70 << 20},
		{"progressive 4:2:0", craftedJPEG(7000, 5700, 0xC2, yuv420), 280 << 20, 330 << 20},
		{"progressive 4:4:4", craftedJPEG(7000, 5700, 0xC2, yuv444), 560 << 20, 600 << 20},
		{"gray", craftedJPEG(7000, 5700, 0xC0, [][2]int{{1, 1}}), 35 << 20, 45 << 20},
	} {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(c.data))
		if err != nil || cfg.Width*cfg.Height != mp40 {
			t.Fatalf("%s: %+v %v", c.name, cfg, err)
		}
		if got := DecodeCost("jpg", cfg, bytes.NewReader(c.data)); got < c.min || got > c.max {
			t.Errorf("%s: cost %d MB, want %d-%d MB", c.name, got>>20, c.min>>20, c.max>>20)
		}
	}
	cfg := image.Config{Width: 3000, Height: 4000, ColorModel: color.NRGBA64Model}
	if got := DecodeCost("png", cfg, nil); got != 3000*4000*8 {
		t.Errorf("16-bit PNG: %d", got)
	}
	cfg.ColorModel = color.GrayModel
	if got := DecodeCost("png", cfg, nil); got != 3000*4000 {
		t.Errorf("gray PNG: %d", got)
	}
}

func TestWriteClientThumb(t *testing.T) {
	dir := t.TempDir()
	good := jpegOf(t, 480, 270)
	if err := WriteClientThumb(bytes.NewReader(good), filepath.Join(dir, "a_t.jpg")); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"too wide":    jpegOf(t, 481, 100),
		"too tall":    jpegOf(t, 100, 961),
		"png":         {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		"html":        []byte("<html><script>alert(1)</script></html>"),
		"truncated":   good[:len(good)/2],
		"empty":       nil,
		"over 512 KB": append(append([]byte{}, good...), bytes.Repeat([]byte{0}, MaxClientThumb)...),
	} {
		dst := filepath.Join(dir, "x_t.jpg")
		if err := WriteClientThumb(bytes.NewReader(data), dst); err == nil {
			t.Errorf("%s accepted", name)
		}
		if _, err := os.Stat(dst); err == nil {
			t.Errorf("%s left a file", name)
		}
	}
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The backup stops copying before the SD card drops under the margin, file by file.
func TestSyncNewKeepsFreeSpaceMargin(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, n := range []string{"1/a.png", "1/b.png", "1/c.png"} {
		os.MkdirAll(filepath.Join(src, filepath.Dir(n)), 0o755)
		os.WriteFile(filepath.Join(src, n), bytes.Repeat([]byte{1}, 100), 0o644)
	}
	// dst already holds a file that is gone from src: it is not part of what must be copied, but it must stay
	os.MkdirAll(filepath.Join(dst, "9"), 0o755)
	os.WriteFile(filepath.Join(dst, "9", "old.png"), []byte("old"), 0o644)
	old := freeBytes
	defer func() { freeBytes = old }()
	free := uint64(1000)
	freeBytes = func(string) (uint64, bool) { return free, true }
	// the disk reports 1000 free and each copy uses 100: with a 750 margin only 2 of the 3 files fit
	free = 1000
	freeBytes = func(p string) (uint64, bool) {
		var used int64
		filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() && filepath.Base(p) != "old.png" {
				used += 100
			}
			return nil
		})
		return uint64(1000 - used), true
	}
	r, err := SyncNew(src, dst, 750)
	if err != nil || r.Copied != 2 || r.Skipped != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "9", "old.png")); err != nil {
		t.Error("an old backup file was removed")
	}
	// unknown free space (not Linux): no limit
	freeBytes = func(string) (uint64, bool) { return 0, false }
	if r, _ := SyncNew(src, dst, 1<<40); r.Copied != 1 {
		t.Errorf("without a free-space reading: %+v", r)
	}
}

// An RGB JPEG (Adobe transform 0) is converted to a separate RGBA image by the decoder: 4 more bytes per pixel.
func TestDecodeCostCountsTheRGBConversion(t *testing.T) {
	yuv444 := [][2]int{{1, 1}, {1, 1}, {1, 1}}
	plain := craftedJPEG(4000, 4000, 0xC0, yuv444)
	// the same file with an Adobe APP14 segment before the frame header (transform byte 0: RGB, 1: YCbCr)
	adobe := func(transform byte) []byte {
		seg := []byte{0xFF, 0xEE, 0, 14, 'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, transform}
		return append(append(append([]byte{}, plain[:20]...), seg...), plain[20:]...)
	}
	cost := func(b []byte) int64 {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return DecodeCost("jpg", cfg, bytes.NewReader(b))
	}
	px := int64(4000 * 4000)
	if got := cost(plain); got != 3*px {
		t.Errorf("YCbCr: %d", got)
	}
	if got := cost(adobe(1)); got != 3*px {
		t.Errorf("Adobe YCbCr: %d", got)
	}
	if got := cost(adobe(0)); got != 7*px {
		t.Errorf("Adobe RGB: %d, want %d", got, 7*px)
	}
	// component ids R, G, B without an Adobe segment
	rgbIDs := append([]byte{}, plain...)
	for i, c := range []byte{'R', 'G', 'B'} {
		rgbIDs[len(rgbIDs)-4-9+3*i] = c
	}
	if got := cost(rgbIDs); got != 7*px {
		t.Errorf("RGB ids: %d", got)
	}
}
