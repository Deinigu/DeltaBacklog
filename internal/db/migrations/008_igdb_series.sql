-- IGDB's collection name for a linked game, kept by the enrichment (never shown). A game's own series stays empty
-- unless the name matches a series the library has or at least two library games share it; see internal/igdblink.
ALTER TABLE game ADD COLUMN igdb_series TEXT;
