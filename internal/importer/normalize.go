package importer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// splitList splits "a, b (c, d), e" on commas outside parentheses, trims and drops
// empties and case-insensitive duplicates, keeping the first spelling.
func splitList(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])

	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		k := strings.ToLower(p)
		if p == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

// Play is one entry of the old Years_played text: a year or a named period.
type Play struct {
	Year   int
	Period string // "childhood" | "always"; empty when Year is set
}

// parseYears parses "2025,2026", "Childhood,2023", "Always"... Unknown tokens are returned as bad.
func parseYears(s string) (plays []Play, bad []string) {
	seen := map[Play]bool{}
	for _, tok := range splitList(s) {
		var p Play
		switch low := strings.ToLower(tok); low {
		case "childhood", "always":
			p = Play{Period: low}
		default:
			y, err := strconv.Atoi(tok)
			if err != nil || y < 1970 || y > 2100 {
				bad = append(bad, tok)
				continue
			}
			p = Play{Year: y}
		}
		if !seen[p] {
			seen[p] = true
			plays = append(plays, p)
		}
	}
	return plays, bad
}

// parseBoxart extracts the cover location from a NocoDB attachment JSON array.
// Remote attachments carry "url"; uploaded ones a relative "path" under the NocoDB base URL.
func parseBoxart(raw, nocodbBase string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var atts []struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &atts); err != nil {
		return "", fmt.Errorf("boxart json: %w", err)
	}
	for _, a := range atts {
		if a.URL != "" {
			return a.URL, nil
		}
		if a.Path != "" && nocodbBase != "" {
			return strings.TrimRight(nocodbBase, "/") + "/" + strings.TrimLeft(a.Path, "/"), nil
		}
	}
	return "", nil
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "true", "1", "yes":
		return true
	}
	return false
}
