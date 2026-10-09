package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// Backup writes a consistent, compacted copy of the database to dest while the app keeps running
// (VACUUM INTO reads a snapshot, it does not block writers for long). dest must not exist: an existing
// backup is never overwritten. The copy is written under a temporary name and renamed when complete,
// so an interrupted backup never leaves a half-written file that looks valid.
func Backup(d *sql.DB, dest string) error {
	if dest == "" {
		return fmt.Errorf("backup: no destination")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: %s already exists", dest)
	}
	tmp := dest + ".partial"
	os.Remove(tmp)
	// VACUUM INTO takes a string literal, not a bound parameter.
	if _, err := d.Exec("VACUUM INTO '" + strings.ReplaceAll(tmp, "'", "''") + "'"); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("backup: %w", err)
	}
	return os.Rename(tmp, dest)
}

// BackupFile copies the database file src to dest (see Backup) exactly as it is: it never migrates src, so a newer
// program can back up an older database before it upgrades it, and a missing src is an error, not a new empty database.
func BackupFile(src, dest string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// mode=rw never creates the file; read-write (not ro) so a WAL database opens even when its -shm file is missing.
	d, err := sql.Open("sqlite", FileURI(src)+"?mode=rw&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	return Backup(d, dest)
}
