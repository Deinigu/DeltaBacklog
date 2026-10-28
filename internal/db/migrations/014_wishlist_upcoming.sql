-- Wishlist and Upcoming releases.
--   status 'wishlist'      a game the owner wants but does not have; sits after Backlog in the status order
--   first_release_date     IGDB's first release date, unix seconds (UTC); NULL when IGDB has none or says TBD
--   release_date_format    how exact it is, IGDB's date_format: 0 day, 1 month, 2 year, 3-6 Q1-Q4. A year release
--                          carries a day inside that year, so the date is only shown to the stated precision
--   platforms              the IGDB platforms of a cached collection game, "PC, PS5"
-- release_year stays as it is. The dates of the library's games are filled by `sync-igdb --enrich`, on add and relink,
-- and by Refresh from IGDB; the dates of the collection games by the collection refresh.
UPDATE status SET sort = sort + 1 WHERE sort >= 5;
INSERT INTO status (code, label, sort, color)
SELECT 'wishlist', 'Wishlist', 5, '#ec4899' WHERE NOT EXISTS (SELECT 1 FROM status WHERE code = 'wishlist' OR label = 'Wishlist');

ALTER TABLE game ADD COLUMN first_release_date INTEGER;
ALTER TABLE game ADD COLUMN release_date_format INTEGER CHECK (release_date_format BETWEEN 0 AND 6);

ALTER TABLE igdb_collection_game ADD COLUMN first_release_date INTEGER;
ALTER TABLE igdb_collection_game ADD COLUMN release_date_format INTEGER CHECK (release_date_format BETWEEN 0 AND 6);
ALTER TABLE igdb_collection_game ADD COLUMN platforms TEXT;

CREATE INDEX idx_game_release_date ON game(first_release_date) WHERE first_release_date IS NOT NULL;
CREATE INDEX idx_icg_release_date ON igdb_collection_game(first_release_date);
