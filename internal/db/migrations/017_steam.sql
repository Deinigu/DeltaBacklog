-- Steam sync: the owned games and hours played read from the Steam Web API, on demand (never on a page load).
--   game.playtime_min        minutes played on Steam, summed over the Steam apps matched to the game; NULL = never read.
--                            A sync only ever raises it, so a partial or failed read cannot shrink it
--   game.steam_last_played   unix seconds of the last time any of those apps was played; NULL when unknown or never
--   steam_app                one row per Steam app the account owns, kept between syncs
--     game_id  the library game it is matched to (NULL = not in the library: the review list offers to add it)
--     source   how the match was made: 'link' (the game's Steam store link), 'title' (an exact title), 'user' (the
--              owner chose), or 'none' (the owner said no library game is this app; never matched automatically)
--     ignored  the owner hid the app from the review list (reversible)
CREATE TABLE steam_app (
    app_id       INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    playtime_min INTEGER NOT NULL DEFAULT 0 CHECK (playtime_min >= 0),
    last_played  INTEGER CHECK (last_played IS NULL OR last_played > 0),
    game_id      INTEGER REFERENCES game(id) ON DELETE SET NULL,
    source       TEXT CHECK (source IN ('link', 'title', 'user', 'none')),
    ignored      INTEGER NOT NULL DEFAULT 0 CHECK (ignored IN (0, 1)),
    seen_at      TEXT NOT NULL
) STRICT;
CREATE INDEX idx_steam_app_game ON steam_app(game_id) WHERE game_id IS NOT NULL;

ALTER TABLE game ADD COLUMN playtime_min INTEGER CHECK (playtime_min IS NULL OR playtime_min >= 0);
ALTER TABLE game ADD COLUMN steam_last_played INTEGER CHECK (steam_last_played IS NULL OR steam_last_played > 0);
