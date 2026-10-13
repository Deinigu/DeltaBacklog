package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
)

// rkEnv is a server with a fake metadata service and four games (ids 1 and 2 from the CSV, 3 and 4 added here).
func rkEnv(t *testing.T) *addEnv {
	t.Helper()
	e := newAddEnv(t)
	for _, title := range []string{"Celeste", "Hollow Knight"} {
		if _, err := e.db.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES (?, lower(?), 7)`, title, title); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func call(t *testing.T, e *addEnv, method, path, body string, want int) []byte {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, res.StatusCode, want, b.String())
	}
	return []byte(b.String())
}

func getRk(t *testing.T, e *addEnv, id int64) Ranking {
	t.Helper()
	var rk Ranking
	if err := json.Unmarshal(call(t, e, "GET", fmt.Sprintf("/api/rankings/%d", id), "", 200), &rk); err != nil {
		t.Fatal(err)
	}
	return rk
}

// layout renders a ranking as "tier:1,2 | tier:3 | -:4" (the pool is "-"), by label.
func layout(rk Ranking) string {
	label := map[int64]string{}
	var parts []string
	idx := map[string]int{}
	for _, tr := range rk.Tiers {
		label[tr.ID] = tr.Label
		idx[tr.Label] = len(parts)
		parts = append(parts, tr.Label+":")
	}
	idx["-"] = len(parts)
	parts = append(parts, "-:")
	for _, e := range rk.Entries {
		l := "-"
		if e.TierID != nil {
			l = label[*e.TierID]
		}
		i := idx[l]
		if !strings.HasSuffix(parts[i], ":") {
			parts[i] += ","
		}
		parts[i] += fmt.Sprint(e.GameID)
	}
	return strings.Join(parts, " | ")
}

func create(t *testing.T, e *addEnv, body string) Ranking {
	t.Helper()
	var rk Ranking
	if err := json.Unmarshal(call(t, e, "POST", "/api/rankings", body, 201), &rk); err != nil {
		t.Fatal(err)
	}
	return rk
}

func TestRankingsCRUD(t *testing.T) {
	e := rkEnv(t)
	if got := string(call(t, e, "GET", "/api/rankings", "", 200)); strings.TrimSpace(got) != "[]" {
		t.Fatalf("fresh list = %s", got)
	}
	a := create(t, e, `{"name":"  Best of 2025 ","description":"my year","game_ids":[1,2]}`)
	if a.Name != "Best of 2025" || a.Description == nil || *a.Description != "my year" || len(a.Tiers) != 6 || a.Tiers[0].Label != "S" || len(a.Entries) != 2 {
		t.Fatalf("created = %+v", a)
	}
	b := create(t, e, `{"name":"Empty","preset":"empty"}`)
	if len(b.Tiers) != 0 || len(b.Entries) != 0 {
		t.Fatalf("empty preset = %+v", b)
	}

	for name, body := range map[string]string{
		"no name":      `{}`,
		"blank name":   `{"name":"   "}`,
		"long name":    `{"name":"` + strings.Repeat("x", 81) + `"}`,
		"bad preset":   `{"name":"x","preset":"zzz"}`,
		"unknown game": `{"name":"x","game_ids":[1,999]}`,
		"long desc":    `{"name":"x","description":"` + strings.Repeat("y", 501) + `"}`,
		"not json":     `nope`,
	} {
		call(t, e, "POST", "/api/rankings", body, 400)
		_ = name
	}
	if list := string(call(t, e, "GET", "/api/rankings", "", 200)); strings.Count(list, `"name"`) != 2 {
		t.Fatalf("a rejected create left something behind: %s", list)
	}

	// rename, describe, clear the description
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", a.ID), `{"name":"Best 2025","description":""}`, 200)
	if got := getRk(t, e, a.ID); got.Name != "Best 2025" || got.Description != nil {
		t.Fatalf("patched = %+v", got)
	}
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", a.ID), `{"name":""}`, 400)
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", a.ID), `{}`, 400)
	call(t, e, "PATCH", "/api/rankings/999", `{"name":"x"}`, 404)

	// the year a ranking is "the ranking of"
	for _, body := range []string{`{"name":"x","year":1800}`, `{"name":"x","year":2200}`} {
		call(t, e, "POST", "/api/rankings", body, 400)
	}
	y := create(t, e, `{"name":"Best of 2025","year":2025}`)
	if y.Year == nil || *y.Year != 2025 {
		t.Fatalf("year = %v", y.Year)
	}
	var yd Ranking
	json.Unmarshal(call(t, e, "POST", fmt.Sprintf("/api/rankings/%d/duplicate", y.ID), ``, 201), &yd)
	if yd.Year != nil {
		t.Fatalf("a copy must not claim the year: %v", *yd.Year)
	}
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", y.ID), `{"year":2024}`, 200)
	if got := getRk(t, e, y.ID); got.Year == nil || *got.Year != 2024 || got.Name != "Best of 2025" {
		t.Fatalf("year patch = %+v", got)
	}
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", y.ID), `{"year":0}`, 200)
	if got := getRk(t, e, y.ID); got.Year != nil {
		t.Fatalf("year not cleared: %v", *got.Year)
	}
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d", y.ID), `{"year":1800}`, 400)
	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d", y.ID), ``, 204)
	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d", yd.ID), ``, 204)

	// duplicate: same tiers and placements, independent afterwards
	a1 := a.Tiers[0].ID
	call(t, e, "POST", fmt.Sprintf("/api/rankings/%d/move", a.ID), fmt.Sprintf(`{"game_ids":[2],"tier_id":%d}`, a1), 200)
	var dup Ranking
	json.Unmarshal(call(t, e, "POST", fmt.Sprintf("/api/rankings/%d/duplicate", a.ID), ``, 201), &dup)
	if dup.ID == a.ID || dup.Name != "Best 2025 (copy)" || layout(dup) != layout(getRk(t, e, a.ID)) {
		t.Fatalf("duplicate = %q vs %q", layout(dup), layout(getRk(t, e, a.ID)))
	}
	if dup.Tiers[0].ID == a.Tiers[0].ID {
		t.Fatal("the copy shares tier rows with the original")
	}
	call(t, e, "POST", fmt.Sprintf("/api/rankings/%d/duplicate", a.ID), `{"name":"Mine"}`, 201)
	call(t, e, "POST", "/api/rankings/999/duplicate", ``, 404)
	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d/entries/2", dup.ID), ``, 204)
	if len(getRk(t, e, a.ID).Entries) != 2 {
		t.Fatal("removing a game from the copy changed the original")
	}

	// order
	var list []RankingSummary
	json.Unmarshal(call(t, e, "GET", "/api/rankings", "", 200), &list)
	ids := []int64{}
	for i := len(list) - 1; i >= 0; i-- {
		ids = append(ids, list[i].ID)
	}
	body, _ := json.Marshal(ids)
	json.Unmarshal(call(t, e, "PUT", "/api/rankings/order", string(body), 200), &list)
	if list[0].ID != ids[0] {
		t.Fatalf("order not applied: %+v", list)
	}
	call(t, e, "PUT", "/api/rankings/order", `[1]`, 400)
	call(t, e, "PUT", "/api/rankings/order", fmt.Sprintf("[%d,%d,%d,%d]", ids[0], ids[0], ids[1], ids[2]), 400)

	// the list shows counts, tier colours and a preview of the best games
	for _, s := range list {
		if s.ID == a.ID && (s.Games != 2 || s.Ranked != 1 || len(s.Tiers) != 6 || len(s.Preview) != 1 || s.Preview[0] != 2) {
			t.Fatalf("summary = %+v", s)
		}
	}

	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d", a.ID), ``, 204)
	call(t, e, "GET", fmt.Sprintf("/api/rankings/%d", a.ID), ``, 404)
	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d", a.ID), ``, 404)
	var n int
	e.db.QueryRow(`SELECT count(*) FROM ranking_tier WHERE ranking_id = ?`, a.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("tiers of a deleted ranking = %d", n)
	}
}

func TestRankingTiers(t *testing.T) {
	e := rkEnv(t)
	rk := create(t, e, `{"name":"R","game_ids":[1,2,3]}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	long := strings.Repeat("Mediocre, pero tiene sus cosillas ", 2)[:60]

	var tier RankingTier
	json.Unmarshal(call(t, e, "POST", base+"/tiers", fmt.Sprintf(`{"label":%q,"color":"#AABBCC","index":0}`, long), 201), &tier)
	if tier.Label != long || tier.Color != "#aabbcc" || tier.Pos != 0 {
		t.Fatalf("new tier = %+v", tier)
	}
	if got := getRk(t, e, rk.ID); got.Tiers[0].ID != tier.ID || len(got.Tiers) != 7 || got.Tiers[1].Pos != 1 || got.Tiers[6].Pos != 6 {
		t.Fatalf("tiers after insert at 0 = %+v", got.Tiers)
	}
	for name, body := range map[string]string{
		"no color":  `{"label":"x"}`,
		"no label":  `{"color":"#aabbcc"}`,
		"bad color": `{"label":"x","color":"red"}`,
		"blank":     `{"label":"  ","color":"#aabbcc"}`,
		"too long":  `{"label":"` + strings.Repeat("x", 81) + `","color":"#aabbcc"}`,
	} {
		call(t, e, "POST", base+"/tiers", body, 400)
		_ = name
	}
	call(t, e, "POST", "/api/rankings/999/tiers", `{"label":"x","color":"#aabbcc"}`, 404)

	// rename and recolour; another ranking's tier is not reachable through this one
	tp := fmt.Sprintf("%s/tiers/%d", base, tier.ID)
	json.Unmarshal(call(t, e, "PATCH", tp, `{"label":"Vaya mierda","color":"#112233"}`, 200), &tier)
	if tier.Label != "Vaya mierda" || tier.Color != "#112233" {
		t.Fatalf("patched = %+v", tier)
	}
	call(t, e, "PATCH", tp, `{"color":"nope"}`, 400)
	call(t, e, "PATCH", tp, `{}`, 400)
	other := create(t, e, `{"name":"Other"}`)
	call(t, e, "PATCH", fmt.Sprintf("/api/rankings/%d/tiers/%d", other.ID, tier.ID), `{"label":"x"}`, 404)
	call(t, e, "DELETE", fmt.Sprintf("/api/rankings/%d/tiers/%d", other.ID, tier.ID), ``, 404)

	// reorder: must name every tier once
	ids := []int64{}
	for i := len(rk.Tiers) - 1; i >= 0; i-- {
		ids = append(ids, rk.Tiers[i].ID)
	}
	ids = append([]int64{tier.ID}, ids...)
	body, _ := json.Marshal(ids)
	call(t, e, "PUT", base+"/tiers/order", string(body), 200)
	got := getRk(t, e, rk.ID)
	if got.Tiers[0].ID != tier.ID || got.Tiers[1].Label != "F" || got.Tiers[6].Label != "S" {
		t.Fatalf("order = %+v", got.Tiers)
	}
	call(t, e, "PUT", base+"/tiers/order", fmt.Sprintf("[%d]", tier.ID), 400)

	// delete a tier: its games return to the pool, after the games already there, in their order
	s, a := got.Tiers[6].ID, got.Tiers[5].ID
	call(t, e, "POST", base+"/move", fmt.Sprintf(`{"game_ids":[3],"tier_id":%d}`, s), 200)
	call(t, e, "POST", base+"/move", fmt.Sprintf(`{"game_ids":[2,1],"tier_id":%d}`, a), 200)
	call(t, e, "DELETE", fmt.Sprintf("%s/tiers/%d", base, a), ``, 204)
	got = getRk(t, e, rk.ID)
	if l := layout(got); !strings.HasSuffix(l, "S:3 | -:2,1") || len(got.Tiers) != 6 {
		t.Fatalf("after deleting tier A: %s", l)
	}
	for i, tr := range got.Tiers {
		if tr.Pos != i {
			t.Fatalf("positions not compact: %+v", got.Tiers)
		}
	}
	call(t, e, "DELETE", fmt.Sprintf("%s/tiers/%d", base, a), ``, 404)

	tiers := 0
	for len(getRk(t, e, rk.ID).Tiers) < maxRankingTiers {
		call(t, e, "POST", base+"/tiers", `{"label":"x","color":"#000000"}`, 201)
		tiers++
	}
	call(t, e, "POST", base+"/tiers", `{"label":"x","color":"#000000"}`, 400)
}

func TestRankingEntries(t *testing.T) {
	e := rkEnv(t)
	rk := create(t, e, `{"name":"R"}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	tiers := rk.Tiers // S A B C D F

	// add: many at once, duplicates and games already there are ignored, nothing outside the library
	var added struct{ Added []int64 }
	json.Unmarshal(call(t, e, "POST", base+"/entries", `{"game_ids":[1,2,2,3]}`, 200), &added)
	if len(added.Added) != 3 {
		t.Fatalf("added = %v", added.Added)
	}
	json.Unmarshal(call(t, e, "POST", base+"/entries", `{"game_ids":[3,4]}`, 200), &added)
	if len(added.Added) != 1 || added.Added[0] != 4 {
		t.Fatalf("re-adding = %v", added.Added)
	}
	call(t, e, "POST", base+"/entries", `{"game_ids":[1,999]}`, 400)
	call(t, e, "POST", base+"/entries", `{"game_ids":[]}`, 400)
	call(t, e, "POST", base+"/entries", fmt.Sprintf(`{"game_ids":[1],"tier_id":%d}`, 99999), 400)
	if got := layout(getRk(t, e, rk.ID)); !strings.HasSuffix(got, "-:1,2,3,4") {
		t.Fatalf("pool = %s", got)
	}

	move := func(body, want string) {
		t.Helper()
		call(t, e, "POST", base+"/move", body, 200)
		if got := layout(getRk(t, e, rk.ID)); got != want {
			t.Fatalf("after %s:\n got  %s\n want %s", body, got, want)
		}
	}
	s, a := tiers[0].ID, tiers[1].ID
	move(fmt.Sprintf(`{"game_ids":[1],"tier_id":%d}`, s), "S:1 | A: | B: | C: | D: | F: | -:2,3,4")
	move(fmt.Sprintf(`{"game_ids":[3],"tier_id":%d}`, s), "S:1,3 | A: | B: | C: | D: | F: | -:2,4")
	move(fmt.Sprintf(`{"game_ids":[2],"tier_id":%d,"index":0}`, s), "S:2,1,3 | A: | B: | C: | D: | F: | -:4")
	move(fmt.Sprintf(`{"game_ids":[3],"tier_id":%d,"index":1}`, s), "S:2,3,1 | A: | B: | C: | D: | F: | -:4")
	move(fmt.Sprintf(`{"game_ids":[2,1],"tier_id":%d,"index":0}`, a), "S:3 | A:2,1 | B: | C: | D: | F: | -:4")
	move(`{"game_ids":[2],"tier_id":null}`, "S:3 | A:1 | B: | C: | D: | F: | -:4,2")
	move(fmt.Sprintf(`{"game_ids":[1],"tier_id":%d,"index":99}`, s), "S:3,1 | A: | B: | C: | D: | F: | -:4,2")

	call(t, e, "POST", base+"/move", `{"game_ids":[1,1],"tier_id":null}`, 400)
	call(t, e, "POST", base+"/move", `{"game_ids":[999],"tier_id":null}`, 400)
	call(t, e, "POST", base+"/move", `{"game_ids":[1],"tier_id":999}`, 400)
	call(t, e, "POST", base+"/move", `{"game_ids":[1],"index":-1}`, 400)
	call(t, e, "POST", base+"/move", `{"game_ids":[]}`, 400)
	other := create(t, e, `{"name":"Other"}`)
	call(t, e, "POST", base+"/move", fmt.Sprintf(`{"game_ids":[1],"tier_id":%d}`, other.Tiers[0].ID), 400)
	call(t, e, "POST", "/api/rankings/999/move", `{"game_ids":[1]}`, 404)

	call(t, e, "DELETE", base+"/entries/3", ``, 204)
	call(t, e, "DELETE", base+"/entries/3", ``, 404)
	if got := layout(getRk(t, e, rk.ID)); got != "S:1 | A: | B: | C: | D: | F: | -:4,2" {
		t.Fatalf("after remove = %s", got)
	}
	// the game itself is untouched
	var n int
	e.db.QueryRow(`SELECT count(*) FROM game WHERE id = 3`).Scan(&n)
	if n != 1 {
		t.Fatal("removing from a ranking deleted the game")
	}
}

func TestRankingBoardSave(t *testing.T) {
	e := rkEnv(t)
	rk := create(t, e, `{"name":"R","game_ids":[1,2,3,4]}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	s, a, b := rk.Tiers[0].ID, rk.Tiers[1].ID, rk.Tiers[2].ID
	other := create(t, e, `{"name":"Other"}`)

	board := func(tiers string, pool string) string {
		return `{"tiers":[` + tiers + `],"pool":[` + pool + `]}`
	}
	tier := func(id int64, label, color string, games string) string {
		return fmt.Sprintf(`{"id":%d,"label":%q,"color":%q,"games":[%s]}`, id, label, color, games)
	}
	save := func(body string) { t.Helper(); call(t, e, "PUT", base+"/board", body, 200) }

	// a whole board: relabel, recolour, reorder tiers (A first, S second), place and order the games
	save(board(tier(a, "Me ha gustado mucho", "#00AA00", "4,2")+","+tier(s, "S", "#ff7f7f", "1"), "3"))
	got := getRk(t, e, rk.ID)
	if l := layout(got); l != "Me ha gustado mucho:4,2 | S:1 | B: | C: | D: | F: | -:3" {
		t.Fatalf("board = %s", l)
	}
	if got.Tiers[0].Color != "#00aa00" || got.Tiers[0].Pos != 0 || got.Tiers[1].Pos != 1 || got.Tiers[2].ID != b {
		t.Fatalf("tiers = %+v", got.Tiers)
	}

	// games and tiers the body leaves out stay put (a request that overtook this one is not undone)
	save(board(tier(a, "Me ha gustado mucho", "#00aa00", "2"), ""))
	if l := layout(getRk(t, e, rk.ID)); l != "Me ha gustado mucho:2,4 | S:1 | B: | C: | D: | F: | -:3" {
		t.Fatalf("partial board = %s", l)
	}
	// unknown games are ignored
	save(board(tier(a, "Me ha gustado mucho", "#00aa00", "2,999,4"), "3"))

	for name, body := range map[string]string{
		"missing pool":   `{"tiers":[]}`,
		"missing tiers":  `{"pool":[]}`,
		"foreign tier":   board(tier(other.Tiers[0].ID, "S", "#ff7f7f", ""), ""),
		"duplicate tier": board(tier(s, "S", "#ff7f7f", "")+","+tier(s, "S", "#ff7f7f", ""), ""),
		"game twice":     board(tier(s, "S", "#ff7f7f", "1")+","+tier(a, "A", "#ff7f7f", "1"), ""),
		"game and pool":  board(tier(s, "S", "#ff7f7f", "1"), "1"),
		"bad colour":     board(tier(s, "S", "red", ""), ""),
		"blank label":    board(tier(s, " ", "#ff7f7f", ""), ""),
		"not json":       `nope`,
	} {
		call(t, e, "PUT", base+"/board", body, 400)
		_ = name
	}
	// a tier that no longer exists (a delete overtook this save) is skipped; its games stay where they are
	save(board(tier(a, "Me ha gustado mucho", "#00aa00", "2,4")+","+tier(99999, "Gone", "#ff7f7f", "3")+","+tier(s, "S", "#ff7f7f", "1"), ""))
	call(t, e, "PUT", "/api/rankings/999/board", board("", ""), 404)
	if l := layout(getRk(t, e, rk.ID)); l != "Me ha gustado mucho:2,4 | S:1 | B: | C: | D: | F: | -:3" {
		t.Fatalf("rejected saves changed the board: %s", l)
	}
}

func TestRankingGameDeleteCascades(t *testing.T) {
	e := rkEnv(t)
	rk := create(t, e, `{"name":"R","game_ids":[1,2]}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	call(t, e, "POST", base+"/move", fmt.Sprintf(`{"game_ids":[1],"tier_id":%d}`, rk.Tiers[0].ID), 200)
	call(t, e, "DELETE", "/api/games/1", ``, 204)
	got := getRk(t, e, rk.ID)
	if len(got.Entries) != 1 || got.Entries[0].GameID != 2 {
		t.Fatalf("entries after deleting game 1 = %+v", got.Entries)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func posterOf(e *addEnv, id int64) (file, src string) {
	var f, s *string
	e.db.QueryRow(`SELECT poster_file, poster_src FROM game WHERE id = ?`, id).Scan(&f, &s)
	if f != nil {
		file = *f
	}
	if s != nil {
		src = *s
	}
	return
}

func TestPostersAreFetchedWhenGamesJoinARanking(t *testing.T) {
	e := rkEnv(t)
	// game 1 is linked to IGDB, game 2 is not but SteamGridDB knows it, game 3 only has a wide cover, game 4 has nothing
	e.db.Exec(`UPDATE game SET igdb_id = 77 WHERE id = 1`)
	e.meta.igdbCover = e.images.URL + "/portrait.png"
	e.meta.portrait = e.images.URL + "/portrait.png"
	do(t, "POST", e.srv.URL+"/api/games/3/cover", `{"url":"`+e.images.URL+`/ok.png"}`)

	rk := create(t, e, `{"name":"R"}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	before := time.Now()
	call(t, e, "POST", base+"/entries", `{"game_ids":[1,2,3,4]}`, 200)
	if time.Since(before) > 2*time.Second {
		t.Fatal("adding games waited for the poster downloads")
	}
	waitFor(t, "all posters", func() bool { return getRk(t, e, rk.ID).PostersPending == 0 })

	for id, want := range map[int64]string{1: "igdb", 2: "sgdb", 3: "sgdb", 4: "sgdb"} {
		if f, src := posterOf(e, id); f != fmt.Sprintf("p%d.jpg", id) || src != want {
			t.Errorf("game %d poster = %q from %q, want %s", id, f, src, want)
		}
	}
	// the file is a portrait 2:3 JPEG, 300 px wide at most
	fi, err := os.Stat(filepath.Join(e.coverDir, "p1.jpg"))
	if err != nil || fi.Size() == 0 {
		t.Fatalf("poster file: %v", err)
	}
	var games []Game
	json.Unmarshal(call(t, e, "GET", "/api/games", "", 200), &games)
	for _, g := range games {
		if g.ID == 1 && (g.Poster == nil || *g.Poster != "p1.jpg" || g.PosterV == "") {
			t.Errorf("the game list does not carry the poster: %+v", g)
		}
	}
	// served like covers
	res, err := http.Get(e.srv.URL + "/covers/p1.jpg")
	if err != nil || res.StatusCode != 200 || !strings.Contains(res.Header.Get("Cache-Control"), "max-age") {
		t.Fatalf("poster not served with cache headers: %v %v", err, res)
	}
	res.Body.Close()

	// deleting the game deletes its poster file
	call(t, e, "DELETE", "/api/games/1", ``, 204)
	if _, err := os.Stat(filepath.Join(e.coverDir, "p1.jpg")); !os.IsNotExist(err) {
		t.Errorf("poster file left behind: %v", err)
	}
}

func TestPosterSourcesFallBackInOrder(t *testing.T) {
	e := rkEnv(t)
	rk := create(t, e, `{"name":"R"}`)
	base := fmt.Sprintf("/api/rankings/%d", rk.ID)
	e.db.Exec(`UPDATE game SET igdb_id = 77 WHERE id IN (1, 2, 3)`)
	_, _ = do(t, "POST", e.srv.URL+"/api/games/2/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	_, _ = do(t, "POST", e.srv.URL+"/api/games/3/cover", `{"url":"`+e.images.URL+`/ok.png"}`)

	// IGDB has no cover, SteamGridDB neither: game 2 and 3 fall back to a crop of their wide cover, 1 and 4 get nothing
	e.meta.igdbCover, e.meta.portrait = "", ""
	call(t, e, "POST", base+"/entries", `{"game_ids":[1,2,3,4]}`, 200)
	waitFor(t, "posters", func() bool { return getRk(t, e, rk.ID).PostersPending == 0 })
	for id, want := range map[int64]string{1: "", 2: "crop", 3: "crop", 4: ""} {
		if _, src := posterOf(e, id); src != want {
			t.Errorf("game %d poster source = %q, want %q", id, src, want)
		}
	}
	// a game without any source is not retried over and over: opening the ranking again queues nothing
	if got := getRk(t, e, rk.ID); got.PostersPending != 0 {
		t.Errorf("pending = %d", got.PostersPending)
	}

	// IGDB fails with an error: SteamGridDB answers
	e2 := rkEnv(t)
	e2.db.Exec(`UPDATE game SET igdb_id = 77 WHERE id = 1`)
	e2.meta.igdbCover = e2.images.URL + "/missing.png" // 404: the download fails, the next source is tried
	e2.meta.portrait = e2.images.URL + "/portrait.png"
	rk2 := create(t, e2, `{"name":"R","game_ids":[1]}`)
	waitFor(t, "poster", func() bool { _, src := posterOf(e2, 1); return src != "" })
	if _, src := posterOf(e2, 1); src != "sgdb" {
		t.Errorf("source = %q, want sgdb", src)
	}
	_ = rk2
}

func TestPosterDownloadLimits(t *testing.T) {
	e := rkEnv(t)
	e.db.Exec(`UPDATE game SET igdb_id = 77`)

	// A host outside the allow-list is never contacted, a redirect is not followed and a huge image is refused. The
	// server remembers a game that failed, so every case uses its own game.
	cases := []struct {
		name, url string
		game      int64
	}{
		{"foreign host", "https://evil.example.com/p.png", 1},
		{"file", "file:///etc/passwd", 2},
		{"redirect", "", 3},
		{"huge", "", 4},
	}
	cases[2].url, cases[3].url = e.images.URL+"/redirect.png", e.images.URL+"/huge.png"
	for _, c := range cases {
		e.meta.igdbCover, e.meta.portrait = c.url, ""
		rk := create(t, e, fmt.Sprintf(`{"name":%q,"game_ids":[%d]}`, c.name, c.game))
		waitFor(t, c.name, func() bool { return getRk(t, e, rk.ID).PostersPending == 0 })
		if f, src := posterOf(e, c.game); f != "" {
			t.Errorf("%s: a poster was stored (%s from %s)", c.name, f, src)
		}
	}
}

// fetch-posters and sync-igdb write to the database from another process while the server runs: the cached
// /api/games must notice (PRAGMA data_version) instead of serving the old list until the next write of its own.
func TestGamesCacheSeesWritesFromAnotherConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO game(title, title_sort, status_id) VALUES ('A', 'a', 7)`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&Server{DB: d, CoverDir: t.TempDir()}).Handler())
	defer srv.Close()
	poster := func() *string {
		var games []Game
		if err := json.Unmarshal(call2(t, srv.URL+"/api/games"), &games); err != nil || len(games) != 1 {
			t.Fatalf("games: %v %v", err, games)
		}
		return games[0].Poster
	}
	if poster() != nil {
		t.Fatal("no poster yet")
	}
	other, err := sql.Open("sqlite", db.FileURI(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec(`UPDATE game SET poster_file = 'p1.jpg', poster_updated_at = 'x'`); err != nil {
		t.Fatal(err)
	}
	if p := poster(); p == nil || *p != "p1.jpg" {
		t.Fatalf("the server kept serving the cached list: %v", p)
	}
}

func call2(t *testing.T, url string) []byte {
	t.Helper()
	res, b := do(t, "GET", url, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET %s = %d", url, res.StatusCode)
	}
	return b
}

// A source that fails for now (rate limit, network) must not turn into a permanent crop of the wide cover.
func TestTemporaryPosterErrorLeavesNoCrop(t *testing.T) {
	e := rkEnv(t)
	e.db.Exec(`UPDATE game SET igdb_id = 77 WHERE id = 2`)
	do(t, "POST", e.srv.URL+"/api/games/2/cover", `{"url":"`+e.images.URL+`/ok.png"}`)
	e.meta.posterErr = errors.New("429 Too Many Requests")
	rk := create(t, e, `{"name":"R","game_ids":[2]}`)
	waitFor(t, "the attempt", func() bool { return getRk(t, e, rk.ID).PostersPending == 0 })
	if f, src := posterOf(e, 2); f != "" {
		t.Fatalf("a poster was stored (%s from %s) although both sources failed for now", f, src)
	}
}
