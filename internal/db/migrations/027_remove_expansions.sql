-- The Expansions feature is gone (it was added by 025 and 026, which stay as they were applied).
--   Dropped: expansion_play, expansion_fetch and expansion, with their indexes, in foreign-key order.
-- Nothing else changes: games, plays, views and settings are untouched.
DROP INDEX IF EXISTS idx_expansion_play_year;
DROP INDEX IF EXISTS uq_expansion_play_period;
DROP INDEX IF EXISTS uq_expansion_play_year;
DROP TABLE expansion_play;
DROP TABLE expansion_fetch;
DROP INDEX IF EXISTS idx_expansion_steam;
DROP INDEX IF EXISTS idx_expansion_game;
DROP TABLE expansion;
