-- Custom rankings (TierMaker style) replace the per-year tier list: any number of rankings, each with its own
-- tiers (long labels, any colour) and any set of games. The old tier / tierlist_entry tables stay in the file,
-- unused and kept as they were, so nothing is lost and a downgrade still finds its data.
CREATE TABLE ranking (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT,
    -- The calendar year this ranking is "the ranking of" (Year in review opens it); NULL for any other ranking.
    year        INTEGER CHECK (year BETWEEN 1970 AND 2100),
    pos         INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE ranking_tier (
    id         INTEGER PRIMARY KEY,
    ranking_id INTEGER NOT NULL REFERENCES ranking(id) ON DELETE CASCADE,
    label      TEXT NOT NULL CHECK (length(trim(label)) > 0),
    color      TEXT NOT NULL,
    pos        INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_ranking_tier ON ranking_tier(ranking_id, pos);

-- tier_id NULL is the unranked pool of the ranking. Deleting a tier sends its games back to the pool.
CREATE TABLE ranking_entry (
    ranking_id INTEGER NOT NULL REFERENCES ranking(id) ON DELETE CASCADE,
    game_id    INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    tier_id    INTEGER REFERENCES ranking_tier(id) ON DELETE SET NULL,
    pos        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (ranking_id, game_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_ranking_entry_tier ON ranking_entry(ranking_id, tier_id, pos);
CREATE INDEX idx_ranking_entry_game ON ranking_entry(game_id);

-- Portrait box art for the rankings: a ~300 px wide 2:3 JPEG in the covers folder (p<id>.jpg).
-- poster_src says where it came from: 'igdb', 'sgdb' or 'crop' (a centre crop of the wide cover; a better source replaces it).
ALTER TABLE game ADD COLUMN poster_file TEXT;
ALTER TABLE game ADD COLUMN poster_src TEXT CHECK (poster_src IN ('igdb', 'sgdb', 'crop'));
ALTER TABLE game ADD COLUMN poster_updated_at TEXT NOT NULL DEFAULT '';

-- Every year with a tier list becomes the ranking "Tier list <year>": the global tiers copied into it, its entries
-- (also those of games that lost the year since: nothing is dropped), and the rest of the year's games in the pool.
INSERT INTO ranking (name, year, pos)
SELECT 'Tier list ' || year, year, row_number() OVER (ORDER BY year DESC) FROM (SELECT DISTINCT year FROM tierlist_entry);

INSERT INTO ranking_tier (ranking_id, label, color, pos)
SELECT r.id, t.name, t.color, t.sort FROM ranking r JOIN tier t ORDER BY r.id, t.sort, t.id;

INSERT INTO ranking_entry (ranking_id, game_id, tier_id, pos)
SELECT r.id, e.game_id, rt.id, e.pos
FROM tierlist_entry e
JOIN ranking r ON r.year = e.year
JOIN tier t ON t.id = e.tier_id
JOIN ranking_tier rt ON rt.ranking_id = r.id AND rt.label = t.name;

INSERT INTO ranking_entry (ranking_id, game_id, tier_id, pos)
SELECT r.id, p.game_id, NULL, row_number() OVER (PARTITION BY r.id ORDER BY p.game_id) - 1
FROM ranking r JOIN play p ON r.year = p.year
WHERE NOT EXISTS (SELECT 1 FROM ranking_entry x WHERE x.ranking_id = r.id AND x.game_id = p.game_id);
