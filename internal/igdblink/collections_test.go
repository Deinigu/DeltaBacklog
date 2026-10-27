package igdblink

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// The enrichment keeps every IGDB collection of a linked game and its parent game (also on a --refresh run), and a game
// IGDB puts in no collection loses the stale ones.
func TestEnrichStoresTheCollectionsAndTheParent(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.Collections = []meta.CollectionRef{{ID: 77, Name: "Hades"}, {ID: 78, Name: "Supergiant"}}
	det.ParentID = 55
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	var n, parent int
	d.QueryRow(`SELECT count(*) FROM game_collection WHERE game_id = ?`, id).Scan(&n)
	d.QueryRow(`SELECT igdb_parent_id FROM game WHERE id = ?`, id).Scan(&parent)
	if n != 2 || parent != 55 {
		t.Fatalf("collections %d, parent %d", n, parent)
	}
	det.Collections, det.ParentID = nil, 0
	f.details[1] = det
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	var read sql.NullInt64
	d.QueryRow(`SELECT count(*) FROM game_collection WHERE game_id = ?`, id).Scan(&n)
	d.QueryRow(`SELECT igdb_parent_id FROM game WHERE id = ?`, id).Scan(&read)
	if n != 0 || !read.Valid || read.Int64 != 0 { // 0 = read, IGDB has none
		t.Errorf("stale links survived a refresh: %d collections, parent %v", n, read)
	}
}

// collectionSearch adds the collection reads to the enrichment fake.
type collectionSearch struct {
	*enrichSearch
	read []int64
}

func (c *collectionSearch) GameLinks(context.Context, []int64) (map[int64]meta.GameLinks, error) {
	return map[int64]meta.GameLinks{}, nil
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
	det.Collections = []meta.CollectionRef{{ID: 5, Name: "Hades"}}
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
