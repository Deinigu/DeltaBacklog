// Package db opens the SQLite database and applies the embedded migrations.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// lookupTables are the tables GetOrCreate may write to (names are never user input).
var lookupTables = map[string]bool{
	"platform": true, "genre": true, "theme": true, "ownership": true,
	"series": true, "company": true, "player": true, "game_mode": true, "perspective": true,
}

// FileURI turns a file path into a SQLite "file:" URI, escaping the characters that would otherwise start the
// query or fragment part (%, #, ?).
func FileURI(path string) string {
	return "file:" + strings.NewReplacer("%", "%25", "#", "%23", "?", "%3f").Replace(path)
}

// Open opens (creating if needed) the database at path and migrates it to the latest version.
func Open(path string) (*sql.DB, error) {
	dsn := FileURI(path) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; one connection keeps the Pi's memory use predictable.
	d.SetMaxOpenConns(1)
	if err := Migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Migrate applies every migrations/NNN_*.sql whose number is above PRAGMA user_version.
func Migrate(d *sql.DB) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var current int
	if err := d.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		if n <= current {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Execer is satisfied by *sql.DB and *sql.Tx.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// GetOrCreate returns the id of the lookup row with this name, inserting it when missing.
func GetOrCreate(x Execer, table, name string) (int64, error) {
	if !lookupTables[table] {
		return 0, fmt.Errorf("not a lookup table: %s", table)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("empty %s name", table)
	}
	var id int64
	err := x.QueryRow("SELECT id FROM "+table+" WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := x.Exec("INSERT INTO "+table+"(name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// IndexGame refreshes the full-text row for one game.
func IndexGame(x Execer, gameID int64) error {
	if _, err := x.Exec("DELETE FROM game_fts WHERE rowid = ?", gameID); err != nil {
		return err
	}
	_, err := x.Exec(`INSERT INTO game_fts(rowid, title, series, notes)
		SELECT g.id, g.title, coalesce(s.name, ''), g.notes
		FROM game g LEFT JOIN series s ON s.id = g.series_id WHERE g.id = ?`, gameID)
	return err
}

// NormalizeTheme folds verbose IGDB theme names ("4X (explore, expand, ...)" becomes "4X").
func NormalizeTheme(name string) string {
	if strings.HasPrefix(name, "4X (") {
		return "4X"
	}
	return name
}

// TitleSort lowercases and drops a leading article and punctuation so "The Witcher 3" sorts under W.
func TitleSort(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) {
			t = t[len(a):]
			break
		}
	}
	return strings.TrimLeftFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
