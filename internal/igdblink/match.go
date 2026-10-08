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
	lowProposals   = 3 // IGDB's first results kept as weak proposals when nothing links
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
// showing to the owner (best first, at most five): those scoring >= MinScore, plus IGDB's first results as weak proposals
// when the game is not linked.
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
		sc := meta.TitleScore(title, c.Name)
		for _, alt := range c.AltNames { // localized names rescue titles such as "La ira de la Bruja Blanca"
			sc = max(sc, meta.TitleScore(title, alt))
		}
		scored = append(scored, Scored{Candidate: c, Score: sc, Overlap: overlaps(platforms, c.Platforms)})
	}
	byRelevance := append([]Scored(nil), scored...) // IGDB's own order, for the low-score proposals below
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Overlap && !scored[j].Overlap
	})
	res := Result{Bucket: NotFound}
	var strong []Scored
	for _, s := range scored {
		if s.Score >= MinScore {
			res.Bucket = Uncertain
			if len(res.Candidates) < maxCandidates {
				res.Candidates = append(res.Candidates, s)
			}
		}
		if s.Score >= ConfidentScore && s.Overlap {
			strong = append(strong, s)
		}
	}
	if len(strong) > 1 { // several strong candidates: a single exact name ("Mirror's Edge", not "The Mirror's Edge") wins
		var exact []Scored
		for _, s := range strong {
			if meta.SameName(title, s.Name) {
				exact = append(exact, s)
			}
		}
		if len(exact) == 1 {
			strong = exact
		}
	}
	if len(strong) == 1 {
		for i := range res.Candidates {
			if res.Candidates[i].IGDBID == strong[0].IGDBID {
				res.Bucket, res.Best = Confident, &res.Candidates[i]
				return res
			}
		}
	}
	// Not linked: keep IGDB's top results by relevance even when weak. They are only proposals.
	for i := 0; i < len(byRelevance) && i < lowProposals; i++ {
		have := false
		for _, c := range res.Candidates {
			have = have || c.IGDBID == byRelevance[i].IGDBID
		}
		if !have && len(res.Candidates) < maxCandidates {
			res.Candidates = append(res.Candidates, byRelevance[i])
		}
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool { return res.Candidates[i].Score > res.Candidates[j].Score })
	return res
}
