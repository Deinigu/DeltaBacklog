package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIsAConsistentOpenableCopy(t *testing.T) {
	d := open(t)
	if _, err := d.Exec(`INSERT INTO platform(name) VALUES ('PC'), ('Switch')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('Hades II', 'hades ii', 1)`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := Backup(d, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Error("the temporary file must be gone after a successful backup")
	}

	// the copy opens as a normal database with the same data and schema version
	b, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var title string
	var platforms, version int
	b.QueryRow(`SELECT title FROM game`).Scan(&title)
	b.QueryRow(`SELECT count(*) FROM platform`).Scan(&platforms)
	b.QueryRow(`PRAGMA user_version`).Scan(&version)
	if title != "Hades II" || platforms != 2 || version != 8 {
		t.Errorf("copy has title=%q platforms=%d version=%d", title, platforms, version)
	}

	// an existing file is never overwritten, and a bad destination fails cleanly
	if err := Backup(d, dest); err == nil {
		t.Error("overwriting an existing backup must fail")
	}
	if err := Backup(d, ""); err == nil {
		t.Error("an empty destination must fail")
	}
	if err := Backup(d, filepath.Join(dir, "missing-dir", "x.db")); err == nil {
		t.Error("a destination in a missing folder must fail")
	}
	// quotes in a path must not break the statement
	quoted := filepath.Join(dir, "it's a backup.db")
	if err := Backup(d, quoted); err != nil {
		t.Errorf("a path with a quote: %v", err)
	}
}
