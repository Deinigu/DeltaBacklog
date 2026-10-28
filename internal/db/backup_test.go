package db

import (
	"database/sql"
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
	if title != "Hades II" || platforms != 2 || version != 14 {
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

// oldDatabase writes a database at schema version 5 (migrations 001 to 005 only), like a library made by an older release.
func oldDatabase(t *testing.T, path string) {
	t.Helper()
	d, err := sql.Open("sqlite", FileURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, name := range []string{"001_init.sql", "002_drop_rating.sql", "003_views.sql", "004_cover_version.sql", "005_drop_series_order.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := d.Exec(`PRAGMA user_version = 5; INSERT INTO game(title, title_sort, status_id) VALUES ('Outer Wilds', 'outer wilds', 4)`); err != nil {
		t.Fatal(err)
	}
}

func TestBackupFileNeverMigratesOrCreates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app.db")
	oldDatabase(t, src)

	dest := filepath.Join(dir, "before-upgrade.db")
	if err := BackupFile(src, dest); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{src, dest} {
		r, err := Check(f)
		if err != nil {
			t.Fatal(err)
		}
		if r.Version != 5 || r.Games != 1 || r.Integrity != "ok" {
			t.Errorf("%s: version %d, %d games, integrity %s; want the untouched version 5 with 1 game", filepath.Base(f), r.Version, r.Games, r.Integrity)
		}
	}

	// a wrong data folder must fail, not back up a brand-new empty database
	missing := filepath.Join(dir, "nothing-here", "app.db")
	if err := BackupFile(missing, filepath.Join(dir, "empty.db")); err == nil {
		t.Error("backing up a missing database must fail")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("BackupFile created the missing database")
	}
	if _, err := os.Stat(filepath.Join(dir, "empty.db")); !os.IsNotExist(err) {
		t.Error("a backup file was written for a missing database")
	}
}
