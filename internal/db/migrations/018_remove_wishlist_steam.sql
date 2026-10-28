-- Wishlist and Steam sync are gone.
--   Steam: the steam_app table and the hours 017 added to game (playtime_min, steam_last_played) are dropped.
--   Wishlist: a game with that status becomes Backlog, the status row goes, and the statuses after it get the sort numbers
--   they had before 014 shifted them. Saved views lose the code from their status lists and any sort level by hours played; the Steam sync time setting goes.
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

DELETE FROM setting WHERE key = 'steam_synced_at';

-- Saved views: the wishlist code leaves the status lists (only those two arrays are touched), and a sort level by the
-- hours played (field 'playtime') is dropped.
UPDATE saved_view SET filter_json = json_set(filter_json, '$.in.status',
    json((SELECT json_group_array(value) FROM json_each(filter_json, '$.in.status') WHERE value <> 'wishlist')))
WHERE json_valid(filter_json) AND EXISTS (SELECT 1 FROM json_each(filter_json, '$.in.status') WHERE value = 'wishlist');
UPDATE saved_view SET filter_json = json_set(filter_json, '$.ex.status',
    json((SELECT json_group_array(value) FROM json_each(filter_json, '$.ex.status') WHERE value <> 'wishlist')))
WHERE json_valid(filter_json) AND EXISTS (SELECT 1 FROM json_each(filter_json, '$.ex.status') WHERE value = 'wishlist');

UPDATE saved_view SET sort_json = (SELECT json_group_array(json(value)) FROM json_each(sort_json) WHERE json_extract(value, '$.field') IS NOT 'playtime')
WHERE json_valid(sort_json) AND EXISTS (SELECT 1 FROM json_each(sort_json) WHERE json_extract(value, '$.field') = 'playtime');
