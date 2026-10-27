package meta

import (
	"context"
	"fmt"
)

// IGDB release_dates.date_format values. The date of a year, quarter or month release is a day inside that period,
// so a stored date is only meaningful together with its format.
const (
	FormatDay     = 0 // exact day
	FormatMonth   = 1
	FormatYear    = 2
	FormatQ1      = 3 // Q1..Q4 are 3..6
	FormatQ4      = 6
	FormatUnknown = 7 // "TBD": IGDB has no date to give
)

// Release is the first release date of a game: Date is unix seconds (UTC), 0 when IGDB has none or says TBD, and
// Format says how exact it is (a game "due 2027" has a date inside 2027 and FormatYear).
type Release struct {
	Date   int64
	Format int
}

// release picks the date_format of the release that first_release_date comes from. IGDB's own precision is kept, so
// "2027" is never shown as a day.
func (g igdbGame) release() Release {
	if g.FirstReleaseDate <= 0 {
		return Release{}
	}
	format, matched, tbd := FormatDay, false, false
	for _, r := range g.ReleaseDates {
		switch {
		case r.Date != g.FirstReleaseDate:
		case r.DateFormat == FormatUnknown:
			tbd = true
		case !matched || r.DateFormat < format: // the finest format of the platforms that share the date
			format, matched = r.DateFormat, true
		}
	}
	if tbd && !matched {
		return Release{}
	}
	return Release{Date: g.FirstReleaseDate, Format: format}
}

// ReleaseOf is the release (and the platform names) of one game, as ReleaseDates reads them.
type ReleaseOf struct {
	Release   Release
	Platforms []string
}

// ReleaseDates reads the first release date of up to ~50 games in one request. Games IGDB no longer knows are absent.
func (c *Client) ReleaseDates(ctx context.Context, ids []int64) (map[int64]ReleaseOf, error) {
	out := map[int64]ReleaseOf{}
	if len(ids) == 0 {
		return out, nil
	}
	var games []igdbGame
	q := fmt.Sprintf("fields id,first_release_date,release_dates.date,release_dates.date_format,platforms.abbreviation,platforms.name; where id = (%s); limit %d;", idList(ids), len(ids))
	if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	for _, g := range games {
		out[g.ID] = ReleaseOf{Release: g.release(), Platforms: g.candidate().Platforms}
	}
	return out, nil
}


