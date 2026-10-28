package meta

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CollectionRef is an IGDB collection a game belongs to.
type CollectionRef struct {
	ID   int64
	Name string
}

// GameLinks is how IGDB relates one game to others: its collections, and the game it is another version of (see
// igdbGame.parent: never the game a standalone expansion expands or a remake remakes).
type GameLinks struct {
	Collections []CollectionRef
	ParentID    int64 // 0 = none
}

// CollectionGame is one game of an IGDB collection, as much as the Series page shows of a game the library lacks.
type CollectionGame struct {
	IGDBID    int64
	Name      string
	Year      int // 0 when IGDB has no release date
	GameType  int
	ParentID  int64    // the game this one is another version of; 0 = none
	CoverID   string   // IGDB image id ("" when the game has no cover)
	Release   Release  // the full first release date (zero when IGDB has none or says TBD)
	Platforms []string // IGDB platforms, abbreviated
}

// realGameTypes are the IGDB game_type values that count as a game of a series: 0 main game, 4 standalone expansion,
// 8 remake, 9 remaster. DLC (1), expansions (2), bundles (3), mods (5), episodes (6), seasons (7), expanded editions (10),
// ports (11), forks (12), packs (13) and updates (14) are left out.
var realGameTypes = map[int]bool{0: true, 4: true, 8: true, 9: true}

// IsRealGameType reports whether a game_type is one the Series page lists.
func IsRealGameType(t int) bool { return realGameTypes[t] }

// collectionPage is how many games one request reads (IGDB allows 500); maxCollectionGames bounds a collection.
const (
	collectionPage     = 500
	maxCollectionGames = 1000
)

// GameLinks reads the collections and the parent of up to ~50 games in one request. Games IGDB does not return are
// absent; a game without collections or parent is present with empty values.
func (c *Client) GameLinks(ctx context.Context, igdbIDs []int64) (map[int64]GameLinks, error) {
	out := map[int64]GameLinks{}
	if len(igdbIDs) == 0 {
		return out, nil
	}
	var games []igdbGame
	q := fmt.Sprintf("fields collections.id,collections.name,game_type,parent_game,version_parent; where id = (%s); limit %d;", idList(igdbIDs), len(igdbIDs))
	if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
		return nil, err
	}
	for _, g := range games {
		l := GameLinks{ParentID: g.parent()}
		for _, col := range g.Collections {
			l.Collections = append(l.Collections, CollectionRef{ID: col.ID, Name: col.Name})
		}
		out[g.ID] = l
	}
	return out, nil
}

// CollectionGames lists the real games of one IGDB collection (see realGameTypes) and the collection's name. The
// shared IGDB pacing applies to every request.
func (c *Client) CollectionGames(ctx context.Context, collectionID int64) (string, []CollectionGame, error) {
	var out []CollectionGame
	name := ""
	for offset := 0; offset < maxCollectionGames; offset += collectionPage {
		var games []igdbGame
		q := fmt.Sprintf("fields name,first_release_date,game_type,parent_game,version_parent,cover.image_id,collections.id,collections.name,"+
			"release_dates.date,release_dates.date_format,platforms.abbreviation,platforms.name; "+
			"where collections = (%d) & game_type = (0,4,8,9); sort id asc; limit %d; offset %d;", collectionID, collectionPage, offset)
		if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
			return "", nil, err
		}
		for _, g := range games {
			if !IsRealGameType(g.GameType) || strings.TrimSpace(g.Name) == "" {
				continue
			}
			cg := CollectionGame{IGDBID: g.ID, Name: strings.TrimSpace(g.Name), GameType: g.GameType, ParentID: g.parent(), CoverID: g.Cover.ImageID}
			if g.FirstReleaseDate > 0 {
				cg.Year = time.Unix(g.FirstReleaseDate, 0).UTC().Year()
			}
			cg.Release = g.release()
			cg.Platforms = g.candidate().Platforms
			out = append(out, cg)
			for _, col := range g.Collections {
				if col.ID == collectionID && name == "" {
					name = col.Name
				}
			}
		}
		if len(games) < collectionPage {
			break
		}
	}
	return name, out, nil
}

// CollectionCoverURL is the URL of a collection game's cover (IGDB's "big" size, 264x374), from its image id.
func CollectionCoverURL(imageID string) string { return igdbImg + "t_cover_big/" + imageID + ".jpg" }

// GameCandidate reads one IGDB game as a quick-add candidate (genres, themes, platforms, developer, collection,
// cover), for the "Add" button of a game the library does not have. A game IGDB does not know is ErrNoGame.
func (c *Client) GameCandidate(ctx context.Context, igdbID int64) (Candidate, error) {
	var games []igdbGame
	q := fmt.Sprintf("%s,parent_game,version_parent; where id = %d;", strings.TrimSuffix(strings.TrimSpace(searchFields), ";"), igdbID)
	if err := c.igdbPost(ctx, "/games", q, &games); err != nil {
		return Candidate{}, err
	}
	if len(games) == 0 {
		return Candidate{}, ErrNoGame
	}
	cand := games[0].candidate()
	cand.ParentID = games[0].parent()
	return cand, nil
}

// ErrNoGame means IGDB does not hold the game asked for.
var ErrNoGame = fmt.Errorf("IGDB has no such game")
