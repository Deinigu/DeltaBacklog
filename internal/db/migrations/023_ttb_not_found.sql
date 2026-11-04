-- "No time on HowLongToBeat": the owner says no time to beat exists for this game (HowLongToBeat does not know it), so
-- the Needs attention page stops listing it, the bulk lookup skips it and Stats counts it as "no time exists", not as missing.
-- It only matters while the game has no time at all.
ALTER TABLE game ADD COLUMN ttb_not_found INTEGER NOT NULL DEFAULT 0 CHECK (ttb_not_found IN (0, 1));
