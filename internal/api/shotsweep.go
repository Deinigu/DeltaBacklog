package api

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/shots"
)

// shotGrace is how old a leftover must be before the sweep touches it: an upload in progress writes a temporary file
// and, for a moment, a stored file without its row.
const shotGrace = time.Hour

// SweepShots cleans the screenshot folder after crashes and deletes made while the drive was away. Inside each
// numeric game folder it removes temporary files (*.tmp) and generated files that no screenshot row points to, then
// the folder itself when it is empty and the game no longer exists. Only files older than an hour are touched, and
// only when the storage is available; anything else on the drive is left alone. It returns how many files it removed.
func (s *Server) SweepShots() int {
	if !s.shotStorageOK() {
		return 0
	}
	entries, err := os.ReadDir(s.ShotDir)
	if err != nil {
		return 0
	}
	removed := 0
	cutoff := time.Now().Add(-shotGrace)
	for _, e := range entries {
		gid, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() || strconv.FormatInt(gid, 10) != e.Name() {
			continue
		}
		dir := filepath.Join(s.ShotDir, e.Name())
		known := map[string]bool{}
		rows, err := s.DB.Query("SELECT file, thumb FROM screenshot WHERE game_id = ?", gid)
		if err != nil {
			log.Printf("screenshots sweep: %v", err)
			return removed
		}
		for rows.Next() {
			var f, t string
			if rows.Scan(&f, &t) == nil {
				known[f], known[t] = true, true
			}
		}
		rows.Close()
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		left := 0
		for _, f := range files {
			name := f.Name()
			orphan := strings.HasSuffix(name, ".tmp") || (shots.NameRE.MatchString(name) && !known[name])
			fi, err := f.Info()
			if f.IsDir() || !orphan || err != nil || fi.ModTime().After(cutoff) {
				left++
				continue
			}
			if os.Remove(filepath.Join(dir, name)) == nil {
				removed++
			} else {
				left++
			}
		}
		if left == 0 {
			var one int
			if s.DB.QueryRow("SELECT 1 FROM game WHERE id = ?", gid).Scan(&one) != nil {
				os.Remove(dir) // fails (harmlessly) if something appeared meanwhile
			}
		}
	}
	return removed
}
