-- IGDB's DLC (game_type 1) next to its expansions (game_type 2). IGDB types some big add-ons as DLC (Skyrim's Dawnguard and
-- Hearthfire), so both are read; only the ones the owner adds show on the pages (see isAdded in web/static/js/expansions.js).
--   kind         'expansion' (IGDB game_type 2) or 'dlc' (1). Every row read so far is an expansion.
--   steam_owned  the Steam import found the DLC among the owner's apps. A DLC is not given an ownership by the import (Steam
--                lists cosmetic packs as DLC too): it is shown first, marked "Owned on Steam", in its game's details.
-- expansion_fetch is emptied so every game is read again (once) and gets its DLC.
ALTER TABLE expansion ADD COLUMN kind TEXT NOT NULL DEFAULT 'expansion' CHECK (kind IN ('expansion', 'dlc'));
ALTER TABLE expansion ADD COLUMN steam_owned INTEGER NOT NULL DEFAULT 0 CHECK (steam_owned IN (0, 1));
DELETE FROM expansion_fetch;
