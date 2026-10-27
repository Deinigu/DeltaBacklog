package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/importer"
)

type shotEnv struct {
	srv  *httptest.Server
	s    *Server
	root string
}

func newShotEnv(t *testing.T, configure func(*Server)) shotEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, _, err := importer.Import(d, strings.NewReader(csvData), importer.Options{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s := &Server{DB: d, CoverDir: t.TempDir(), ShotDir: root}
	if configure != nil {
		configure(s)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return shotEnv{srv, s, root}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w; i += 7 {
		img.Set(i, i%h, color.RGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// bombPNG claims a huge size in its header without carrying the pixels.
func bombPNG(w, h uint32) []byte {
	chunk := func(typ string, data []byte) []byte {
		var b bytes.Buffer
		binary.Write(&b, binary.BigEndian, uint32(len(data)))
		body := append([]byte(typ), data...)
		b.Write(body)
		binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(body))
		return b.Bytes()
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6
	return append(append([]byte("\x89PNG\r\n\x1a\n"), chunk("IHDR", ihdr)...), chunk("IEND", nil)...)
}

type upFile struct {
	name  string
	data  []byte
	mod   string
	thumb []byte // the browser-made preview, sent as a "thumb" part right after the file
}

func (e shotEnv) upload(t *testing.T, gameID string, files ...upFile) (int, []shotResult) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range files {
		if f.mod != "" {
			mw.WriteField("modified", f.mod)
		}
		fw, _ := mw.CreateFormFile("file", f.name)
		fw.Write(f.data)
		if f.thumb != nil {
			tw, _ := mw.CreateFormFile("thumb", "thumb.jpg")
			tw.Write(f.thumb)
		}
	}
	mw.Close()
	res, err := http.Post(e.srv.URL+"/api/games/"+gameID+"/screenshots", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Results []shotResult `json:"results"`
		Error   string       `json:"error"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	if out.Error != "" {
		return res.StatusCode, []shotResult{{Error: out.Error}}
	}
	return res.StatusCode, out.Results
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestUploadStoresOriginalAndThumbnail(t *testing.T) {
	e := newShotEnv(t, nil)
	orig := pngBytes(t, 1200, 800)
	code, res := e.upload(t, "1", upFile{"../../evil name.png", orig, "1700000000000", nil}, upFile{"p.jpg", jpegBytes(t, 640, 360), "", nil})
	if code != 200 || len(res) != 2 || res[0].Screenshot == nil || res[1].Screenshot == nil {
		t.Fatalf("%d %+v", code, res)
	}
	a := res[0].Screenshot
	if a.Width != 1200 || a.Height != 800 || a.Pos != 1 || res[1].Screenshot.Pos != 2 || a.Taken != "2023-11-14T22:13:20Z" {
		t.Errorf("%+v", a)
	}
	if strings.Contains(a.Src, "evil") || strings.Contains(a.Src, "..") || res[0].Name != "evil name.png" {
		t.Errorf("client name leaked: %+v", res[0])
	}
	if !strings.HasPrefix(a.Src, "screenshots/1/") || !strings.HasPrefix(a.Thumb, "screenshots/1/") {
		t.Errorf("paths must be relative to the app (it may be served under a sub-path): %q %q", a.Src, a.Thumb)
	}
	// the original is stored byte for byte
	got, err := http.Get(e.srv.URL + "/" + a.Src)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(b, orig) || got.Header.Get("Content-Type") != "image/png" || got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("original: %d bytes, %v", len(b), got.Header)
	}
	th, _ := http.Get(e.srv.URL + "/" + a.Thumb)
	cfg, err := jpeg.DecodeConfig(th.Body)
	th.Body.Close()
	if err != nil || cfg.Width != 480 || cfg.Height != 320 || !strings.Contains(th.Header.Get("Cache-Control"), "max-age=31536000") {
		t.Errorf("thumb %+v %v %v", cfg, err, th.Header)
	}
	dl, _ := http.Get(e.srv.URL + "/" + a.Src + "?download=1")
	dl.Body.Close()
	if !strings.HasPrefix(dl.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("no download header: %v", dl.Header)
	}
	if n := len(files(t, e.root)); n != 4 {
		t.Errorf("%d files on disk, want 4 (no temp files left)", n)
	}
	// listing
	lr, _ := http.Get(e.srv.URL + "/api/games/1/screenshots")
	var list struct {
		Screenshots []Screenshot `json:"screenshots"`
		Available   bool         `json:"available"`
	}
	json.NewDecoder(lr.Body).Decode(&list)
	lr.Body.Close()
	if len(list.Screenshots) != 2 || !list.Available {
		t.Errorf("%+v", list)
	}
}

func TestUploadRejectsWhatIsNotAnImage(t *testing.T) {
	e := newShotEnv(t, nil)
	for name, data := range map[string][]byte{
		"a.png":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"b.png":    []byte("<html><script>alert(1)</script></html>"),
		"c.png":    []byte("GIF89a......."),
		"d.png":    {},
		"e.png":    append([]byte("\x89PNG\r\n\x1a\n"), "garbage garbage"...), // right magic, broken body
		"f.webp":   []byte("RIFF....WEBPjunkjunk"),
		"g.jpg":    {0xFF, 0xD8, 0xFF, 0xE0},
		"h.svg":    []byte("<?xml version='1.0'?><svg/>"),
		"bomb.png": bombPNG(30000, 30000),
	} {
		code, res := e.upload(t, "1", upFile{name, data, "", nil})
		if code != 200 || len(res) != 1 || res[0].Error == "" || res[0].Screenshot != nil {
			t.Errorf("%s: %d %+v", name, code, res)
		}
	}
	if n := len(files(t, e.root)); n != 0 {
		t.Errorf("%d files left behind: %v", n, files(t, e.root))
	}
	var n int
	e.s.DB.QueryRow("SELECT count(*) FROM screenshot").Scan(&n)
	if n != 0 {
		t.Errorf("%d rows", n)
	}
	// one bad file does not stop a good one
	_, res := e.upload(t, "1", upFile{"x.svg", []byte("<svg/>"), "", nil}, upFile{"ok.png", pngBytes(t, 50, 50), "", nil})
	if len(res) != 2 || res[0].Error == "" || res[1].Screenshot == nil {
		t.Errorf("%+v", res)
	}
}

func TestUploadLimits(t *testing.T) {
	e := newShotEnv(t, func(s *Server) { s.ShotMaxFile = 20 << 10; s.ShotMaxRequest = 100 << 10 })
	big := pngBytes(t, 900, 900) // random-ish content is not needed: only the size matters
	big = append(big, bytes.Repeat([]byte{0}, 30<<10)...)
	_, res := e.upload(t, "1", upFile{"big.png", big, "", nil})
	if len(res) != 1 || !strings.Contains(res[0].Error, "larger than") {
		t.Errorf("per-file limit: %+v", res)
	}
	code, res := e.upload(t, "1", upFile{"a.png", bytes.Repeat([]byte{1}, 60<<10), "", nil}, upFile{"b.png", bytes.Repeat([]byte{1}, 60<<10), "", nil})
	if code != http.StatusOK || len(res) != 3 || !strings.Contains(res[2].Error, "larger than") || res[2].Name != "" { // one error entry for the cut-off file, not two
		t.Errorf("per-request limit: status %d %+v", code, res)
	}
	if n := len(files(t, e.root)); n != 0 {
		t.Errorf("%d files left behind: %v", n, files(t, e.root))
	}
}

func TestUploadUnknownGameAndBadBody(t *testing.T) {
	e := newShotEnv(t, nil)
	if code, _ := e.upload(t, "999", upFile{"a.png", pngBytes(t, 10, 10), "", nil}); code != 404 {
		t.Errorf("unknown game: %d", code)
	}
	res, _ := http.Post(e.srv.URL+"/api/games/1/screenshots", "application/json", strings.NewReader("{}"))
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("not multipart: %d", res.StatusCode)
	}
}

func TestServeOnlyGeneratedNames(t *testing.T) {
	e := newShotEnv(t, nil)
	os.WriteFile(filepath.Join(e.root, "secret.txt"), []byte("secret"), 0o644)
	os.MkdirAll(filepath.Join(e.root, "1"), 0o755)
	os.WriteFile(filepath.Join(e.root, "1", "notes.txt"), []byte("x"), 0o644)
	for _, p := range []string{"/screenshots/1/notes.txt", "/screenshots/1/..%2fsecret.txt", "/screenshots/..%2f/secret.txt", "/screenshots/1/", "/screenshots/x/" + strings.Repeat("a", 32) + ".png", "/screenshots/1/" + strings.Repeat("a", 32) + ".png"} {
		res, err := http.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
}

func TestCaptionOrderDelete(t *testing.T) {
	e := newShotEnv(t, nil)
	_, res := e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), "", nil}, upFile{"b.png", pngBytes(t, 40, 40), "", nil}, upFile{"c.png", pngBytes(t, 40, 40), "", nil})
	ids := []int64{res[0].Screenshot.ID, res[1].Screenshot.ID, res[2].Screenshot.ID}
	do := func(method, path, body string) (int, []byte) {
		req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b
	}
	id := func(n int64) string { return "/api/screenshots/" + itoa(n) }
	if c, _ := do("PATCH", id(ids[0]), `{"caption":"  Boss fight  "}`); c != 200 {
		t.Fatalf("patch %d", c)
	}
	if c, _ := do("PATCH", id(ids[0]), `{"caption":"`+strings.Repeat("x", 501)+`"}`); c != 400 {
		t.Errorf("long caption %d", c)
	}
	if c, _ := do("PATCH", id(9999), `{"caption":"x"}`); c != 404 {
		t.Errorf("unknown %d", c)
	}
	if c, _ := do("PUT", "/api/games/1/screenshots/order", `[`+itoa(ids[2])+`,`+itoa(ids[0])+`,`+itoa(ids[1])+`]`); c != 200 {
		t.Fatalf("order %d", c)
	}
	for _, bad := range []string{`[1]`, `[` + itoa(ids[0]) + `,` + itoa(ids[0]) + `,` + itoa(ids[1]) + `]`, `[` + itoa(ids[0]) + `,` + itoa(ids[1]) + `,99999]`} {
		if c, _ := do("PUT", "/api/games/1/screenshots/order", bad); c != 400 {
			t.Errorf("bad order %s: %d", bad, c)
		}
	}
	list, _ := e.s.loadShots(1)
	if len(list) != 3 || list[0].ID != ids[2] || list[1].ID != ids[0] || list[1].Caption != "Boss fight" {
		t.Errorf("%+v", list)
	}
	if c, _ := do("DELETE", id(ids[0]), ""); c != 204 {
		t.Fatalf("delete %d", c)
	}
	if c, _ := do("DELETE", id(ids[0]), ""); c != 404 {
		t.Errorf("second delete %d", c)
	}
	if n := len(files(t, e.root)); n != 4 {
		t.Errorf("%d files after deleting one of three, want 4", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestDeletingAGameRemovesItsFiles(t *testing.T) {
	e := newShotEnv(t, nil)
	e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), "", nil})
	e.upload(t, "2", upFile{"a.png", pngBytes(t, 40, 40), "", nil})
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/games/1", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 204 {
		t.Fatalf("%v %v", res, err)
	}
	var n int
	e.s.DB.QueryRow("SELECT count(*) FROM screenshot").Scan(&n)
	if n != 1 {
		t.Errorf("%d rows left, want 1 (cascade)", n)
	}
	if _, err := os.Stat(filepath.Join(e.root, "1")); !os.IsNotExist(err) {
		t.Errorf("game 1 folder still there: %v", err)
	}
	if len(files(t, filepath.Join(e.root, "2"))) != 2 {
		t.Error("game 2 files touched")
	}
}

// A configured folder that is missing (the external drive is not mounted) is never created by the server: uploads
// fail with a clear message, the listing still works and says the storage is unavailable.
func TestMissingStorageNeverCreated(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "mnt", "drive", "screenshots")
	e := newShotEnv(t, func(s *Server) { s.ShotDir = gone })
	code, res := e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), "", nil})
	if code != 503 || len(res) != 1 || res[0].Error != "screenshot storage not available" {
		t.Errorf("%d %+v", code, res)
	}
	if _, err := os.Stat(filepath.Dir(gone)); !os.IsNotExist(err) {
		t.Errorf("the server created %s", filepath.Dir(gone))
	}
	lr, _ := http.Get(e.srv.URL + "/api/games/1/screenshots")
	var list struct {
		Screenshots []Screenshot `json:"screenshots"`
		Available   bool         `json:"available"`
	}
	json.NewDecoder(lr.Body).Decode(&list)
	lr.Body.Close()
	if lr.StatusCode != 200 || list.Available {
		t.Errorf("listing: %d %+v", lr.StatusCode, list)
	}
	// deleting a game still works without the storage
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/games/1", nil)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 204 {
		t.Errorf("delete game: %v %v", res, err)
	}
	// the storage appears (drive mounted): uploads work, per-game folders are created
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, res := e.upload(t, "2", upFile{"a.png", pngBytes(t, 40, 40), "", nil}); code != 200 || res[0].Screenshot == nil {
		t.Errorf("after mount: %d %+v", code, res)
	}
}

func TestStorageIsAFileNotAFolder(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, []byte("x"), 0o644)
	e := newShotEnv(t, func(s *Server) { s.ShotDir = f })
	if code, _ := e.upload(t, "1", upFile{"a.png", pngBytes(t, 10, 10), "", nil}); code != 503 {
		t.Errorf("%d", code)
	}
}

// craftedJPEG is a header-only progressive 4:2:0 JPEG of 7000x5700 pixels (40 MP): the size claim is all the server
// reads before deciding; decoding it for real would need about 285 MB.
func craftedBigJPEG() []byte {
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	b = append(b, 0xFF, 0xC2, 0, 17, 8, 5700>>8, 5700&255, 7000>>8, 7000&255, 3, 1, 0x22, 0, 2, 0x11, 0, 3, 0x11, 0)
	return append(b, 0xFF, 0xDA, 0, 2)
}

func TestOverBudgetNeedsTheBrowsersThumbnail(t *testing.T) {
	e := newShotEnv(t, nil) // default budget 48 MB
	big := craftedBigJPEG()

	// the heap must stay far under what decoding would take while the server handles it
	var peak uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			time.Sleep(time.Millisecond)
		}
	}()

	code, res := e.upload(t, "1", upFile{name: "huge.jpg", data: big})
	if code != 200 || len(res) != 1 || res[0].Screenshot != nil || !strings.Contains(res[0].Error, "too big") || !res[0].NeedsThumb || res[0].Width != 7000 || res[0].Height != 5700 {
		t.Errorf("without a thumbnail: %d %+v", code, res)
	}
	if n := len(files(t, e.root)); n != 0 {
		t.Errorf("files left behind: %v", files(t, e.root))
	}

	// a thumbnail that is not a small JPEG does not help
	_, res = e.upload(t, "1", upFile{name: "huge.jpg", data: big, thumb: jpegBytes(t, 2000, 1000)})
	if res[0].Screenshot != nil || res[0].Error == "" || len(files(t, e.root)) != 0 {
		t.Errorf("oversized thumbnail accepted: %+v %v", res, files(t, e.root))
	}

	// with a valid thumbnail the original is stored untouched
	_, res = e.upload(t, "1", upFile{name: "huge.jpg", data: big, thumb: jpegBytes(t, 480, 270)}, upFile{name: "ok.png", data: pngBytes(t, 50, 50), thumb: jpegBytes(t, 10, 10)})
	close(stop)
	<-done
	if len(res) != 2 || res[0].Screenshot == nil || res[0].Name != "huge.jpg" || res[1].Screenshot == nil {
		t.Fatalf("with a thumbnail: %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(e.root, "1", res[0].Screenshot.Src[strings.LastIndex(res[0].Screenshot.Src, "/")+1:]))
	if err != nil || !bytes.Equal(got, big) {
		t.Errorf("the original was changed or missing: %v", err)
	}
	if res[0].Screenshot.Width != 7000 || res[0].Screenshot.Height != 5700 {
		t.Errorf("size %dx%d", res[0].Screenshot.Width, res[0].Screenshot.Height)
	}
	// the small picture was thumbnailed by the server: the browser's 10x10 thumb was ignored
	tf, _ := os.ReadFile(filepath.Join(e.root, "1", strings.TrimPrefix(res[1].Screenshot.Thumb, "screenshots/1/")))
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(tf)); err != nil || cfg.Width != 50 {
		t.Errorf("server thumbnail: %+v %v", cfg, err)
	}
	if peak > base.HeapAlloc+48<<20 {
		t.Errorf("heap grew by %d MB while handling the big picture", (peak-base.HeapAlloc)>>20)
	}
}

func TestThumbnailOfATallImageFits(t *testing.T) {
	e := newShotEnv(t, nil)
	_, res := e.upload(t, "1", upFile{name: "long.png", data: pngBytes(t, 400, 20000)})
	if res[0].Screenshot == nil {
		t.Fatalf("%+v", res)
	}
	tf, _ := os.ReadFile(filepath.Join(e.root, "1", strings.TrimPrefix(res[0].Screenshot.Thumb, "screenshots/1/")))
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(tf))
	if err != nil || cfg.Width > 480 || cfg.Height > 960 {
		t.Errorf("thumbnail %+v %v", cfg, err)
	}
}

func TestTooManyPartsIs413AndStopsReading(t *testing.T) {
	e := newShotEnv(t, nil)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	small := pngBytes(t, 8, 8)
	for i := 0; i < maxShotFiles+5; i++ {
		fw, _ := mw.CreateFormFile("file", "a.png")
		fw.Write(small)
	}
	mw.Close()
	res, err := http.Post(e.srv.URL+"/api/games/1/screenshots", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Error   string       `json:"error"`
		Results []shotResult `json:"results"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusRequestEntityTooLarge || out.Error != "too many files in one request" || len(out.Results) != maxShotFiles {
		t.Errorf("%d %q %d results", res.StatusCode, out.Error, len(out.Results))
	}
	// the many non-file parts are capped too
	body.Reset()
	mw = multipart.NewWriter(&body)
	for i := 0; i < maxShotParts+1; i++ {
		mw.WriteField("modified", "1")
	}
	mw.Close()
	res2, _ := http.Post(e.srv.URL+"/api/games/1/screenshots", mw.FormDataContentType(), &body)
	res2.Body.Close()
	if res2.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("non-file parts: %d", res2.StatusCode)
	}
}

func TestFailedUploadLeavesNoFolder(t *testing.T) {
	e := newShotEnv(t, nil)
	e.upload(t, "1", upFile{name: "x.svg", data: []byte("<svg/>")})
	if _, err := os.Stat(filepath.Join(e.root, "1")); err == nil {
		t.Error("an empty game folder was left behind")
	}
}

func TestDeleteNeedsTheStorage(t *testing.T) {
	e := newShotEnv(t, nil)
	_, res := e.upload(t, "1", upFile{name: "a.png", data: pngBytes(t, 40, 40)})
	id := res[0].Screenshot.ID
	gone := e.root + "-gone"
	os.Rename(e.root, gone) // the drive goes away
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/screenshots/"+itoa(id), nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil || r.StatusCode != 503 {
		t.Fatalf("delete without storage: %v %v", r, err)
	}
	var n int
	e.s.DB.QueryRow("SELECT count(*) FROM screenshot WHERE id = ?", id).Scan(&n)
	if n != 1 {
		t.Error("the row was deleted although the files could not be")
	}
	os.Rename(gone, e.root) // back
	r, _ = http.DefaultClient.Do(req)
	if r.StatusCode != 204 || len(files(t, e.root)) != 0 {
		t.Errorf("delete with storage: %d %v", r.StatusCode, files(t, e.root))
	}
}

func TestSweepNeverDeletesAnOriginal(t *testing.T) {
	e := newShotEnv(t, nil)
	e.s.DB.Exec("UPDATE game SET added_at = '2020-01-01T00:00:00Z'") // the games existed long before these files
	old := time.Now().Add(-2 * time.Hour)
	age := func(p string, at time.Time) { os.Chtimes(p, at, at) }
	_, res := e.upload(t, "1", upFile{name: "a.png", data: pngBytes(t, 40, 40)})
	keepOrig := filepath.Join(e.root, "1", strings.TrimPrefix(res[0].Screenshot.Src, "screenshots/1/"))
	keepThumb := filepath.Join(e.root, "1", strings.TrimPrefix(res[0].Screenshot.Thumb, "screenshots/1/"))
	hex := strings.Repeat("a", 32)
	mk := func(rel string, data []byte, at time.Time) string {
		p := filepath.Join(e.root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, data, 0o644)
		age(p, at)
		return p
	}
	oldTmp := mk("1/up-1.tmp", []byte("x"), old)
	newTmp := mk("1/up-2.tmp", []byte("x"), time.Now())
	loneThumb := mk("1/"+hex+"_t.jpg", []byte("x"), old) // its original is gone
	// an original (and its thumbnail) the database lost, of a game that exists: re-adopted
	lostName := strings.Repeat("b", 32)
	lostOrig := mk("1/"+lostName+".png", pngBytes(t, 60, 30), old)
	lostThumb := mk("1/"+lostName+"_t.jpg", jpegBytes(t, 20, 10), old)
	// one still being uploaded (rename done, row not yet inserted)
	fresh := mk("1/"+strings.Repeat("c", 32)+".png", pngBytes(t, 10, 10), time.Now())
	// an original of a game this database does not know: moved, never deleted
	goneName := strings.Repeat("d", 32)
	goneOrig := mk("9999/"+goneName+".png", pngBytes(t, 10, 10), old)
	mk("9999/"+goneName+"_t.jpg", []byte("t"), old)
	// a file that is no image, in a game folder with a name that looks generated
	junk := mk("1/"+strings.Repeat("e", 32)+".jpg", []byte("not an image"), old)
	foreign := mk("notes.txt", []byte("x"), old)
	foreignInGame := mk("1/readme.txt", []byte("x"), old)

	r, err := e.s.SweepShots()
	if err != nil || r.Removed != 2 || r.Adopted != 1 || r.Quarantined != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, p := range []string{keepOrig, keepThumb, newTmp, fresh, foreign, foreignInGame, lostOrig, lostThumb} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should stay: %v", p, err)
		}
	}
	for _, p := range []string{oldTmp, loneThumb} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should be gone", p)
		}
	}
	for _, p := range []string{goneOrig, junk} { // nothing is deleted: moved to .orphans
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should have been moved", p)
		}
	}
	if b, err := os.ReadFile(filepath.Join(e.root, ".orphans", "9999", goneName+".png")); err != nil || len(b) == 0 {
		t.Errorf("the original of the unknown game was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".orphans", "9999", goneName+"_t.jpg")); err != nil {
		t.Error("its thumbnail was not kept")
	}
	if _, err := os.Stat(filepath.Join(e.root, ".orphans", "1", filepath.Base(junk))); err != nil {
		t.Error("the unreadable file was not kept")
	}
	if _, err := os.Stat(filepath.Join(e.root, "9999")); err == nil {
		t.Error("the empty folder of the unknown game should be removed")
	}
	// the adopted picture is listed again with its real size
	list, _ := e.s.loadShots(1)
	if len(list) != 2 || list[1].Width != 60 || list[1].Height != 30 || !strings.Contains(list[1].Src, lostName) {
		t.Errorf("after adopting: %+v", list)
	}
	// running it again changes nothing
	if r, _ := e.s.SweepShots(); r != (SweepResult{}) {
		t.Errorf("second sweep: %+v", r)
	}
	// no storage: the sweep does nothing
	e2 := newShotEnv(t, func(s *Server) { s.ShotDir = filepath.Join(t.TempDir(), "absent") })
	if r, err := e2.s.SweepShots(); r != (SweepResult{}) || err != nil {
		t.Error("swept without storage")
	}
}

