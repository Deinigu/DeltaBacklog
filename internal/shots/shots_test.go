package shots

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
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
	n, err := SyncNew(src, dst)
	if err != nil || n != 3 {
		t.Fatalf("first run: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "2", "up-123.tmp")); err == nil {
		t.Error("temp file copied")
	}
	if n, _ := SyncNew(src, dst); n != 0 {
		t.Errorf("second run copied %d", n)
	}
	// a changed copy in dst is not overwritten; a new file is added; a file deleted in src stays in dst
	write(dst, "1/a.png", "KEEP")
	write(src, "3/c.png", "C")
	os.Remove(filepath.Join(src, "2", "b.png"))
	if n, err := SyncNew(src, dst); err != nil || n != 1 {
		t.Errorf("third run: %d %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "1", "a.png")); string(b) != "KEEP" {
		t.Errorf("existing file overwritten: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "2", "b.png")); err != nil {
		t.Error("deleted file removed from the backup")
	}
	// no source folder (storage absent) is not an error and copies nothing
	if n, err := SyncNew(filepath.Join(src, "nope"), dst); n != 0 || err != nil {
		t.Errorf("missing source: %d %v", n, err)
	}
}
