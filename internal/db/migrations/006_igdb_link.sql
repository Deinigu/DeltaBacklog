-- Linking the library to IGDB (phase 1 of the IGDB sync). Only the link is stored: no IGDB scores or ratings.
--   igdb_synced_at   when igdb_id was set by the sync or accepted in the review screen
--   igdb_checked_at  when the sync last searched IGDB for this game; makes the sync resumable
--   igdb_not_found   the owner says the game is not on IGDB: never search for it again
ALTER TABLE game ADD COLUMN igdb_synced_at  TEXT;
ALTER TABLE game ADD COLUMN igdb_checked_at TEXT;
ALTER TABLE game ADD COLUMN igdb_not_found  INTEGER NOT NULL DEFAULT 0 CHECK (igdb_not_found IN (0, 1));

-- Uncertain IGDB matches waiting for the owner's decision in the "Link with IGDB" screen.
CREATE TABLE sync_candidate (
    id         INTEGER PRIMARY KEY,
    game_id    INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    igdb_id    INTEGER NOT NULL,
    name       TEXT NOT NULL,
    year       INTEGER,
    score      INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (game_id, igdb_id)
) STRICT;
CREATE INDEX idx_sync_candidate_pending ON sync_candidate(game_id) WHERE status = 'pending';
