-- Deals: current best price and historical low from IsThereAnyDeal (ITAD) for the Wishlist, Backlog and Up next games.
-- Refreshed on demand (Deals page), never on a page load.
--   setting      small key/value store; 'deals_country' is the ISO country the prices are read for (default ES)
--   deal_match   the ITAD game id of a library game. itad_id NULL = no match: for source 'user' the owner said so (never
--                searched again), for 'steam'/'title' the search found nothing (retried after a week)
--   deal_price   the cached prices of a matched game, in cents of `currency`; cur_* is the best price right now (NULL when
--                no shop sells it), low_* the historical low. Dropped when the country changes
CREATE TABLE setting (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT, WITHOUT ROWID;

CREATE TABLE deal_match (
    game_id    INTEGER PRIMARY KEY REFERENCES game(id) ON DELETE CASCADE,
    itad_id    TEXT,
    itad_title TEXT NOT NULL DEFAULT '',
    itad_slug  TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL CHECK (source IN ('steam', 'title', 'user')),
    matched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE deal_price (
    game_id       INTEGER PRIMARY KEY REFERENCES game(id) ON DELETE CASCADE,
    currency      TEXT NOT NULL,
    cur_shop      TEXT,
    cur_price     INTEGER CHECK (cur_price >= 0),
    cur_regular   INTEGER CHECK (cur_regular >= 0),
    cur_cut       INTEGER CHECK (cur_cut BETWEEN 0 AND 100),
    cur_url       TEXT CHECK (cur_url IS NULL OR cur_url LIKE 'http://%' OR cur_url LIKE 'https://%'),
    cur_expiry    TEXT,
    low_price     INTEGER CHECK (low_price >= 0),
    low_shop      TEXT,
    low_at        TEXT,
    fetched_at    TEXT NOT NULL
) STRICT;
