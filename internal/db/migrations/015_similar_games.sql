-- Similar games: IGDB's similar_games of the library's finished and favourite games, cached so the Similar page can list
-- the games the library does not have. Refreshed on demand and by `sync-igdb --enrich`, never on a page load.
--   similar_game    one row per suggested IGDB game (real games only: main game, standalone expansion, remake, remaster);
--                   cover_image is IGDB's image id, cover_file the small poster kept on disk (shared i<id>.jpg files, see
--                   internal/collections); parent_igdb_id is the root of its version chain, 0 = itself
--   similar_link    which source game (by IGDB id) suggested which game
--   similar_fetch   when a source game's similar games were read (also for a game with none)
--   similar_hidden  suggestions the owner never wants to see again (reversible); keyed by IGDB id and separate from
--                   series_hidden, so hiding a suggestion does not hide the game on the Series page, nor the reverse
CREATE TABLE similar_game (
    igdb_id             INTEGER PRIMARY KEY,
    title               TEXT NOT NULL,
    release_year        INTEGER,
    game_type           INTEGER NOT NULL,
    parent_igdb_id      INTEGER NOT NULL DEFAULT 0,
    cover_image         TEXT,
    cover_file          TEXT,
    first_release_date  INTEGER,
    release_date_format INTEGER CHECK (release_date_format BETWEEN 0 AND 6)
) STRICT;

CREATE TABLE similar_link (
    source_igdb_id  INTEGER NOT NULL,
    similar_igdb_id INTEGER NOT NULL REFERENCES similar_game(igdb_id) ON DELETE CASCADE,
    PRIMARY KEY (source_igdb_id, similar_igdb_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_similar_link_similar ON similar_link(similar_igdb_id);

CREATE TABLE similar_fetch (
    source_igdb_id INTEGER PRIMARY KEY,
    fetched_at     TEXT NOT NULL
) STRICT;

CREATE TABLE similar_hidden (
    igdb_id   INTEGER PRIMARY KEY,
    hidden_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;
