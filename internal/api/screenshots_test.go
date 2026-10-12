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
	"strconv"
	"strings"
	"testing"

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
	name string
	data []byte
	mod  string
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
	code, res := e.upload(t, "1", upFile{"../../evil name.png", orig, "1700000000000"}, upFile{"p.jpg", jpegBytes(t, 640, 360), ""})
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
	// the original is stored byte for byte
	got, err := http.Get(e.srv.URL + a.Src)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(b, orig) || got.Header.Get("Content-Type") != "image/png" || got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("original: %d bytes, %v", len(b), got.Header)
	}
	th, _ := http.Get(e.srv.URL + a.Thumb)
	cfg, err := jpeg.DecodeConfig(th.Body)
	th.Body.Close()
	if err != nil || cfg.Width != 480 || cfg.Height != 320 || !strings.Contains(th.Header.Get("Cache-Control"), "max-age=31536000") {
		t.Errorf("thumb %+v %v %v", cfg, err, th.Header)
	}
	dl, _ := http.Get(e.srv.URL + a.Src + "?download=1")
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
		code, res := e.upload(t, "1", upFile{name, data, ""})
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
	_, res := e.upload(t, "1", upFile{"x.svg", []byte("<svg/>"), ""}, upFile{"ok.png", pngBytes(t, 50, 50), ""})
	if len(res) != 2 || res[0].Error == "" || res[1].Screenshot == nil {
		t.Errorf("%+v", res)
	}
}

func TestUploadLimits(t *testing.T) {
	e := newShotEnv(t, func(s *Server) { s.ShotMaxFile = 20 << 10; s.ShotMaxRequest = 100 << 10 })
	big := pngBytes(t, 900, 900) // random-ish content is not needed: only the size matters
	big = append(big, bytes.Repeat([]byte{0}, 30<<10)...)
	_, res := e.upload(t, "1", upFile{"big.png", big, ""})
	if len(res) != 1 || !strings.Contains(res[0].Error, "larger than") {
		t.Errorf("per-file limit: %+v", res)
	}
	code, _ := e.upload(t, "1", upFile{"a.png", bytes.Repeat([]byte{1}, 60<<10), ""}, upFile{"b.png", bytes.Repeat([]byte{1}, 60<<10), ""})
	if code != http.StatusRequestEntityTooLarge {
		t.Errorf("per-request limit: status %d", code)
	}
	if n := len(files(t, e.root)); n != 0 {
		t.Errorf("%d files left behind: %v", n, files(t, e.root))
	}
}

func TestUploadUnknownGameAndBadBody(t *testing.T) {
	e := newShotEnv(t, nil)
	if code, _ := e.upload(t, "999", upFile{"a.png", pngBytes(t, 10, 10), ""}); code != 404 {
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
	_, res := e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), ""}, upFile{"b.png", pngBytes(t, 40, 40), ""}, upFile{"c.png", pngBytes(t, 40, 40), ""})
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
	e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), ""})
	e.upload(t, "2", upFile{"a.png", pngBytes(t, 40, 40), ""})
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
	code, res := e.upload(t, "1", upFile{"a.png", pngBytes(t, 40, 40), ""})
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
	if code, res := e.upload(t, "2", upFile{"a.png", pngBytes(t, 40, 40), ""}); code != 200 || res[0].Screenshot == nil {
		t.Errorf("after mount: %d %+v", code, res)
	}
}

func TestStorageIsAFileNotAFolder(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, []byte("x"), 0o644)
	e := newShotEnv(t, func(s *Server) { s.ShotDir = f })
	if code, _ := e.upload(t, "1", upFile{"a.png", pngBytes(t, 10, 10), ""}); code != 503 {
		t.Errorf("%d", code)
	}
}