// The documented restore: an older database next to a drive that holds newer pictures must not lose them.
func TestRestoredOlderDatabaseKeepsAndReadoptsNewerPictures(t *testing.T) {
	e := newShotEnv(t, nil)
	e.s.DB.Exec("UPDATE game SET added_at = '2020-01-01T00:00:00Z'")
	_, res := e.upload(t, "1", upFile{name: "after-backup.png", data: pngBytes(t, 80, 40)})
	sc := res[0].Screenshot
	// the database goes back to a state without that row
	if _, err := e.s.DB.Exec("DELETE FROM screenshot WHERE id = ?", sc.ID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range files(t, e.root) {
		os.Chtimes(p, old, old)
	}
	r, err := e.s.SweepShots()
	if err != nil || r.Adopted != 1 || r.Removed != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	list, _ := e.s.loadShots(1)
	if len(list) != 1 || list[0].Src != sc.Src || list[0].Width != 80 {
		t.Fatalf("not back: %+v", list)
	}
	for _, u := range []string{list[0].Src, list[0].Thumb} {
		if r, err := http.Get(e.srv.URL + "/" + u); err != nil || r.StatusCode != 200 {
			t.Errorf("%s: %v %v", u, r, err)
		}
	}
	// a thumbnail that is missing is made again
	os.Remove(filepath.Join(e.root, "1", strings.TrimPrefix(sc.Thumb, "screenshots/1/")))
	e.s.DB.Exec("DELETE FROM screenshot")
	for _, p := range files(t, e.root) {
		os.Chtimes(p, old, old)
	}
	if r, _ := e.s.SweepShots(); r.Adopted != 1 {
		t.Errorf("second restore: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(e.root, "1", strings.TrimPrefix(sc.Thumb, "screenshots/1/"))); err != nil {
		t.Error("the thumbnail was not made again")
	}
}

func TestSweepStopsOnADatabaseError(t *testing.T) {
	e := newShotEnv(t, nil)
	_, res := e.upload(t, "1", upFile{name: "a.png", data: pngBytes(t, 40, 40)})
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range files(t, e.root) {
		os.Chtimes(p, old, old)
	}
	e.s.DB.Close() // every query now fails
	r, err := e.s.SweepShots()
	if err == nil || r != (SweepResult{}) {
		t.Errorf("%+v %v", r, err)
	}
	if len(files(t, e.root)) != 2 || res[0].Screenshot == nil {
		t.Errorf("files touched after a database error: %v", files(t, e.root))
	}
}

// trackHeap samples the heap while a request runs; stop returns how far above the starting heap it went.
func trackHeap() (stop func() uint64) {
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-quit:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			time.Sleep(time.Millisecond)
		}
	}()
	return func() uint64 {
		close(quit)
		<-done
		return peak - min(peak, base.HeapAlloc)
	}
}

