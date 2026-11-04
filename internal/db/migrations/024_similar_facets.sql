-- Similar games: the platforms and genres of a cached suggestion, so the Similar page can filter by them. Both are
-- comma separated lists ("PC, PS5"; "Adventure, Role-playing (RPG)"), empty until the next Refresh from IGDB.
ALTER TABLE similar_game ADD COLUMN platforms TEXT;
ALTER TABLE similar_game ADD COLUMN genres TEXT;
