package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckReadsABackupWithoutChangingIt(t *testing.T) {
	d := open(t)
	d.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('A', 'a', 1), ('B', 'b', 2)`)
	d.Exec(`INSERT INTO play(game_id, year) VALUES (1, 2024), (1, 2025), (2, 2025)`)
	dest := filepath.Join(t.TempDir(), "b.db")
	if err := Backup(d, dest); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(dest)

	r, err := Check(dest)
	if err != nil {
		t.Fatal(err)
	}
	if r.Integrity != "ok" || r.Games != 2 || r.Plays != 3 || r.Views != 5 || r.Version != 12 {
		t.Errorf("report = %+v", r)
	}
	after, _ := os.ReadFile(dest)
	if string(before) != string(after) {
		t.Error("checking a database must never modify it")
	}
}

func TestCheckRejectsNonDatabases(t *testing.T) {
	dir := t.TempDir()
	if _, err := Check(filepath.Join(dir, "missing.db")); err == nil {
		t.Error("a missing file must be an error, not an empty report")
	}
	junk := filepath.Join(dir, "junk.db")
	os.WriteFile(junk, []byte("this is not sqlite at all, just text that is long enough to look like a header....."), 0o644)
	if r, err := Check(junk); err == nil && r.Integrity == "ok" {
		t.Errorf("a text file must not pass the check: %+v", r)
	}
}
