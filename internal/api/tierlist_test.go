package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func getTL(t *testing.T, base string, year int) Tierlist {
	t.Helper()
	res, b := do(t, "GET", fmt.Sprintf("%s/api/tierlist/%d", base, year), "")
	if res.StatusCode != 200 {
		t.Fatalf("GET %d = %d %s", year, res.StatusCode, b)
	}
	var tl Tierlist
	if err := json.Unmarshal(b, &tl); err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestTierlist(t *testing.T) {
	srv := newServer(t) // game 1 (Hades II) played 2025 and 2026; game 2 only in childhood
	tl := getTL(t, srv.URL, 2025)
	if len(tl.Tiers) != 6 || tl.Tiers[0].Name != "S" || tl.Tiers[5].Name != "F" || len(tl.Entries) != 0 {
		t.Fatalf("fresh list = %+v", tl)
	}
	s, a := tl.Tiers[0].ID, tl.Tiers[1].ID
	url := srv.URL + "/api/tierlist/2025"

	body := fmt.Sprintf(`{"entries":[{"game_id":1,"tier_id":%d}]}`, a)
	if res, b := do(t, "PUT", url, body); res.StatusCode != 200 {
		t.Fatalf("PUT = %d %s", res.StatusCode, b)
	}
	if got := getTL(t, srv.URL, 2025); len(got.Entries) != 1 || got.Entries[0] != (TierEntry{1, a}) {
		t.Fatalf("after save = %+v", got)
	}
	if got := getTL(t, srv.URL, 2026); len(got.Entries) != 0 {
		t.Fatalf("another year must be untouched: %+v", got)
	}

	// invalid saves change nothing
	bad := map[string]string{
		"not played that year": fmt.Sprintf(`{"entries":[{"game_id":2,"tier_id":%d}]}`, s),
		"unknown tier":         `{"entries":[{"game_id":1,"tier_id":999}]}`,
		"duplicate game":       fmt.Sprintf(`{"entries":[{"game_id":1,"tier_id":%d},{"game_id":1,"tier_id":%d}]}`, s, a),
		"not json":             `nope`,
	}
	for name, body := range bad {
		if res, _ := do(t, "PUT", url, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, res.StatusCode)
		}
		if got := getTL(t, srv.URL, 2025); len(got.Entries) != 1 || got.Entries[0].TierID != a {
			t.Fatalf("%s changed the list: %+v", name, got)
		}
	}
	for _, y := range []string{"abc", "1800", "3000"} {
		if res, _ := do(t, "GET", srv.URL+"/api/tierlist/"+y, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("year %s: status %d", y, res.StatusCode)
		}
	}

	// moving to another tier and clearing
	if res, _ := do(t, "PUT", url, fmt.Sprintf(`{"entries":[{"game_id":1,"tier_id":%d}]}`, s)); res.StatusCode != 200 {
		t.Fatal("move failed")
	}
	if got := getTL(t, srv.URL, 2025); got.Entries[0].TierID != s {
		t.Fatalf("move = %+v", got)
	}
	do(t, "PUT", url, `{"entries":[]}`)
	if got := getTL(t, srv.URL, 2025); len(got.Entries) != 0 {
		t.Fatalf("cleared = %+v", got)
	}
}

func TestTierlistOrderAndOrphans(t *testing.T) {
	srv := newServer(t)
	_, b := do(t, "POST", srv.URL+"/api/games", `{"title":"Second","years":[2025]}`)
	var created struct{ Game Game }
	json.Unmarshal(b, &created)
	id2 := created.Game.ID
	tl := getTL(t, srv.URL, 2025)
	s := tl.Tiers[0].ID
	do(t, "PUT", srv.URL+"/api/tierlist/2025", fmt.Sprintf(`{"entries":[{"game_id":%d,"tier_id":%d},{"game_id":1,"tier_id":%d}]}`, id2, s, s))
	if got := getTL(t, srv.URL, 2025); len(got.Entries) != 2 || got.Entries[0].GameID != id2 {
		t.Fatalf("order inside a tier is kept: %+v", got.Entries)
	}
	// dropping 2025 from the game hides its entry
	do(t, "PATCH", fmt.Sprintf("%s/api/games/%d", srv.URL, id2), `{"years":[2024]}`)
	if got := getTL(t, srv.URL, 2025); len(got.Entries) != 1 || got.Entries[0].GameID != 1 {
		t.Fatalf("orphan entry shown: %+v", got.Entries)
	}
}
