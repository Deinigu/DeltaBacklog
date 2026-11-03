package deals

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func jpegBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{200, 10, 10, 255})
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

// fakeCDN serves images by path and counts the requests.
func fakeCDN(t *testing.T, files map[string][]byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://example.invalid/x.jpg", http.StatusFound)
			return
		}
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func testCache(t *testing.T) *ImageCache {
	c := NewImageCache(filepath.Join(t.TempDir(), "steam"))
	c.allowLoopback = true
	return c
}

func TestImageFetchStoresRealType(t *testing.T) {
	srv, _ := fakeCDN(t, map[string][]byte{"/a.jpg": jpegBytes(231, 87), "/b.jpg": pngBytes(40, 20)})
	c := testCache(t)
	if err := c.Fetch(context.Background(), 10, srv.URL+"/a.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := ImageName(c.Dir, 10); got != "steam/10.jpg" {
		t.Fatalf("name = %q", got)
	}
	// a PNG behind a .jpg name is stored as the PNG it is
	if err := c.Fetch(context.Background(), 11, srv.URL+"/b.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := ImageName(c.Dir, 11); got != "steam/11.png" {
		t.Fatalf("name = %q", got)
	}
	if ImageName(c.Dir, 12) != "" {
		t.Fatal("an app without a file has no name")
	}
}

func TestImageFetchRefusals(t *testing.T) {
	big := append(jpegBytes(10, 10), make([]byte, maxImageBytes)...)
	srv, _ := fakeCDN(t, map[string][]byte{
		"/html": []byte("<html>not an image</html>"), "/big": big, "/wide": jpegBytes(maxImageSide+1, 4),
		"/gif": append([]byte("GIF89a"), make([]byte, 20)...),
	})
	c := testCache(t)
	for _, p := range []string{"/html", "/big", "/wide", "/gif", "/missing", "/redirect"} {
		err := c.Fetch(context.Background(), 5, srv.URL+p)
		if err == nil || !strings.Contains(err.Error(), errBadImage.Error()) {
			t.Fatalf("%s: err = %v, want a refusal", p, err)
		}
	}
	if ImageName(c.Dir, 5) != "" {
		t.Fatal("a refused image must leave no file")
	}
	if ents, _ := os.ReadDir(c.Dir); len(ents) != 0 {
		t.Fatalf("leftovers: %v", ents)
	}
}

func TestImageHostAllowList(t *testing.T) {
	c := NewImageCache(t.TempDir()) // production rules: no loopback
	for _, u := range []string{
		"http://127.0.0.1/a.jpg", "https://evil.example/a.jpg", "http://shared.akamai.steamstatic.com/a.jpg",
		"https://shared.akamai.steamstatic.com.evil.example/a.jpg", "https://user@shared.akamai.steamstatic.com/a.jpg",
		"https://shared.akamai.steamstatic.com:8443/a.jpg", "file:///etc/passwd", "ftp://shared.akamai.steamstatic.com/a",
	} {
		if err := c.Fetch(context.Background(), 1, u); err == nil || !strings.Contains(err.Error(), "host not allowed") {
			t.Errorf("%s: err = %v, want host not allowed", u, err)
		}
	}
	for h := range imageHosts {
		if !strings.HasSuffix(h, ".steamstatic.com") && h != "steamcdn-a.akamaihd.net" {
			t.Errorf("unexpected allowed host %s", h)
		}
	}
}

// steamFake answers GetItems (names and assets) and counts the requests; the SteamID never appears in any of them.
func steamFake(t *testing.T, cdn string, noAssets map[int64]bool) (*Steam, *atomic.Int32, *[]string) {
	t.Helper()
	var n atomic.Int32
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		reqs = append(reqs, r.URL.String())
		var in struct {
			IDs []struct {
				AppID int64 `json:"appid"`
			} `json:"ids"`
			Req map[string]any `json:"data_request"`
		}
		_ = json.Unmarshal([]byte(r.URL.Query().Get("input_json")), &in)
		type item map[string]any
		var items []item
		for _, id := range in.IDs {
			it := item{"id": id.AppID, "success": 1, "name": "Game"}
			if in.Req["include_assets"] == true && !noAssets[id.AppID] {
				it["assets"] = item{"asset_url_format": "steam/apps/1/${FILENAME}?t=1", "small_capsule": "capsule_231x87.jpg", "header": "header.jpg"}
			}
			items = append(items, it)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"store_items": items}})
	}))
	t.Cleanup(srv.Close)
	s, err := NewSteam("76561198000000000")
	if err != nil {
		t.Fatal(err)
	}
	s.BaseURL = srv.URL
	s.ImageBase = cdn + "/"
	s.Images = testCache(t)
	return s, &n, &reqs
}

func TestAssetsParsing(t *testing.T) {
	s, _, reqs := steamFake(t, "https://shared.akamai.steamstatic.com/store_item_assets", nil)
	got, err := s.Assets(context.Background(), []int64{7})
	if err != nil {
		t.Fatal(err)
	}
	// the app id is already part of the format Steam sends
	want := []string{
		"https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/1/capsule_231x87.jpg?t=1",
	}
	if len(got[7]) != 2 || got[7][0] != want[0] {
		t.Fatalf("urls = %v", got[7])
	}
	if !strings.Contains((*reqs)[0], "include_assets") {
		t.Fatalf("request asks for no assets: %s", (*reqs)[0])
	}
}

