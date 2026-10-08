-- The image cache key of a cover. updated_at changes on every edit; using it for the cache key made every
-- status or favorite change re-download the cover and duplicate it in the service worker's cache.
ALTER TABLE game ADD COLUMN cover_updated_at TEXT NOT NULL DEFAULT '';
UPDATE game SET cover_updated_at = updated_at WHERE cover_file IS NOT NULL;
