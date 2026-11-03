-- Deals: price alerts. When a game's best price is at or below its historical low, or its lowest price of the last two
-- years, the daily `deals-check` (and the Alerts chip on Deals) says so.
--   deal_alert   one row per game that was ever alerted or muted. muted = the owner turned alerts off for the game;
--                last_alert_* is what was announced last (price in cents, kind 'historical' or 'two_year', time), so the same
--                deal is not announced twice; seen = 0 until the owner opens Deals (the badge on the More menu);
--                pending = 1 until the alert has reached Telegram (an alert that could not be sent is tried again)
--   deal_price   low2y_price is the lowest price of the last two years (cents, NULL = none found), read from the ITAD
--                price history only for games that need it; low2y_checked_at is when
CREATE TABLE deal_alert (
    game_id          INTEGER PRIMARY KEY REFERENCES game(id) ON DELETE CASCADE,
    muted            INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0, 1)),
    last_alert_price INTEGER CHECK (last_alert_price >= 0),
    last_alert_kind  TEXT CHECK (last_alert_kind IN ('historical', 'two_year')),
    last_alert_at    TEXT,
    seen             INTEGER NOT NULL DEFAULT 1 CHECK (seen IN (0, 1)),
    pending          INTEGER NOT NULL DEFAULT 0 CHECK (pending IN (0, 1))
) STRICT;

ALTER TABLE deal_price ADD COLUMN low2y_price INTEGER CHECK (low2y_price >= 0);
ALTER TABLE deal_price ADD COLUMN low2y_checked_at TEXT;
