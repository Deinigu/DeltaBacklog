package igdblink

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// The enrichment keeps IGDB's collection id of a linked game (also on a --refresh run), and a game IGDB puts in no
// collection loses a stale one.
func TestEnrichStoresTheCollectionID(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.CollectionID = 77
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	var col *int64
	d.QueryRow(`SELECT igdb_collection_id FROM game WHERE id = ?`, id).Scan(&col)
	if col == nil || *col != 77 {
		t.Fatalf("collection id = %v", col)
	}
	det.CollectionID = 0
	f.details[1] = det
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	col = nil
	d.QueryRow(`SELECT igdb_collection_id FROM game WHERE id = ?`, id).Scan(&col)
	if col != nil {
		t.Errorf("a stale collection id survived a refresh: %v", *col)
	}
}

func TestEnrichStoresTheReleaseDate(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.Release = meta.Release{Date: 1830211199, Format: meta.FormatYear}
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	var date, format *int64
	d.QueryRow(`SELECT first_release_date, release_date_format FROM game WHERE id = ?`, id).Scan(&date, &format)
	if date == nil || *date != 1830211199 || *format != meta.FormatYear {
		t.Fatalf("date %v format %v", date, format)
	}
	det.Release = meta.Release{} // IGDB took the date back (TBD)
	f.details[1] = det
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	date = nil
	d.QueryRow(`SELECT first_release_date FROM game WHERE id = ?`, id).Scan(&date)
	if date != nil {
		t.Errorf("a stale date survived a refresh: %v", *date)
	}
}

// collectionSearch adds the collection reads to the enrichment fake.
type collectionSearch struct {
	*enrichSearch
	read []int64
}

func (c *collectionSearch) CollectionsOf(context.Context, []int64) (map[int64]meta.CollectionRef, error) {
	return map[int64]meta.CollectionRef{}, nil
}

func (c *collectionSearch) CollectionGames(_ context.Context, id int64) (string, []meta.CollectionGame, error) {
	c.read = append(c.read, id)
	return "Hades", []meta.CollectionGame{{IGDBID: 1, Name: "Hades", Year: 2020}, {IGDBID: 2, Name: "Hades II", Year: 2025}}, nil
}

// `sync-igdb --enrich` also reads the collections of the library's series; a dry run does not.
func TestEnrichCommandReadsTheCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, _ := db.Open(path)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	series, _ := db.GetOrCreate(d, "series", "Hades")
	d.Exec(`UPDATE game SET series_id = ? WHERE id = ?`, series, id)
	d.Close()
	det := hades()
	det.CollectionID = 5
	src := &collectionSearch{enrichSearch: &enrichSearch{&fakeEnricher{details: map[int64]meta.Details{1: det}}}}

	var out bytes.Buffer
	if code := run([]string{"--enrich", "--dry-run", "--db", path}, "", src, true, &out); code != 0 || len(src.read) != 0 {
		t.Fatalf("dry run: exit %d, collections read %v\n%s", code, src.read, out.String())
	}
	out.Reset()
	if code := run([]string{"--enrich", "--db", path}, "", src, true, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	if len(src.read) != 1 || src.read[0] != 5 || !strings.Contains(out.String(), "1 collections read (2 games)") {
		t.Errorf("collections read %v\n%s", src.read, out.String())
	}
	d, _ = db.Open(path)
	defer d.Close()
	var n int
	d.QueryRow(`SELECT count(*) FROM igdb_collection_game`).Scan(&n)
	if n != 2 {
		t.Errorf("cached games = %d", n)
	}
}
