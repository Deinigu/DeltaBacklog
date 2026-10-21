package posters

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

type fakeSource struct{ igdb, sgdb string }

func (f fakeSource) IGDBCoverURL(context.Context, int64) (string, error)       { return f.igdb, nil }
func (f fakeSource) SteamGridPortrait(context.Context, string) (string, error) { return f.sgdb, nil }

func TestFetchPostersCommand(t *testing.T) {
	Pause = 0
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		png.Encode(w, image.NewRGBA(image.Rect(0, 0, 528, 748)))
	}))
	defer img.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO game(id, title, title_sort, status_id, igdb_id) VALUES (1, 'Linked', 'linked', 7, 5)`,
		`INSERT INTO game(id, title, title_sort, status_id) VALUES (2, 'Unlinked', 'unlinked', 7)`,
		`INSERT INTO game(id, title, title_sort, status_id, poster_file, poster_src) VALUES (3, 'Done', 'done', 7, 'p3.jpg', 'igdb')`,
		`INSERT INTO game(id, title, title_sort, status_id, poster_file, poster_src) VALUES (4, 'Cropped', 'cropped', 7, 'p4.jpg', 'crop')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	d.Close()
	covers := filepath.Join(dir, "covers")
	allowed := func(u string) bool { return strings.HasPrefix(u, img.URL) }
	src := fakeSource{igdb: img.URL + "/a.png", sgdb: img.URL + "/b.png"}
	run := func(args ...string) (int, string) {
		var out bytes.Buffer
		return Main(args, path, covers, src, allowed, &out), out.String()
	}

	before, _ := os.ReadFile(path)
	if code, out := run("--dry-run"); code != 0 || !strings.Contains(out, "2 games would get a poster") || !strings.Contains(out, "Linked") {
		t.Fatalf("dry run: %d\n%s", code, out)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("the dry run changed the database")
	}
	if _, err := os.Stat(covers); err == nil {
		t.Error("the dry run created files")
	}

	if code, out := run("--limit", "1"); code != 0 || !strings.Contains(out, "1 from IGDB") {
		t.Fatalf("limit: %d\n%s", code, out)
	}
	if code, out := run(); code != 0 || !strings.Contains(out, "1 from SteamGridDB") {
		t.Fatalf("rest: %d\n%s", code, out)
	}
	for _, f := range []string{"p1.jpg", "p2.jpg"} {
		if _, err := os.Stat(filepath.Join(covers, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if code, out := run(); code != 0 || !strings.Contains(out, "0 from IGDB, 0 from SteamGridDB") {
		t.Fatalf("a second run must find nothing: %d\n%s", code, out)
	}
	// --upgrade redoes the crop only
	if code, out := run("--upgrade"); code != 0 || !strings.Contains(out, "1 from SteamGridDB") {
		t.Fatalf("upgrade: %d\n%s", code, out)
	}
	if code, _ := run("--bogus"); code != 2 {
		t.Errorf("bad flag exit %d", code)
	}
	if code, _ := run("extra"); code != 2 {
		t.Errorf("stray argument exit %d", code)
	}
	if code, _ := Main([]string{"--db", filepath.Join(dir, "none.db"), "--dry-run"}, "", covers, src, allowed, &bytes.Buffer{}), 0; code != 1 {
		t.Errorf("missing database exit %d", code)
	}
}

// A poster the user chose is never remade by the queue's Make or by fetch-posters --upgrade; Remake replaces it.
func TestUserPosterIsNotReplaced(t *testing.T) {
	Pause = 0
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		png.Encode(w, image.NewRGBA(image.Rect(0, 0, 528, 748)))
	}))
	defer img.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO game(id, title, title_sort, status_id, poster_file, poster_src, poster_user) VALUES (1, 'Mine', 'mine', 7, 'p1.jpg', 'crop', 1)`,
		`INSERT INTO game(id, title, title_sort, status_id, poster_file, poster_src, poster_user) VALUES (2, 'Auto crop', 'auto crop', 7, 'p2.jpg', 'crop', 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	defer d.Close()
	covers := filepath.Join(dir, "covers")
	allowed := func(u string) bool { return strings.HasPrefix(u, img.URL) }
	src := fakeSource{igdb: img.URL + "/a.png", sgdb: img.URL + "/b.png"}
	var out bytes.Buffer
	if code := Main([]string{"--upgrade"}, path, covers, src, allowed, &out); code != 0 || !strings.Contains(out.String(), "1 from SteamGridDB") {
		t.Fatalf("upgrade: %d\n%s", code, out.String())
	}
	var user int
	var s string
	d.QueryRow(`SELECT poster_src, poster_user FROM game WHERE id = 1`).Scan(&s, &user)
	if s != "crop" || user != 1 {
		t.Errorf("the user's poster changed: %s %d", s, user)
	}
	if _, err := os.Stat(filepath.Join(covers, "p1.jpg")); err == nil {
		t.Error("the user's poster file was rewritten")
	}

	m := &Maker{DB: d, Dir: covers, Src: src, Allowed: allowed}
	if _, err := m.Make(context.Background(), 1); err != ErrUserPoster {
		t.Errorf("Make = %v, want ErrUserPoster", err)
	}
	if how, err := m.Remake(context.Background(), 1); err != nil || how != "sgdb" {
		t.Fatalf("Remake = %q %v", how, err)
	}
	d.QueryRow(`SELECT poster_src, poster_user FROM game WHERE id = 1`).Scan(&s, &user)
	if s != "sgdb" || user != 0 {
		t.Errorf("after Remake: %s %d", s, user)
	}
}
