package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/posters"
)

// a 1x1 lossless WebP
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func upPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, h/2, color.RGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func upJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func upload(t *testing.T, url, field, name string, data []byte) (*http.Response, []byte) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile(field, name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", url, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out bytes.Buffer
	out.ReadFrom(res.Body)
	return res, out.Bytes()
}

func coverRow(t *testing.T, e *addEnv, id int64) (file string, url *string, v string) {
	t.Helper()
	if err := e.db.QueryRow(`SELECT cover_file, cover_url, cover_updated_at FROM game WHERE id = ?`, id).Scan(&file, &url, &v); err != nil {
		t.Fatal(err)
	}
	return
}

func TestUploadCover(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	_, _, v0 := coverRow(t, e, id)

	webp, _ := base64.StdEncoding.DecodeString(tinyWebP)
	for _, c := range []struct {
		name string
		data []byte
	}{{"a.png", upPNG(1200, 600)}, {"b.jpg", upJPEG(900, 500)}, {"c.webp", webp}} {
		res, body := upload(t, url+"/cover/upload", "file", c.name, c.data)
		var g Game
		json.Unmarshal(body, &g)
		if res.StatusCode != 200 || g.Cover == nil {
			t.Fatalf("%s: %d %s", c.name, res.StatusCode, body)
		}
		file, curl, v := coverRow(t, e, id)
		if file != strconv.FormatInt(id, 10)+".jpg" || curl != nil || v == v0 {
			t.Errorf("%s: file=%q url=%v v=%q (was %q)", c.name, file, curl, v, v0)
		}
		v0 = v
		f, err := os.Open(filepath.Join(e.coverDir, file))
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || format != "jpeg" || cfg.Width > covers.MaxWidth {
			t.Errorf("%s stored as %s %dx%d (%v)", c.name, format, cfg.Width, cfg.Height, err)
		}
	}
	_, list := do(t, "GET", e.srv.URL+"/api/games", "")
	if !bytes.Contains(list, []byte(`"cover_v":"`+v0+`"`)) {
		t.Error("the cached game list does not show the new cover key")
	}
}

func TestUploadRefused(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	file0, _, v0 := coverRow(t, e, id)
	big := upPNG(7000, 7000) // 49 megapixels, a few hundred KB

	cases := []struct {
		name  string
		field string
		data  []byte
		want  int
	}{
		{"notes.png", "file", []byte("this is not an image at all"), 415},
		{"x.png", "file", append([]byte("GIF89a"), make([]byte, 100)...), 415}, // a type outside JPEG/PNG/WebP
		{"photo.jpg", "file", upPNG(10, 10)[:40], 422},                      // PNG, cut off in the header
		{"torn.jpg", "file", upJPEG(300, 300)[:700], 422},                   // a readable header, no data
		{"big.png", "file", big, 413},                                          // too many pixels
		{"huge.bin", "file", make([]byte, 11<<20), 413},                        // too many bytes
		{"empty.png", "file", nil, 400},
		{"x.png", "other", upPNG(10, 10), 400}, // no part called file
	}
	for _, c := range cases {
		for _, kind := range []string{"/cover/upload", "/poster/upload"} {
			res, body := upload(t, url+kind, c.field, c.name, c.data)
			if res.StatusCode != c.want {
				t.Errorf("%s %s = %d %s, want %d", kind, c.name, res.StatusCode, body, c.want)
			}
			var out struct {
				Error string `json:"error"`
			}
			json.Unmarshal(body, &out)
			if out.Error == "" {
				t.Errorf("%s %s: no error text", kind, c.name)
			}
		}
	}
	// a body that is not multipart
	if res, _ := do(t, "POST", url+"/cover/upload", `{"a":1}`); res.StatusCode != 400 {
		t.Errorf("not multipart = %d", res.StatusCode)
	}
	if res, _ := upload(t, e.srv.URL+"/api/games/9999/poster/upload", "file", "a.png", upPNG(50, 50)); res.StatusCode != 404 {
		t.Errorf("missing game = %d", res.StatusCode)
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a refused upload made a poster: %q", file)
	}
	if file, _, v := coverRow(t, e, id); file != file0 || v != v0 {
		t.Errorf("a refused upload changed the cover: %q %q", file, v)
	}
	// the name does not matter, the bytes do
	if res, b := upload(t, url+"/cover/upload", "file", "photo.txt", upJPEG(400, 300)); res.StatusCode != 200 {
		t.Errorf("a JPEG named .txt = %d %s", res.StatusCode, b)
	}
}

