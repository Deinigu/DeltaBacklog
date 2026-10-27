-- The games of the IGDB collections behind the user's series, cached so the Series page can list the games the library
-- does not have ("not registered"). Refreshed on demand and by `sync-igdb --enrich`, never on a page load.
--   game.igdb_collection_id  the IGDB collection of a linked game (the first one, like igdb_series); IGDB's own data
--   igdb_collection          one row per cached collection; fetched_at is when its games were read
--   igdb_collection_game     the real games of a collection (IGDB game_type main game, standalone expansion, remake,
--                            remaster); cover_image is IGDB's image id, cover_file the small poster kept on disk
--   series_hidden            games the owner never wants to see as "not registered" again (reversible)
ALTER TABLE game ADD COLUMN igdb_collection_id INTEGER;

CREATE TABLE igdb_collection (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    fetched_at TEXT NOT NULL
) STRICT;

CREATE TABLE igdb_collection_game (
    collection_id INTEGER NOT NULL REFERENCES igdb_collection(id) ON DELETE CASCADE,
    igdb_id       INTEGER NOT NULL,
    title         TEXT NOT NULL,
    release_year  INTEGER,
    game_type     INTEGER NOT NULL,
    cover_image   TEXT,
    cover_file    TEXT,
    PRIMARY KEY (collection_id, igdb_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_icg_igdb ON igdb_collection_game(igdb_id);

CREATE TABLE series_hidden (
    collection_id INTEGER NOT NULL,
    igdb_id       INTEGER NOT NULL,
    hidden_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (collection_id, igdb_id)
) STRICT, WITHOUT ROWID;
