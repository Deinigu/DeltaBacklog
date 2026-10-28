package meta

import (
	"context"
	"strings"
	"testing"
)

func TestReleasePrecision(t *testing.T) {
	const jan1, dec31 = 1798761600, 1830211199 // 2027-01-01, 2027-12-31 (UTC)
	cases := []struct {
		name string
		g    igdbGame
		want Release
	}{
		{"no date", igdbGame{}, Release{}},
		{"exact day", igdbGame{FirstReleaseDate: jan1, ReleaseDates: []igdbReleaseDate{{jan1, FormatDay}}}, Release{jan1, FormatDay}},
		{"year only keeps its precision", igdbGame{FirstReleaseDate: dec31, ReleaseDates: []igdbReleaseDate{{dec31, FormatYear}, {dec31, FormatYear}}}, Release{dec31, FormatYear}},
		{"platforms with the same date: the finest wins", igdbGame{FirstReleaseDate: jan1, ReleaseDates: []igdbReleaseDate{{jan1, FormatYear}, {jan1, FormatDay}}}, Release{jan1, FormatDay}},
		{"a later platform date does not change the first", igdbGame{FirstReleaseDate: jan1, ReleaseDates: []igdbReleaseDate{{dec31, FormatYear}, {jan1, FormatMonth}}}, Release{jan1, FormatMonth}},
		{"quarter", igdbGame{FirstReleaseDate: jan1, ReleaseDates: []igdbReleaseDate{{jan1, FormatQ1 + 1}}}, Release{jan1, FormatQ1 + 1}},
		{"TBD is no date", igdbGame{FirstReleaseDate: jan1, ReleaseDates: []igdbReleaseDate{{jan1, FormatUnknown}}}, Release{}},
	}
	for _, c := range cases {
		if got := c.g.release(); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestReleaseDatesRequest(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	got, err := c.ReleaseDates(context.Background(), []int64{10})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := got[10]
	if !ok || r.Release.Date != 1830211199 || r.Release.Format != FormatYear || len(r.Platforms) != 1 || r.Platforms[0] != "PC" {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(queries[0], "release_dates.date_format") || strings.Contains(strings.ToLower(queries[0]), "rating") {
		t.Errorf("query: %s", queries[0])
	}
}

func TestCollectionGamesReadDatesAndPlatforms(t *testing.T) {
	var queries []string
	c := collectionFake(t, &queries)
	_, games, err := c.CollectionGames(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if games[0].Release.Format != FormatDay || games[0].Release.Date != 852076800 || len(games[0].Platforms) != 2 || games[0].Platforms[0] != "PC" || games[0].Platforms[1] != "Switch" {
		t.Errorf("zed: %+v", games[0])
	}
	if games[1].Release != (Release{}) { // TBD
		t.Errorf("zed II: %+v", games[1])
	}
}
