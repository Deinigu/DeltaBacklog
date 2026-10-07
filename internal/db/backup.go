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
