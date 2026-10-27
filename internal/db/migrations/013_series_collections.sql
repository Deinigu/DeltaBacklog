-- The games of the IGDB collections behind the user's series, cached so the Series page can list the games the library
-- does not have ("not registered"). Refreshed on demand and by `sync-igdb --enrich`, never on a page load.
--   game_collection         every IGDB collection of a linked game (IGDB's own data); a series is mapped to the
--                           collection(s) most of its linked games share (see internal/collections)
--   game.igdb_parent_id     the root of the game's IGDB version chain (original <- expanded edition <- remaster or
--                           port; only links that mean "another version of the same game", never a standalone
--                           expansion's or a remake's parent); 0 = the game is its own root, NULL = not read yet.
--                           IGDB splits one game into several entries, so a game counts as owned with its whole family
--   igdb_collection         one row per cached collection; fetched_at is when its games were read
--   igdb_collection_game    the real games of a collection (IGDB game_type main game, standalone expansion, remake,
--                           remaster); cover_image is IGDB's image id, cover_file the small poster kept on disk
--   series_hidden           games the owner never wants to see as "not registered" again (reversible)
ALTER TABLE game ADD COLUMN igdb_parent_id INTEGER;

CREATE TABLE game_collection (
    game_id       INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    collection_id INTEGER NOT NULL,
    name          TEXT NOT NULL,
    PRIMARY KEY (game_id, collection_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_collection ON game_collection(collection_id);

CREATE TABLE igdb_collection (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    fetched_at TEXT NOT NULL
) STRICT;

CREATE TABLE igdb_collection_game (
    collection_id   INTEGER NOT NULL REFERENCES igdb_collection(id) ON DELETE CASCADE,
    igdb_id         INTEGER NOT NULL,
    title           TEXT NOT NULL,
    release_year    INTEGER,
    game_type       INTEGER NOT NULL,
    parent_igdb_id  INTEGER NOT NULL DEFAULT 0, -- the root of its version chain, 0 = itself
    cover_image     TEXT,
    cover_file      TEXT,
    PRIMARY KEY (collection_id, igdb_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_icg_igdb ON igdb_collection_game(igdb_id);

CREATE TABLE series_hidden (
    collection_id INTEGER NOT NULL,
    igdb_id       INTEGER NOT NULL,
    hidden_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (collection_id, igdb_id)
) STRICT, WITHOUT ROWID;