func TestUploadPoster(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)

	res, body := upload(t, url+"/poster/upload", "file", "mine.png", upPNG(800, 1000))
	var g Game
	json.Unmarshal(body, &g)
	if res.StatusCode != 200 || g.Poster == nil || g.PosterV == "" {
		t.Fatalf("upload poster: %d %s", res.StatusCode, body)
	}
	file, _, user, v1 := posterRow(t, e, id)
	if file != covers.PosterName(id) || user != 1 {
		t.Errorf("row = %q user=%d", file, user)
	}
	f, err := os.Open(filepath.Join(e.coverDir, file))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(f)
	f.Close()
	if cfg.Width != covers.PosterWidth || cfg.Height != covers.PosterWidth*3/2 {
		t.Errorf("poster is %dx%d, want a %d wide 2:3 image", cfg.Width, cfg.Height, covers.PosterWidth)
	}
	entries, _ := os.ReadDir(e.coverDir)
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".poster-") {
			t.Error("a scratch folder was left behind")
		}
	}

	// the user's: the background maker leaves it alone
	if _, err := e.server.posterQ().M.Make(context.Background(), id); err != posters.ErrUserPoster {
		t.Errorf("Make on an uploaded poster = %v", err)
	}
	// a second upload within the same second still gets a new cache key
	_, body = upload(t, url+"/poster/upload", "file", "mine.jpg", upJPEG(600, 900))
	json.Unmarshal(body, &g)
	if g.PosterV == v1 {
		t.Errorf("poster_v %q did not change", v1)
	}
}

func patchIGDB(t *testing.T, e *addEnv, url string, igdb int64) {
	t.Helper()
	if res, b := do(t, "PATCH", url, `{"igdb_id":`+strconv.FormatInt(igdb, 10)+`}`); res.StatusCode != 200 {
		t.Fatalf("patch igdb_id %d = %d %s", igdb, res.StatusCode, b)
	}
	e.server.bg.Wait() // the poster is fetched after the response
}

func TestRelinkRefreshesTheAutomaticPoster(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	e.meta.igdbCover = e.images.URL + "/portrait.png"

	// no poster yet: the new link brings its IGDB poster
	patchIGDB(t, e, url, 111)
	file, src, user, v1 := posterRow(t, e, id)
	if file != covers.PosterName(id) || src != "igdb" || user != 0 {
		t.Fatalf("after the first link: %q %q user=%d", file, src, user)
	}
	_, list := do(t, "GET", e.srv.URL+"/api/games", "")
	if !bytes.Contains(list, []byte(`"poster_v":"`+v1+`"`)) {
		t.Error("the game list does not show the new poster at once")
	}

	// another IGDB game: the poster is replaced and gets a new cache key
	patchIGDB(t, e, url, 222)
	if _, src, user, v2 := posterRow(t, e, id); v2 == v1 || src != "igdb" || user != 0 {
		t.Errorf("after relinking: src=%q user=%d v=%q (was %q)", src, user, v2, v1)
	}

	// IGDB has no cover for the new game: SteamGridDB, as in the usual pipeline
	e.meta.igdbCover = ""
	e.meta.portrait = e.images.URL + "/portrait.png"
	patchIGDB(t, e, url, 333)
	if _, src, _, _ := posterRow(t, e, id); src != "sgdb" {
		t.Errorf("fallback source = %q, want sgdb", src)
	}
	// ... and then the crop of the wide cover
	e.meta.portrait = ""
	patchIGDB(t, e, url, 334)
	if _, src, _, _ := posterRow(t, e, id); src != "crop" {
		t.Errorf("last fallback = %q, want crop", src)
	}

	// clearing the link leaves the poster alone
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	_, _, _, vc := posterRow(t, e, id)
	patchIGDB(t, e, url, 0)
	if _, _, _, v := posterRow(t, e, id); v != vc {
		t.Error("clearing the link changed the poster")
	}
}

