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

// The enrichment keeps every IGDB collection of a linked game (and leaves its version root to the collections refresh) (also on a --refresh run), and a game
// IGDB puts in no collection loses the stale ones.
func TestEnrichStoresTheCollections(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	det := hades()
	det.Collections = []meta.CollectionRef{{ID: 77, Name: "Hades"}, {ID: 78, Name: "Supergiant"}}
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	var n int
	var parent sql.NullInt64
	d.QueryRow(`SELECT count(*) FROM game_collection WHERE game_id = ?`, id).Scan(&n)
	d.QueryRow(`SELECT igdb_parent_id FROM game WHERE id = ?`, id).Scan(&parent)
	if n != 2 || parent.Valid { // the version root is read later by the collections refresh
		t.Fatalf("collections %d, root %v", n, parent)
	}
	det.Collections = nil
	f.details[1] = det
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT count(*) FROM game_collection WHERE game_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("stale collections survived a refresh: %d", n)
	}
}

// An enrichment (also --refresh) keeps the version root a game already has: it depends only on the IGDB id.
func TestEnrichKeepsAStoredVersionRoot(t *testing.T) {
	d := openDB(t)
	id := addGame(t, d, "Hades")
	link(t, d, id, 1)
	d.Exec(`UPDATE game SET igdb_parent_id = 221280 WHERE id = ?`, id)
	det := hades()
	det.Collections = []meta.CollectionRef{{ID: 77, Name: "Hades"}}
	f := &fakeEnricher{details: map[int64]meta.Details{1: det}}
	for _, refresh := range []bool{false, true} {
		if refresh {
			d.Exec(`UPDATE game SET igdb_enriched_at = '2026-01-01T00:00:00Z' WHERE id = ?`, id)
		}
		if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep, Refresh: refresh}); err != nil {
			t.Fatal(err)
		}
		var root sql.NullInt64
		d.QueryRow(`SELECT igdb_parent_id FROM game WHERE id = ?`, id).Scan(&root)
		if !root.Valid || root.Int64 != 221280 {
			t.Errorf("refresh=%v: the stored root became %v", refresh, root)
		}
	}
}

// collectionSearch adds the collection reads to the enrichment fake.
type collectionSearch struct {
	*enrichSearch
	read []int64
}

func (c *collectionSearch) GameLinks(_ context.Context, ids []int64) (map[int64]meta.GameLinks, error) {
	out := map[int64]meta.GameLinks{}
	for _, id := range ids {
		out[id] = meta.GameLinks{Collections: []meta.CollectionRef{{ID: 5, Name: "Hades"}}} // no parent
	}
	return out, nil
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
