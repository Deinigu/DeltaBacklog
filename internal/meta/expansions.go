package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ExpansionType is IGDB's game_type of an expansion. Standalone expansions (4) stay normal games, and small DLC (1),
// packs (13), cosmetics, soundtracks and season passes are not tracked.
const ExpansionType = 2

// ExpansionBatch is how many parent games one Expansions request asks for.
const ExpansionBatch = 100

// steamSource is IGDB's external game source (formerly external_games.category) of Steam.
const steamSource = 1

// Expansion is one IGDB expansion of a game.
type Expansion struct {
	IGDBID     int64
	Name       string
	Release    int64  // unix seconds (UTC), 0 when IGDB has none
	Year       int    // 0 when IGDB has no release date
	CoverID    string // IGDB image id ("" when none)
	SteamAppID int64  // 0 when IGDB lists no Steam store page
}

// igdbRef is an IGDB reference field that is an id, or an object when it was expanded.
type igdbRef int64

func (r *igdbRef) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*r = igdbRef(n)
		return nil
	}
	var o struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil // an unknown shape is "no source", never an error
	}
	*r = igdbRef(o.ID)
	return nil
}

type igdbExternal struct {
	UID      string  `json:"uid"`
	Category int     `json:"category"`             // the old field: 1 = Steam
	Source   igdbRef `json:"external_game_source"` // the new one: 1 = Steam
}

type igdbExpansionParent struct {
	ID         int64 `json:"id"`
	Expansions []struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		GameType         int    `json:"game_type"`
		FirstReleaseDate int64  `json:"first_release_date"`
		Cover            struct {
			ImageID string `json:"image_id"`
		} `json:"cover"`
		ExternalGames []igdbExternal `json:"external_games"`
	} `json:"expansions"`
}

// steamAppID is the Steam app id among a game's external games (0 when none): the source is Steam by the new
// external_game_source or the old category, and the uid is the app id.
func steamAppID(list []igdbExternal) int64 {
	for _, e := range list {
		if e.Category != steamSource && int64(e.Source) != steamSource {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(e.UID), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// Expansions reads the expansions (game_type 2 only) of up to ExpansionBatch games in one request. A parent IGDB does not
// return is absent; one without expansions is present with an empty list.
func (c *Client) Expansions(ctx context.Context, igdbIDs []int64) (map[int64][]Expansion, error) {
	out := map[int64][]Expansion{}
	if len(igdbIDs) == 0 {
		return out, nil
	}
	if len(igdbIDs) > ExpansionBatch {
		return nil, fmt.Errorf("at most %d games per request", ExpansionBatch)
	}
	var games []igdbExpansionParent
	q := fmt.Sprintf("fields expansions.name,expansions.game_type,expansions.first_release_date,expansions.cover.image_id,expansions.external_games.*; where id = (%s); limit %d;", idList(igdbIDs), len(igdbIDs))
	if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	for _, g := range games {
		list := []Expansion{}
		for _, x := range g.Expansions {
			if x.ID <= 0 || x.GameType != ExpansionType || strings.TrimSpace(x.Name) == "" {
				continue
			}
			e := Expansion{IGDBID: x.ID, Name: strings.TrimSpace(x.Name), Release: x.FirstReleaseDate, CoverID: x.Cover.ImageID, SteamAppID: steamAppID(x.ExternalGames)}
			if x.FirstReleaseDate > 0 {
				e.Year = time.Unix(x.FirstReleaseDate, 0).UTC().Year()
			}
			list = append(list, e)
		}
		out[g.ID] = list
	}
	return out, nil
}
