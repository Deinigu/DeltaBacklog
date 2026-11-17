package expansions

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// MaxSteamFile is the largest Steam userdata file accepted (bytes).
const MaxSteamFile = 5 << 20

// OwnedValue is the ownership an owned Steam expansion gets: the one the library uses for a game it has.
const OwnedValue = "Owned"

// ErrNotSteamFile means the upload is not Steam's userdata JSON with an rgOwnedApps list of integers.
var ErrNotSteamFile = errors.New("not a Steam userdata file")

// ErrTooBig means the upload is over MaxSteamFile.
var ErrTooBig = errors.New("file too big")

// ParseOwned reads the owned app ids from Steam's userdata JSON (https://store.steampowered.com/dynamicstore/userdata/).
// Only rgOwnedApps is read, and only as a list of positive integers; everything else in the file is ignored. The file is
// never stored or logged. The ids are returned without duplicates.
func ParseOwned(r io.Reader) ([]int64, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxSteamFile+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxSteamFile {
		return nil, ErrTooBig
	}
	var doc struct {
		Apps json.RawMessage `json:"rgOwnedApps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Apps) == 0 {
		return nil, ErrNotSteamFile
	}
	var apps []int64
	if err := json.Unmarshal(doc.Apps, &apps); err != nil || apps == nil {
		return nil, ErrNotSteamFile
	}
	seen := make(map[int64]bool, len(apps))
	out := make([]int64, 0, len(apps))
	for _, a := range apps {
		if a <= 0 {
			return nil, ErrNotSteamFile
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// Import is what ImportOwned did.
type Import struct {
	Marked    int // expansions that got the ownership
	Matched   int // owned apps that are expansions of the library (already owned or not)
	Unmatched int // other owned apps: not an expansion of a game in the library (or not on IGDB)
}

// Text is the sentence shown after an import.
func (i Import) Text() string {
	return fmt.Sprintf("Marked %s as owned; %s are not expansions of games in your library (or not on IGDB).",
		plural(i.Marked, "expansion", "expansions"), plural(i.Unmatched, "other owned Steam item", "other owned Steam items"))
}

var steamAppURL = regexp.MustCompile(`store\.steampowered\.com/app/(\d+)`)

// ImportOwned gives the ownership "Owned" to every expansion whose Steam app id is among the owned apps and that has no
// ownership yet. A status or an ownership the owner set is never changed, so it can be run again. Apps that are the
// Steam page of a library game are not counted as unmatched.
func ImportOwned(d *sql.DB, owned []int64) (Import, error) {
	var res Import
	have := make(map[int64]bool, len(owned))
	for _, a := range owned {
		have[a] = true
	}
	tx, err := d.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, steam_app_id, ownership_id IS NULL FROM expansion WHERE steam_app_id IS NOT NULL`)
	if err != nil {
		return res, err
	}
	type row struct {
		id    int64
		empty bool
	}
	var hit []row
	matched := map[int64]bool{}
	for rows.Next() {
		var id, app int64
		var empty bool
		if err := rows.Scan(&id, &app, &empty); err != nil {
			rows.Close()
			return res, err
		}
		if have[app] {
			matched[app] = true
			hit = append(hit, row{id, empty})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	var owner any
	for _, h := range hit {
		if !h.empty {
			continue
		}
		if owner == nil {
			if owner, err = db.GetOrCreate(tx, "ownership", OwnedValue); err != nil {
				return res, err
			}
		}
		if _, err := tx.Exec(`UPDATE expansion SET ownership_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ? AND ownership_id IS NULL`, owner, h.id); err != nil {
			return res, err
		}
		res.Marked++
	}
	res.Matched = len(matched)
	games := map[int64]bool{}
	lrows, err := tx.Query(`SELECT url FROM game_link WHERE kind = 'steam'`)
	if err != nil {
		return res, err
	}
	for lrows.Next() {
		var u string
		if err := lrows.Scan(&u); err != nil {
			lrows.Close()
			return res, err
		}
		if m := steamAppURL.FindStringSubmatch(u); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				games[n] = true
			}
		}
	}
	lrows.Close()
	for _, a := range owned {
		if !matched[a] && !games[a] {
			res.Unmatched++
		}
	}
	return res, tx.Commit()
}
