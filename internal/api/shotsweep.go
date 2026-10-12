package api

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/shots"
)

// shotGrace is how old a leftover must be before the sweep touches it: an upload in progress writes a temporary file
// and, for a moment, a stored file without its row.
const shotGrace = time.Hour

// adoptTolerance is how much older than its game a file may be and still be adopted (clock differences, imports).
const adoptTolerance = 10 * time.Minute

// orphanDir is where originals of games this database does not know are moved (never deleted).
const orphanDir = ".orphans"

// SweepResult says what SweepShots did.
type SweepResult struct {
	Removed     int // temporary files and thumbnails whose original is gone
	Adopted     int // originals without a row, of a game that exists: a row was added again
	Quarantined int // originals of unknown games, moved to <SCREENSHOTS_DIR>/.orphans/<game id>/
}

func (r SweepResult) String() string {
	return fmt.Sprintf("%d leftover files removed, %d pictures re-adopted, %d moved to %s", r.Removed, r.Adopted, r.Quarantined, orphanDir)
}

// SweepShots tidies the screenshot folder at start-up, only when the storage is available. It NEVER deletes an
// original: after a database restore (an older app.db next to a drive that is newer) the files on the drive are the
// only copy of pictures the database no longer lists. In each numeric game folder it
//   - removes *.tmp files and thumbnails whose original is gone, when older than an hour;
//   - re-adopts an original that has no row when the game exists and is not older than the game (size and EXIF are read again, the thumbnail is made
//     again if missing and the picture fits the decoding budget; otherwise the row is added and the thumbnail shows as
//     "missing");
//   - moves an original of an unknown game (and its thumbnail) to .orphans/<game id>/.
//
// Only files older than an hour are touched, anything else on the drive is left alone, and the sweep stops at the
// first database error: an incomplete list of rows must never turn pictures into orphans.
func (s *Server) SweepShots() (SweepResult, error) {
	var res SweepResult
	if !s.shotStorageOK() {
		return res, nil
	}
	entries, err := os.ReadDir(s.ShotDir)
	if err != nil {
		return res, err
	}
	cutoff := time.Now().Add(-shotGrace)
	for _, e := range entries {
		gid, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() || strconv.FormatInt(gid, 10) != e.Name() {
			continue
		}
		if err := s.sweepGame(gid, filepath.Join(s.ShotDir, e.Name()), cutoff, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Server) sweepGame(gid int64, dir string, cutoff time.Time, res *SweepResult) error {
	var addedRaw string
	exists := true
	if err := s.DB.QueryRow("SELECT added_at FROM game WHERE id = ?", gid).Scan(&addedRaw); err == sql.ErrNoRows {
		exists = false
	} else if err != nil {
		return err
	}
	// Game ids are reused (INTEGER PRIMARY KEY without AUTOINCREMENT): a picture cannot have been uploaded before its
	// game existed, so an older file belongs to an earlier game with the same id and is not adopted.
	addedAt, _ := time.Parse(time.RFC3339, addedRaw) // zero (no limit) if unreadable
	known := map[string]bool{}
	rows, err := s.DB.Query("SELECT file, thumb FROM screenshot WHERE game_id = ?", gid)
	if err != nil {
		return err
	}
	for rows.Next() {
		var f, t string
		if err := rows.Scan(&f, &t); err != nil {
			rows.Close()
			return err
		}
		known[f], known[t] = true, true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil // unreadable folder: leave it
	}
	type file struct {
		name string
		mod  time.Time
		size int64
	}
	var originals []file
	have := map[string]bool{}
	for _, f := range files {
		have[f.Name()] = true
	}
	for _, f := range files {
		name := f.Name()
		fi, err := f.Info()
		if err != nil || f.IsDir() || fi.ModTime().After(cutoff) {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".tmp"):
			if os.Remove(filepath.Join(dir, name)) == nil {
				res.Removed++
			}
		case shots.NameRE.MatchString(name) && strings.HasSuffix(name, "_t.jpg"):
			base := strings.TrimSuffix(name, "_t.jpg")
			if !known[name] && !hasOriginal(have, base) && os.Remove(filepath.Join(dir, name)) == nil { // a thumbnail alone is useless
				res.Removed++
			}
		case shots.NameRE.MatchString(name) && !known[name]:
			originals = append(originals, file{name, fi.ModTime(), fi.Size()})
		}
	}
	sort.Slice(originals, func(i, j int) bool { return originals[i].mod.Before(originals[j].mod) }) // keep the upload order
	for _, o := range originals {
		if exists && !o.mod.Before(addedAt.Add(-adoptTolerance)) && s.adoptShot(gid, dir, o.name, o.size, o.mod) {
			res.Adopted++
			continue
		}
		if err := quarantine(s.ShotDir, gid, dir, o.name); err != nil {
			log.Printf("screenshots sweep: %v", err)
			continue
		}
		res.Quarantined++
	}
	if exists {
		return nil
	}
	os.Remove(dir) // only succeeds when it is empty
	return nil
}

func hasOriginal(have map[string]bool, base string) bool {
	return have[base+".jpg"] || have[base+".png"] || have[base+".webp"]
}

// adoptShot adds the row of an original that lost it (for example to a database restore). false when the file is not
// a usable image: the caller then moves it away instead.
func (s *Server) adoptShot(gid int64, dir, name string, size int64, mod time.Time) bool {
	path := filepath.Join(dir, name)
	info, err := shots.Inspect(path)
	if err != nil {
		return false
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	thumb := shots.ThumbName(base)
	if _, err := os.Stat(filepath.Join(dir, thumb)); err != nil && info.DecodeBytes <= s.decodeBudget() {
		if err := shots.WriteThumb(path, filepath.Join(dir, thumb), info.Orientation); err != nil {
			log.Printf("screenshots sweep: thumbnail of %s: %v", name, err)
		}
	}
	taken := info.Taken
	if taken == "" {
		taken = mod.UTC().Format("2006-01-02T15:04:05Z")
	}
	_, err = s.DB.Exec(`INSERT INTO screenshot(game_id, file, thumb, width, height, bytes, taken_at, pos)
		VALUES (?, ?, ?, ?, ?, ?, ?, (SELECT coalesce(max(pos), 0) + 1 FROM screenshot WHERE game_id = ?))`,
		gid, name, thumb, info.Width, info.Height, size, taken, gid)
	if err != nil {
		log.Printf("screenshots sweep: %v", err)
		return false
	}
	return true
}

// quarantine moves an original (and its thumbnail) to <root>/.orphans/<gid>/. Nothing is overwritten.
func quarantine(root string, gid int64, dir, name string) error {
	dst := filepath.Join(root, orphanDir, strconv.FormatInt(gid, 10))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, n := range []string{name, shots.ThumbName(base)} {
		if _, err := os.Stat(filepath.Join(dst, n)); err == nil {
			return fmt.Errorf("%s already in %s", n, dst)
		}
		if err := os.Rename(filepath.Join(dir, n), filepath.Join(dst, n)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
