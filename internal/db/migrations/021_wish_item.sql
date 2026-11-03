-- Deals: the owner's Steam wishlist, kept apart from the library on purpose (it is NOT the old Wishlist status that
-- migration 018 removed): these rows are never games, so they never show up in the Library, views, stats or series.
--   wish_item   one row per game on the Steam wishlist (read during Deals' Refresh and the daily deals-check; a game that
--               left the wishlist is deleted with its prices). appid is the Steam app id. itad_id NULL = ITAD does not know
--               the app (matched_at NULL = never looked up; an automatic "no match" is retried after a week). title is the
--               ITAD title, or "Steam app <id>" until there is one. hidden_at = the owner hid it from Deals; muted and the
--               last_alert_* / seen / pending columns are deal_alert's, for the same alert rule
--   wish_price  the cached prices of a matched wishlist game: deal_price's columns, keyed by app id
-- Parallel tables rather than a nullable appid on deal_*: those are keyed by game_id with a foreign key to game, and
-- SQLite cannot change a primary key without rebuilding the tables.
CREATE TABLE wish_item (
    appid            INTEGER PRIMARY KEY,
    itad_id          TEXT,
    itad_slug        TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL,
    added_at         TEXT,
    seen_at          TEXT NOT NULL,
    matched_at       TEXT,
    hidden_at        TEXT,
    muted            INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0, 1)),
    last_alert_price INTEGER CHECK (last_alert_price >= 0),
    last_alert_kind  TEXT CHECK (last_alert_kind IN ('historical', 'two_year')),
    last_alert_at    TEXT,
    seen             INTEGER NOT NULL DEFAULT 1 CHECK (seen IN (0, 1)),
    pending          INTEGER NOT NULL DEFAULT 0 CHECK (pending IN (0, 1))
) STRICT;

CREATE TABLE wish_price (
    appid            INTEGER PRIMARY KEY REFERENCES wish_item(appid) ON DELETE CASCADE,
    currency         TEXT NOT NULL,
    cur_shop         TEXT,
    cur_price        INTEGER CHECK (cur_price >= 0),
    cur_regular      INTEGER CHECK (cur_regular >= 0),
    cur_cut          INTEGER CHECK (cur_cut BETWEEN 0 AND 100),
    cur_url          TEXT CHECK (cur_url IS NULL OR cur_url LIKE 'http://%' OR cur_url LIKE 'https://%'),
    cur_expiry       TEXT,
    low_price        INTEGER CHECK (low_price >= 0),
    low_shop         TEXT,
    low_at           TEXT,
    low2y_price      INTEGER CHECK (low2y_price >= 0),
    low2y_checked_at TEXT,
    fetched_at       TEXT NOT NULL
) STRICT;
