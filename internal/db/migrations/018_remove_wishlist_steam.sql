-- Wishlist and Steam sync are gone.
--   Steam: the steam_app table and the hours 017 added to game (playtime_min, steam_last_played) are dropped.
--   Wishlist: a game with that status becomes Backlog, the status row goes, and the statuses after it get the sort numbers
--   they had before 014 shifted them. Saved views lose the code from their filter (an emptied list filters nothing).
-- The release dates 014 added stay: Upcoming still uses them.
DROP INDEX IF EXISTS idx_steam_app_game;
DROP TABLE steam_app;
ALTER TABLE game DROP COLUMN playtime_min;
ALTER TABLE game DROP COLUMN steam_last_played;

UPDATE game SET status_id = (SELECT id FROM status WHERE code = 'backlog')
WHERE status_id = (SELECT id FROM status WHERE code = 'wishlist');

UPDATE status SET sort = sort - 1
WHERE sort > (SELECT sort FROM status WHERE code = 'wishlist') AND EXISTS (SELECT 1 FROM status WHERE code = 'wishlist');
DELETE FROM status WHERE code = 'wishlist';

UPDATE saved_view SET filter_json = replace(replace(replace(filter_json, '"wishlist",', ''), ',"wishlist"', ''), '"wishlist"', '')
WHERE filter_json LIKE '%wishlist%';
