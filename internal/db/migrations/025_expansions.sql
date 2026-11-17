-- Expansions (IGDB game_type 2) live inside their base game; they are never Library cards.
--   expansion        one row per IGDB expansion of a library game. status_id / ownership_id stay NULL ("Not tracked") until the
--                    owner sets them. The ttb_* columns mirror game's time-to-beat columns (minutes, ttb_src as 012).
--                    hidden = the owner hid an IGDB entry that is wrong.
--   expansion_play   the years played, with the same constraints as play (a calendar year, or childhood / always).
--   expansion_fetch  when IGDB was last asked for the expansions of a game, so a game is not asked again within 30 days.
CREATE TABLE expansion (
    id                      INTEGER PRIMARY KEY,
    game_id                 INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    igdb_id                 INTEGER NOT NULL UNIQUE,
    title                   TEXT NOT NULL CHECK (length(trim(title)) > 0),
    release_date            TEXT,
    release_year            INTEGER CHECK (release_year IS NULL OR release_year BETWEEN 1950 AND 2100),
    cover_image_id          TEXT,
    cover_file              TEXT,
    steam_app_id            INTEGER CHECK (steam_app_id IS NULL OR steam_app_id > 0),
    status_id               INTEGER REFERENCES status(id),
    ownership_id            INTEGER REFERENCES ownership(id),
    notes                   TEXT NOT NULL DEFAULT '',
    time_to_beat_hastily    INTEGER CHECK (time_to_beat_hastily IS NULL OR time_to_beat_hastily > 0),
    time_to_beat_normally   INTEGER CHECK (time_to_beat_normally IS NULL OR time_to_beat_normally > 0),
    time_to_beat_completely INTEGER CHECK (time_to_beat_completely IS NULL OR time_to_beat_completely > 0),
    time_to_beat_src        TEXT CHECK (time_to_beat_src IN ('igdb', 'hltb', 'manual')),
    hidden                  INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1)),
    added_at                TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;
CREATE INDEX idx_expansion_game ON expansion(game_id);
CREATE INDEX idx_expansion_steam ON expansion(steam_app_id) WHERE steam_app_id IS NOT NULL;

CREATE TABLE expansion_play (
    id           INTEGER PRIMARY KEY,
    expansion_id INTEGER NOT NULL REFERENCES expansion(id) ON DELETE CASCADE,
    year         INTEGER CHECK (year BETWEEN 1970 AND 2100),
    period       TEXT CHECK (period IN ('childhood', 'always')),
    CHECK ((year IS NULL) <> (period IS NULL))
) STRICT;
CREATE UNIQUE INDEX uq_expansion_play_year   ON expansion_play(expansion_id, year)   WHERE year IS NOT NULL;
CREATE UNIQUE INDEX uq_expansion_play_period ON expansion_play(expansion_id, period) WHERE period IS NOT NULL;
CREATE INDEX idx_expansion_play_year ON expansion_play(year, expansion_id);

CREATE TABLE expansion_fetch (
    game_id    INTEGER PRIMARY KEY REFERENCES game(id) ON DELETE CASCADE,
    fetched_at TEXT NOT NULL
) STRICT;
