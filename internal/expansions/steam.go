package expansions

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"unicode"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// MaxSteamFile is the largest Steam userdata file accepted (bytes).
const MaxSteamFile = 5 << 20

// OwnedValue is the ownership an owned Steam expansion gets: the one the library uses for a game it has.
const OwnedValue = "Owned"

// ErrNotSteamFile means the upload is not Steam's userdata JSON with an rgOwnedApps list of integers.
var ErrNotSteamFile = errors.New("not a Steam userdata file")

// ErrNoOwnedApps means the file is Steam's, but its rgOwnedApps list is empty: what Steam answers to a browser that is not
// logged in. Nothing would be marked, so nothing is started.
var ErrNoOwnedApps = errors.New("the Steam file lists no owned apps")

// ErrTooBig means the upload is over MaxSteamFile.
var ErrTooBig = errors.New("file too big")

// ParseOwned reads the owned app ids from Steam's userdata JSON (https://store.steampowered.com/dynamicstore/userdata/).
// Only rgOwnedApps is read, and only as a list of positive integers; everything else in the file is ignored. The file is
// never stored or logged. The ids are returned without duplicates; an empty list is ErrNoOwnedApps. The page saved by a browser as a web page (the JSON
// inside <pre>, HTML-escaped) is read too.
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
	var apps []int64
	if err := json.Unmarshal(raw, &doc); err == nil {
		if len(doc.Apps) == 0 || json.Unmarshal(doc.Apps, &apps) != nil {
			return nil, ErrNotSteamFile
		}
	} else if apps, err = ownedInPage(raw); err != nil {
		return nil, err
	}
	if apps == nil {
		return nil, ErrNotSteamFile
	}
	if len(apps) == 0 {
		return nil, ErrNoOwnedApps
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

// ownedInPage reads the list from the page saved by a browser as a web page ("Save page as… Webpage"): the JSON sits inside
// the HTML (in <pre>, with &quot; and the like), maybe after a style or script with braces of its own. Only the array that
// follows the "rgOwnedApps" key is decoded.
func ownedInPage(raw []byte) ([]int64, error) {
	text := []byte(html.UnescapeString(string(raw)))
	i := bytes.Index(text, []byte(`"rgOwnedApps"`))
	if i < 0 || bytes.Contains(text[i+1:], []byte(`"rgOwnedApps"`)) { // none, or two: not the file
		return nil, ErrNotSteamFile
	}
	rest := bytes.TrimLeftFunc(text[i+len(`"rgOwnedApps"`):], unicode.IsSpace)
	if len(rest) == 0 || rest[0] != ':' {
		return nil, ErrNotSteamFile
	}
	var apps []int64
	if err := json.NewDecoder(bytes.NewReader(rest[1:])).Decode(&apps); err != nil {
		return nil, ErrNotSteamFile
	}
	return apps, nil
}

// Import is what ImportOwned did.
type Import struct {
	Marked    int // expansions (and DLC the owner added) that got the ownership
	DLC       int // owned DLC (IGDB game_type 1) the owner has not added: marked "Owned on Steam" only, offered first in their game's details
	Already   int // owned expansions and added DLC that already had an ownership (the owner's, or a previous import's): left as they are
	Matched   int // owned apps that are expansions of the library (already owned or not)
	Unmatched int // other owned apps: not an expansion of a game in the library (or not on IGDB)
	// NewlyMarked are the row ids of the DLC whose Steam mark went from 0 to 1 in this import: the job then reads their times
	// and posters, which the background run skipped while they were untouched.
	NewlyMarked []int64 `json:"-"`
}

// Text is the sentence shown after an import: "Marked 2 expansions as owned (3 already had an ownership); 40 other owned
// Steam items are not expansions of games in your library (or not on IGDB)." The part in brackets only when some had one,
// so a second run that marks nothing still shows it found them.
func (i Import) Text() string {
	already := ""
	if i.Already > 0 {
		already = fmt.Sprintf(" (%d already had an ownership)", i.Already)
	}
	switch {
	case i.DLC == 1:
		already += "; 1 DLC you own on Steam is offered first in its game's details"
	case i.DLC > 1:
		already += fmt.Sprintf("; %d DLC you own on Steam are offered first in their games' details", i.DLC)
	}
	rest := "are not expansions of games in your library"
	if i.Unmatched == 1 {
		rest = "is not an expansion of a game in your library"
	}
	return fmt.Sprintf("Marked %s as owned%s; %s %s (or not on IGDB).",
		plural(i.Marked, "expansion", "expansions"), already, plural(i.Unmatched, "other owned Steam item", "other owned Steam items"), rest)
}

var steamAppURL = regexp.MustCompile(`store\.steampowered\.com/app/(\d+)`)

// ImportOwned gives the ownership "Owned" to every expansion whose Steam app id is among the owned apps and that has no
// ownership yet. A DLC gets it only when the owner added it (a status, notes, years or a time of their own: Steam lists cosmetic
// packs and soundtracks as DLC too); any other owned DLC is only marked steam_owned, and the mark follows the uploaded list (a DLC
// no longer in it loses it). A status or an ownership the owner set is never changed, so it can be run again. Apps that are the
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
	// an expansion that is also a game of the library is matched (it is not "unmatched") but not marked: its card is its record
	rows, err := tx.Query(`SELECT e.id, e.steam_app_id, e.ownership_id IS NULL, ` + IsGameSQL + `, e.kind = 'dlc' AND (` + untracked + `), e.steam_owned, e.hidden
		FROM expansion e WHERE e.steam_app_id IS NOT NULL`)
	if err != nil {
		return res, err
	}
	type row struct {
		id    int64
		empty bool
		dlc   bool
		hide  bool // hidden: an owned DLC the owner hid is marked but not offered, so it is not counted
	}
	var hit []row
	var unmark []int64 // marked owned on Steam before, not in this list (a refund)
	matched := map[int64]bool{}
	for rows.Next() {
		var id, app int64
		var empty, isGame, dlc, marked, hide bool // dlc: a DLC the owner has not added
		if err := rows.Scan(&id, &app, &empty, &isGame, &dlc, &marked, &hide); err != nil {
			rows.Close()
			return res, err
		}
		if marked && !have[app] {
			unmark = append(unmark, id)
		}
		if have[app] {
			matched[app] = true
			if !isGame {
				hit = append(hit, row{id, empty, dlc, hide})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, id := range unmark {
		if _, err := tx.Exec(`UPDATE expansion SET steam_owned = 0 WHERE id = ?`, id); err != nil {
			return res, err
		}
	}
	var owner any
	for _, h := range hit {
		// every owned DLC is marked as owned on Steam (the drawer offers it first). One the owner has not added gets nothing more:
		// Steam lists cosmetic packs and soundtracks as DLC too, and they would flood the owner's list.
		r, err := tx.Exec(`UPDATE expansion SET steam_owned = 1 WHERE id = ? AND kind = 'dlc' AND steam_owned = 0`, h.id)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.NewlyMarked = append(res.NewlyMarked, h.id)
		}
		if h.dlc {
			if !h.hide {
				res.DLC++
			}
			continue
		}
		if !h.empty {
			res.Already++
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