// Pictures that fit the budget are decoded, and must not need much more than the budget: the thumbnail scaler must not
// add a big scratch buffer of its own (a kernel scaler did: 148 MB for a 47 MP grayscale JPEG).
func TestWithinBudgetPicturesStayUnderTheBudgetInHeap(t *testing.T) {
	old := debug.SetGCPercent(20)
	defer debug.SetGCPercent(old)
	e := newShotEnv(t, nil)
	mk := func(img image.Image) []byte {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 50}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	gray := mk(image.NewGray(image.Rect(0, 0, 7000, 6700)))                                // 47 MP, 47 MB decoded
	edge := mk(image.NewYCbCr(image.Rect(0, 0, 7000, 4500), image.YCbCrSubsampleRatio420)) // 32 MP 4:2:0, 47 MB decoded
	runtime.GC()
	for name, data := range map[string][]byte{"47 MP grayscale": gray, "32 MP 4:2:0": edge} {
		stop := trackHeap()
		code, res := e.upload(t, "1", upFile{name: "big.jpg", data: data})
		grew := stop()
		if code != 200 || res[0].Screenshot == nil {
			t.Fatalf("%s: %d %+v", name, code, res)
		}
		if grew > 72<<20 { // the 48 MB budget plus the request and encoder
			t.Errorf("%s: the heap grew by %d MB", name, grew>>20)
		}
	}
}