func TestRelinkKeepsTheUsersPoster(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	e.meta.igdbCover = e.images.URL + "/portrait.png"

	upload(t, url+"/poster/upload", "file", "mine.png", upPNG(600, 900))
	_, _, _, v := posterRow(t, e, id)
	patchIGDB(t, e, url, 444)
	if _, src, user, v2 := posterRow(t, e, id); v2 != v || user != 1 || src == "igdb" {
		t.Errorf("the uploaded poster was replaced: src=%q user=%d", src, user)
	}
	// the same for a poster picked from a link
	do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	_, _, _, v = posterRow(t, e, id)
	patchIGDB(t, e, url, 555)
	if _, _, user, v2 := posterRow(t, e, id); v2 != v || user != 1 {
		t.Error("the chosen poster was replaced")
	}
}

func TestLinkAcceptRefreshesThePoster(t *testing.T) {
	e := newAddEnv(t)
	_, id := newPosterGame(t, e)
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	body := `{"game_id":` + strconv.FormatInt(id, 10) + `,"igdb_id":777}`
	if res, b := do(t, "POST", e.srv.URL+"/api/link/accept", body); res.StatusCode != 204 {
		t.Fatalf("accept = %d %s", res.StatusCode, b)
	}
	e.server.bg.Wait()
	if _, src, user, _ := posterRow(t, e, id); src != "igdb" || user != 0 {
		t.Errorf("after the Link screen: src=%q user=%d", src, user)
	}
}