func TestAssetURLsRejectOddNames(t *testing.T) {
	it := storeItem{Assets: &struct {
		URLFormat    string `json:"asset_url_format"`
		SmallCapsule string `json:"small_capsule"`
		MainCapsule  string `json:"main_capsule"`
		Header       string `json:"header"`
	}{URLFormat: "steam/apps/1/${FILENAME}", SmallCapsule: "../../etc/passwd", Header: "a b.jpg", MainCapsule: "ok.jpg"}}
	got := assetURLs("https://x/", it)
	if len(got) != 1 || got[0] != "https://x/steam/apps/1/ok.jpg" {
		t.Fatalf("urls = %v", got)
	}
	it.Assets.URLFormat = "https://evil.example/${FILENAME}"
	if assetURLs("https://x/", it) != nil {
		t.Fatal("a format with a scheme must be refused")
	}
}

func TestSyncImagesSkipsWhatItHas(t *testing.T) {
	cdn, hits := fakeCDN(t, map[string][]byte{
		"/steam/apps/1/capsule_231x87.jpg": jpegBytes(231, 87),
	})
	s, steamReqs, reqs := steamFake(t, cdn.URL, map[int64]bool{3: true})
	//
	s.ImageBase = cdn.URL + "/"
	ctx := context.Background()
	s.SyncImages(ctx, []int64{1, 2, 3})
	if ImageName(s.Images.Dir, 1) == "" || ImageName(s.Images.Dir, 2) == "" {
		t.Fatal("games with assets get an image")
	}
	if ImageName(s.Images.Dir, 3) != "" {
		t.Fatal("a game without assets has none")
	}
	if steamReqs.Load() != 1 || hits.Load() != 2 {
		t.Fatalf("first run: %d Steam requests, %d downloads; want 1 and 2", steamReqs.Load(), hits.Load())
	}
	// second run: 1 and 2 have their file, 3 is marked imageless for a week: nothing is asked at all
	s.SyncImages(ctx, []int64{1, 2, 3})
	if steamReqs.Load() != 1 || hits.Load() != 2 {
		t.Fatalf("second run asked again: %d Steam requests, %d downloads", steamReqs.Load(), hits.Load())
	}
	// a marker older than a week is asked about again
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(s.Images.marker(3), old, old); err != nil {
		t.Fatal(err)
	}
	s.SyncImages(ctx, []int64{1, 2, 3})
	if steamReqs.Load() != 2 || hits.Load() != 2 {
		t.Fatalf("expired marker: %d Steam requests, %d downloads", steamReqs.Load(), hits.Load())
	}
	for _, r := range *reqs {
		if strings.Contains(r, "76561198000000000") {
			t.Fatalf("the SteamID is in a request: %s", r)
		}
	}
}

func TestSyncImagesBadImageIsMarkedAndNetworkErrorIsNot(t *testing.T) {
	cdn, _ := fakeCDN(t, map[string][]byte{"/steam/apps/1/capsule_231x87.jpg": []byte("nope")})
	s, _, _ := steamFake(t, cdn.URL, nil)
	s.SyncImages(context.Background(), []int64{4})
	if ImageName(s.Images.Dir, 4) != "" {
		t.Fatal("a bad image is refused")
	}
	if _, err := os.Stat(s.Images.marker(4)); err != nil {
		t.Fatal("a refused image is remembered for a week")
	}
	// the CDN is down: nothing is marked, the next run tries again
	s2, _, _ := steamFake(t, "http://127.0.0.1:1", nil)
	s2.SyncImages(context.Background(), []int64{5})
	if _, err := os.Stat(s2.Images.marker(5)); err == nil {
		t.Fatal("a network error must not be remembered")
	}
}

func TestSyncImagesOffWithoutCache(t *testing.T) {
	s, n, _ := steamFake(t, "http://127.0.0.1:1", nil)
	s.Images = nil
	s.SyncImages(context.Background(), []int64{1})
	if n.Load() != 0 {
		t.Fatal("no cache, no requests")
	}
}

func TestPruneKeepsOnlyWhatIsWanted(t *testing.T) {
	c := testCache(t)
	for _, n := range []string{"1.jpg", "2.png", "3.none", "4.jpg", "notes.txt", "5.jpg.tmp"} {
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c.Prune([]int64{1, 3})
	ents, _ := os.ReadDir(c.Dir)
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != "1.jpg,3.none,5.jpg.tmp,notes.txt" {
		t.Fatalf("left %v; only 2.png and 4.jpg may go (foreign names are never touched)", got)
	}
}

func TestFailuresCountOverTheWholeRun(t *testing.T) {
	// 250 apps = 3 batches; the CDN is down. The run must stop after 3 failed downloads, not 3 per batch.
	var cdnHits atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnHits.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(down.Close)
	s, _, _ := steamFake(t, down.URL, nil)
	ids := make([]int64, 250)
	for i := range ids {
		ids[i] = int64(1000 + i)
	}
	s.SyncImages(context.Background(), ids)
	if n := cdnHits.Load(); n > 6 { // 3 apps, each tries its two addresses
		t.Fatalf("%d downloads were tried after the CDN failed", n)
	}
}

func TestLibraryImages(t *testing.T) {
	d := newDB(t)
	addGame(t, d, 1, "Has Steam", "backlog", "https://store.steampowered.com/app/111/X")
	addGame(t, d, 2, "Has cover", "backlog", "https://store.steampowered.com/app/222/X")
	addGame(t, d, 3, "No link", "backlog", "")
	if _, err := d.Exec(`UPDATE game SET cover_file = '2.jpg' WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "steam")
	c := &ImageCache{Dir: dir}
	_ = os.MkdirAll(dir, 0o755)
	for _, id := range []string{"111", "222"} {
		_ = os.WriteFile(filepath.Join(dir, id+".jpg"), jpegBytes(10, 10), 0o644)
	}
	_ = c
	got, err := LibraryImages(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[1] != "steam/111.jpg" {
		t.Fatalf("lib images = %v; only the game without a cover and with a Steam link gets one", got)
	}
}
