package igdblink

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Times the owner typed or took from HowLongToBeat are never replaced by IGDB's; IGDB's own are, and say so.
func TestEnrichNeverOverwritesOwnTimes(t *testing.T) {
	d := openDB(t)
	type row struct {
		id  int64
		src string
	}
	var rows []row
	f := &fakeEnricher{details: map[int64]meta.Details{}, ttb: map[int64]meta.TimeToBeat{}}
	for i, src := range []string{"manual", "hltb", "igdb", ""} {
		id := addGame(t, d, "Hades "+src)
		link(t, d, id, int64(i+1))
		if src != "" {
			d.Exec("UPDATE game SET time_to_beat_normally = 1000, time_to_beat_src = ? WHERE id = ?", src, id)
		}
		rows = append(rows, row{id, src})
		det := hades()
		det.IGDBID = int64(i + 1)
		f.details[det.IGDBID] = det
		f.ttb[det.IGDBID] = meta.TimeToBeat{Normally: 7200} // 120 minutes
	}
	if _, err := Enrich(context.Background(), d, f, EnrichOptions{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		var n int
		var src sql.NullString
		d.QueryRow("SELECT time_to_beat_normally, time_to_beat_src FROM game WHERE id = ?", r.id).Scan(&n, &src)
		switch r.src {
		case "manual", "hltb":
			if n != 1000 || src.String != r.src {
				t.Errorf("%s times were overwritten: %d %v", r.src, n, src)
			}
		default:
			if n != 120 || src.String != "igdb" {
				t.Errorf("IGDB's time for a %q game: %d %v", r.src, n, src)
			}
		}
	}
}
