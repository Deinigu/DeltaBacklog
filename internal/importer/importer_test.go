package importer

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

func TestSplitList(t *testing.T) {
	cases := map[string][]string{
		"PC, PS3":                    {"PC", "PS3"},
		"  PC ,pc, ,Wii":             {"PC", "Wii"},
		"":                           nil,
		"4X (explore, expand), Open": {"4X (explore, expand)", "Open"},
		"Hack and slash/Beat 'em up": {"Hack and slash/Beat 'em up"},
	}
	for in, want := range cases {
		got := splitList(in)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitList(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseYears(t *testing.T) {
	plays, bad := parseYears("Childhood, 2023,2025,2023,Always,foo,1850")
	want := []Play{{Period: "childhood"}, {Year: 2023}, {Year: 2025}, {Period: "always"}}
	if !reflect.DeepEqual(plays, want) {
		t.Errorf("plays = %v, want %v", plays, want)
	}
	if !reflect.DeepEqual(bad, []string{"foo", "1850"}) {
		t.Errorf("bad = %v", bad)
	}
}

func TestParseBoxart(t *testing.T) {
	u, _ := parseBoxart(`[{"url":"https://cdn/x.png","id":"a"}]`, "http://nc")
	if u != "https://cdn/x.png" {
		t.Errorf("url = %q", u)
	}
	p, _ := parseBoxart(`[{"path":"download/noco/a/b.png"}]`, "http://nc:8080/")
	if p != "http://nc:8080/download/noco/a/b.png" {
		t.Errorf("path = %q", p)
	}
	if _, err := parseBoxart("not json", ""); err == nil {
		t.Error("expected error for bad json")
	}
}

func openMem(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

const sampleCSV = `id,Title,Platform,Status,Style,VR,Genre,Boxart,Completed,Years_played,Series,Notas,Ownership,Played_with,Favorito,nc_order
1,Hades II,PC,Finished,"Action, Fantasy",f,"RPG, Indie","[{""url"":""https://x/y.png""}]",f,"2025,2026",Hades,"multi
line note",Owned,Solo,t,1
2,Old Game,"PC, PS3",Backlog,"4X (explore, expand, exploit, and exterminate)",t,,,t,"Childhood,2023",,,Pirated,"Savios,Solo",f,2
3,Bad Status,PC,Weird,,f,,,f,2x24,,,,,f,3
`

func TestImportAndIdempotency(t *testing.T) {
	d := openMem(t)
	rep, jobs, err := Import(d, strings.NewReader(sampleCSV), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Inserted != 3 || rep.Skipped != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Warnings) != 2 { // unknown status + unrecognised year
		t.Errorf("warnings = %v", rep.Warnings)
	}
	if len(jobs) != 1 || jobs[0].URL != "https://x/y.png" {
		t.Errorf("jobs = %v", jobs)
	}

	count := func(q string, args ...any) int {
		var n int
		if err := d.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM game_platform`); n != 4 {
		t.Errorf("game_platform rows = %d", n)
	}
	if n := count(`SELECT count(*) FROM theme WHERE name = '4X'`); n != 1 {
		t.Error("4X theme was not folded")
	}
	if n := count(`SELECT count(*) FROM play WHERE period = 'childhood'`); n != 1 {
		t.Error("childhood period missing")
	}
	if n := count(`SELECT count(*) FROM player`); n != 2 {
		t.Errorf("players = %d", n)
	}
	if n := count(`SELECT count(*) FROM game g JOIN status s ON s.id = g.status_id WHERE g.title = 'Bad Status' AND s.code = 'backlog'`); n != 1 {
		t.Error("unknown status should fall back to backlog")
	}
	if n := count(`SELECT count(*) FROM game_fts WHERE game_fts MATCH 'hades'`); n != 1 {
		t.Errorf("fts hades = %d", n)
	}

	rep, _, err = Import(d, strings.NewReader(sampleCSV), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Inserted != 0 || rep.Skipped != 3 {
		t.Errorf("second run = %+v", rep)
	}
	if n := count(`SELECT count(*) FROM game`); n != 3 {
		t.Errorf("games after re-import = %d", n)
	}
}

// TestRealExport runs against the real NocoDB export when DELTABACKLOG_CSV points at it.
func TestRealExport(t *testing.T) {
	path := os.Getenv("DELTABACKLOG_CSV")
	if path == "" {
		t.Skip("DELTABACKLOG_CSV not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d := openMem(t)
	rep, _, err := Import(d, f, Options{NocoDBBase: "http://nocodb"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inserted=%d warnings=%d", rep.Inserted, len(rep.Warnings))
	for _, w := range rep.Warnings {
		t.Log("warning:", w)
	}
	rows, _ := d.Query(`SELECT s.label, count(*) FROM game g JOIN status s ON s.id = g.status_id GROUP BY s.id ORDER BY s.sort`)
	for rows.Next() {
		var l string
		var n int
		rows.Scan(&l, &n)
		t.Logf("%-20s %d", l, n)
	}
	rows.Close()
	if rep.Inserted != 593 {
		t.Errorf("inserted %d, want 593", rep.Inserted)
	}
}
