package meta

import (
	"strings"
	"unicode"
)

var romans = map[string]string{
	"ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9", "x": "10",
	"xi": "11", "xii": "12", "xiii": "13", "xiv": "14", "xv": "15", "xvi": "16",
}

// foldRune maps the accented Latin letters games use onto plain ones.
var foldRune = strings.NewReplacer("à", "a", "á", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "ç", "c", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ñ", "n", "ò", "o", "ó", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o", "ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ý", "y", "ÿ", "y", "ß", "ss", "æ", "ae", "œ", "oe", "&", " and ")

// titleTokens lowercases a title, drops punctuation and the leading "the", and writes roman numerals as digits,
// so "The Witcher III: Wild Hunt" and "witcher 3 wild hunt" compare equal.
func titleTokens(t string) []string {
	t = foldRune.Replace(strings.ToLower(t))
	f := strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(f) > 1 && f[0] == "the" {
		f = f[1:]
	}
	for i, w := range f {
		if n, ok := romans[w]; ok && i > 0 {
			f[i] = n
		}
	}
	return f
}

func isNumber(w string) bool {
	for _, r := range w {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return w != ""
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// TitleScore rates how likely two game titles are the same game, 0 to 100. Case, punctuation, accents, a leading
// "the" and roman numerals do not matter. Titles that differ in their numbers ("Resident Evil 2" and "3") are
// capped at 59: close in spelling, different games.
func TitleScore(a, b string) int {
	ta, tb := titleTokens(a), titleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	sa, sb := strings.Join(ta, " "), strings.Join(tb, " ")
	if sa == sb {
		return 100
	}
	ra, rb := []rune(sa), []rune(sb)
	score := 100 - 100*levenshtein(ra, rb)/max(len(ra), len(rb))
	nums := func(ts []string) string {
		var n []string
		for _, w := range ts {
			if isNumber(w) {
				n = append(n, w)
			}
		}
		return strings.Join(n, ",")
	}
	if nums(ta) != nums(tb) {
		return min(score, 59)
	}
	// "Dark Souls III" and "Dark Souls III: The Ringed City": one title starts the other. Likely related, not
	// certain enough to link on its own.
	short, long := ta, tb
	if len(short) > len(long) {
		short, long = long, short
	}
	if strings.Join(long[:len(short)], " ") == strings.Join(short, " ") {
		score = max(score, 75)
	}
	return max(score, 0)
}
