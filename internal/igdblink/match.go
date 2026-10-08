// Package igdblink links library games to their IGDB entry: it searches IGDB, sorts every game into a confident,
// uncertain or not-found bucket, and stores the outcome. Only the link (igdb_id) is stored, never IGDB's scores.
package igdblink

import (
	"sort"
	"strings"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Thresholds of the buckets (scores are meta.TitleScore, 0-100).
const (
	ConfidentScore = 90
	MinScore       = 60
	maxCandidates  = 5
)

// Bucket is the outcome of matching one game.
type Bucket string

const (
	Confident Bucket = "confident"
	Uncertain Bucket = "uncertain"
	NotFound  Bucket = "not_found"
)

// Scored is an IGDB candidate with its title score and whether it shares a platform with the game.
type Scored struct {
	meta.Candidate
	Score   int
	Overlap bool
}

// Result is the matching outcome of one game: the bucket, the winner (Confident only) and the candidates worth
// showing to the owner (score >= MinScore, best first, at most five).
type Result struct {
	Bucket     Bucket
	Best       *Scored
	Candidates []Scored
}

// platformAliases maps IGDB's names onto the library's short names (the same table as web/static/js/platforms.js).
var platformAliases = map[string]string{
	"pcmicrosoftwindows": "pc", "windows": "pc", "microsoftwindows": "pc",
	"nintendoswitch": "switch", "nintendoswitch2": "switch2", "nsw": "switch",
	"wiiu": "wiiu", "nintendowiiu": "wiiu", "nintendowii": "wii",
	"nintendods": "nds", "nintendo3ds": "3ds", "new3dsnintendo": "3ds", "nintendogamecube": "ngc", "gamecube": "ngc",
	"nintendoentertainmentsystem": "nes", "playstation": "ps1", "ps": "ps1", "playstation2": "ps2", "playstation3": "ps3",
	"playstation4": "ps4", "playstation5": "ps5", "playstationportable": "psp", "playstationvita": "vita", "psvita": "vita",
	"xboxone": "xone", "xbox360": "x360", "xboxseriesxs": "seriesxs", "oculusquest": "quest", "oculusquest2": "quest",
	"metaquest2": "quest", "metaquest3": "quest", "mac": "mac", "macos": "mac",
}

// PlatformKey reduces a platform name to letters and digits, folding IGDB's long names onto the library's.
func PlatformKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if a, ok := platformAliases[k]; ok {
		return a
	}
	return k
}

func overlaps(game []string, cand []string) bool {
	have := map[string]bool{}
	for _, p := range game {
		have[PlatformKey(p)] = true
	}
	for _, p := range cand {
		if have[PlatformKey(p)] {
			return true
		}
	}
	return false
}

// Classify sorts one game's IGDB search results into a bucket.
//   - confident: exactly one candidate scores >= 90 and shares a platform with the game;
//   - uncertain: something scores >= 60 but that rule does not hold (a weaker title match, no shared platform,
//     or several equally good candidates);
//   - not found: nothing scores >= 60.
func Classify(title string, platforms []string, cands []meta.Candidate) Result {
	scored := make([]Scored, 0, len(cands))
	for _, c := range cands {
		scored = append(scored, Scored{Candidate: c, Score: meta.TitleScore(title, c.Name), Overlap: overlaps(platforms, c.Platforms)})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Overlap && !scored[j].Overlap
	})
	res := Result{Bucket: NotFound}
	strong := 0
	for _, s := range scored {
		if s.Score >= MinScore && len(res.Candidates) < maxCandidates {
			res.Candidates = append(res.Candidates, s)
		}
		if s.Score >= ConfidentScore && s.Overlap {
			strong++
		}
	}
	if len(res.Candidates) == 0 {
		return res
	}
	res.Bucket = Uncertain
	if strong == 1 {
		for i := range res.Candidates {
			if c := res.Candidates[i]; c.Score >= ConfidentScore && c.Overlap {
				res.Bucket, res.Best = Confident, &res.Candidates[i]
				break
			}
		}
	}
	return res
}
