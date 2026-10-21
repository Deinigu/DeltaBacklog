-- Where a game's time to beat came from, so a time the owner typed or took from HowLongToBeat is never
-- overwritten (or cleared) by the IGDB enrichment. One source for the three times together.
--   igdb    written by the enrichment (the only kind that existed before this migration)
--   hltb    picked from HowLongToBeat in the game drawer
--   manual  typed by the owner
-- NULL when the game has no time at all.
ALTER TABLE game ADD COLUMN time_to_beat_src TEXT CHECK (time_to_beat_src IN ('igdb', 'hltb', 'manual'));
UPDATE game SET time_to_beat_src = 'igdb'
 WHERE time_to_beat_hastily IS NOT NULL OR time_to_beat_normally IS NOT NULL OR time_to_beat_completely IS NOT NULL;