// A lookup that was started for an old link must not write when the game has been linked again meanwhile.
func TestRelinkStaleWriteGuard(t *testing.T) {
	e := newAddEnv(t)
	_, id := newPosterGame(t, e)
	if _, err := e.db.Exec(`UPDATE game SET igdb_id = 10 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	m := &posters.Maker{DB: e.db, Dir: e.coverDir, Src: e.meta, Allowed: func(string) bool { return true },
		Fetch: func(u, dir string, gameID int64) (string, error) {
			// the owner links the game to another IGDB game while this download runs
			if _, err := e.db.Exec(`UPDATE game SET igdb_id = 11 WHERE id = ?`, gameID); err != nil {
				t.Error(err)
			}
			return covers.SavePoster(bytes.NewReader(upPNG(600, 900)), dir, gameID)
		}}
	if _, err := m.Relink(context.Background(), id, 10); err != posters.ErrStale {
		t.Fatalf("Relink = %v, want ErrStale", err)
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a stale lookup wrote the poster %q", file)
	}
	if _, err := os.Stat(filepath.Join(e.coverDir, covers.PosterName(id))); err == nil {
		t.Error("a stale lookup left an image file")
	}
	// started for a link that is already gone: no lookup at all
	if _, err := m.Relink(context.Background(), id, 10); err != posters.ErrStale {
		t.Errorf("Relink for an old link = %v", err)
	}
}

// exifJPEG makes a w x h JPEG (left half red, right half blue) that says it must be turned by orientation o.
func exifJPEG(w, h, o int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.Set(x, y, color.RGBA{255, 0, 0, 255})
			} else {
				img.Set(x, y, color.RGBA{0, 0, 255, 255})
			}
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	raw := b.Bytes()
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := append([]byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte((len(seg) + 2) & 0xFF)}, seg...)
	return append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
}

func storedSize(t *testing.T, path string) (w, h int, img image.Image) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err = jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy(), img
}

func redish(c color.Color) bool { r, _, b, _ := c.RGBA(); return r > b }

// A photo that says "turn me 90 degrees" is stored upright, as cover and as poster.
func TestUploadAppliesExifOrientation(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	src := exifJPEG(200, 100, 6) // landscape pixels, shown portrait: the left (red) side is the top

	if res, b := upload(t, url+"/cover/upload", "file", "p.jpg", src); res.StatusCode != 200 {
		t.Fatalf("cover = %d %s", res.StatusCode, b)
	}
	w, h, img := storedSize(t, filepath.Join(e.coverDir, strconv.FormatInt(id, 10)+".jpg"))
	if w != 100 || h != 200 {
		t.Fatalf("cover is %dx%d, want 100x200 (turned upright)", w, h)
	}
	if !redish(img.At(50, 20)) || redish(img.At(50, 180)) {
		t.Error("the cover is not red on top and blue below")
	}

	if res, b := upload(t, url+"/poster/upload", "file", "p.jpg", exifJPEG(600, 400, 8)); res.StatusCode != 200 {
		t.Fatalf("poster = %d %s", res.StatusCode, b)
	}
	w, h, img = storedSize(t, filepath.Join(e.coverDir, covers.PosterName(id)))
	if w != 300 || h != 450 { // the whole upright picture (400 wide, 600 tall) is 2:3
		t.Errorf("poster is %dx%d", w, h)
	}
	if redish(img.At(w/2, 20)) || !redish(img.At(w/2, h-20)) { // orientation 8: the left (red) side is the bottom
		t.Error("the poster is not blue on top and red below")
	}
}

// An image that is within the pixel limit but needs more memory to decode than the budget is refused (like a screenshot).
func TestUploadDecodeBudget(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	e.server.ShotDecodeBudget = 1 << 20
	for _, kind := range []string{"/cover/upload", "/poster/upload"} {
		res, body := upload(t, url+kind, "file", "a.png", upPNG(1200, 1000)) // ~4.8 MB to decode
		if res.StatusCode != 413 || !strings.Contains(string(body), "too large to process") {
			t.Errorf("%s over budget = %d %s", kind, res.StatusCode, body)
		}
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a refused upload made a poster: %q", file)
	}
	if res, b := upload(t, url+"/cover/upload", "file", "a.png", upPNG(100, 100)); res.StatusCode != 200 {
		t.Errorf("a small image within budget = %d %s", res.StatusCode, b)
	}
}

// IGDB failing for now must not leave the previous link's poster for good: the poster worker tries again.
func TestRelinkTemporaryFailureIsRetried(t *testing.T) {
	e := newAddEnv(t)
	url, id := newPosterGame(t, e)
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	do(t, "POST", url+"/poster", `{"url":"`+e.images.URL+`/portrait.png"}`)
	do(t, "POST", url+"/poster", `{"auto":true}`) // an automatic poster, as after a first link
	e.meta.failFirst = 1
	patchIGDB(t, e, url, 901)
	for i := 0; i < 300; i++ {
		if _, src, _, _ := posterRow(t, e, id); src == "igdb" {
			if e.meta.failFirst != 0 {
				t.Fatal("the failure was never hit")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the poster worker never made the new poster")
}

// An automatic poster made for an old link must not overwrite the poster of a newer one, whichever maker it is.
func TestMakeStaleWriteGuard(t *testing.T) {
	e := newAddEnv(t)
	_, id := newPosterGame(t, e)
	e.db.Exec(`UPDATE game SET igdb_id = 10 WHERE id = ?`, id)
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	m := &posters.Maker{DB: e.db, Dir: e.coverDir, Src: e.meta, Allowed: func(string) bool { return true },
		Fetch: func(u, dir string, gameID int64) (string, error) {
			e.db.Exec(`UPDATE game SET igdb_id = 11 WHERE id = ?`, gameID)
			return covers.SavePoster(bytes.NewReader(upPNG(600, 900)), dir, gameID)
		}}
	if _, err := m.Make(context.Background(), id); err != posters.ErrStale {
		t.Errorf("Make = %v, want ErrStale", err)
	}
	e.db.Exec(`UPDATE game SET igdb_id = 10 WHERE id = ?`, id)
	if _, err := m.Remake(context.Background(), id); err != posters.ErrStale {
		t.Errorf("Remake = %v, want ErrStale", err)
	}
	if file, _, _, _ := posterRow(t, e, id); file != "" {
		t.Errorf("a stale result wrote %q", file)
	}
}