// Game ids are reused after the newest game goes away. Pictures left on the drive by the earlier game with that id must
// not turn up on the new one: they are older than it, so they are moved away instead of adopted.
func TestSweepDoesNotAttachAnEarlierGamesPicturesToAReusedID(t *testing.T) {
	e := newShotEnv(t, nil)
	var maxID int64
	e.s.DB.QueryRow("SELECT max(id) FROM game").Scan(&maxID)
	newID := maxID + 1
	// backup, then a game and a picture, then a restore that forgets both: the files stay on the drive
	if _, err := e.s.DB.Exec("INSERT INTO game(id, title, title_sort, status_id) VALUES (?, 'Old new game', 'old new game', (SELECT min(id) FROM status))", newID); err != nil {
		t.Fatal(err)
	}
	_, res := e.upload(t, itoa(newID), upFile{name: "a.png", data: pngBytes(t, 40, 40)})
	if res[0].Screenshot == nil {
		t.Fatalf("%+v", res)
	}
	e.s.DB.Exec("DELETE FROM game WHERE id = ?", newID) // the restore (cascade removes the row)
	// a different game is added right away and gets the same id
	if _, err := e.s.DB.Exec("INSERT INTO game(id, title, title_sort, status_id) VALUES (?, 'A different new game', 'a different new game', (SELECT min(id) FROM status))", newID); err != nil {
		t.Fatal(err)
	}
	// the files, uploaded before that game existed, are older than it by more than the tolerance (a restart later)
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range files(t, e.root) {
		os.Chtimes(p, old, old)
	}
	r, err := e.s.SweepShots()
	if err != nil || r.Adopted != 0 || r.Quarantined != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if list, _ := e.s.loadShots(newID); len(list) != 0 {
		t.Errorf("the new game shows the old game's picture: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".orphans", itoa(newID))); err != nil {
		t.Errorf("the picture was not kept in .orphans: %v", err)
	}
}
