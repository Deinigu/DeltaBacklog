package db

import (
	"database/sql"
	"fmt"
)

// Report is what Check found in a database file.
type Report struct {
	Integrity string // "ok" when SQLite's own integrity check passes
	Version   int    // schema version (PRAGMA user_version)
	Games     int
	Plays     int
	Views     int
}

// Check opens a database file strictly read-only (it never migrates or changes it, so it is safe on a backup)
// and reports whether it is intact and what it holds.
func Check(path string) (Report, error) {
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return Report{}, err
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	var r Report
	if err := d.QueryRow("PRAGMA integrity_check").Scan(&r.Integrity); err != nil {
		return r, fmt.Errorf("check %s: %w", path, err)
	}
	d.QueryRow("PRAGMA user_version").Scan(&r.Version)
	// a database that is not ours (or is too old) simply lacks these tables: report zeros instead of failing
	d.QueryRow("SELECT count(*) FROM game").Scan(&r.Games)
	d.QueryRow("SELECT count(*) FROM play").Scan(&r.Plays)
	d.QueryRow("SELECT count(*) FROM saved_view").Scan(&r.Views)
	return r, nil
}
