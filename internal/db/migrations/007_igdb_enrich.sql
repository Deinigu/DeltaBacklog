-- IGDB enrichment (phase 2 of the IGDB sync): time to beat, game modes, player perspectives and store links
-- for games that have an igdb_id. No IGDB scores or ratings are stored.
--   time_to_beat_*   minutes (IGDB reports seconds); NULL when IGDB has no value
--   igdb_enriched_at when the enrichment last read this game from IGDB; makes the run resumable
ALTER TABLE game ADD COLUMN time_to_beat_hastily    INTEGER CHECK (time_to_beat_hastily > 0);
ALTER TABLE game ADD COLUMN time_to_beat_normally   INTEGER CHECK (time_to_beat_normally > 0);
ALTER TABLE game ADD COLUMN time_to_beat_completely INTEGER CHECK (time_to_beat_completely > 0);
ALTER TABLE game ADD COLUMN igdb_enriched_at        TEXT;

CREATE TABLE game_mode   (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE perspective (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;

CREATE TABLE game_game_mode (
    game_id      INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    game_mode_id INTEGER NOT NULL REFERENCES game_mode(id),
    PRIMARY KEY (game_id, game_mode_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_game_mode_rev ON game_game_mode(game_mode_id, game_id);

CREATE TABLE game_perspective (
    game_id        INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    perspective_id INTEGER NOT NULL REFERENCES perspective(id),
    PRIMARY KEY (game_id, perspective_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_perspective_rev ON game_perspective(perspective_id, game_id);

-- At most one link per kind and game.
CREATE TABLE game_link (
    game_id INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL CHECK (kind IN ('steam', 'gog', 'epic', 'official')),
    url     TEXT NOT NULL CHECK (url LIKE 'http://%' OR url LIKE 'https://%'),
    PRIMARY KEY (game_id, kind)
) STRICT, WITHOUT ROWID;
