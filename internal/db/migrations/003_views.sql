-- Saved views: a named filter + sort + layout, pinned to the navigation.
-- filter_json shape (shared with the frontend):
--   {"q":"", "in":{"genre":["RPG","Indie"]}, "ex":{"status":["finished"]}, "all":["genre"], "flags":["favorite"]}
CREATE TABLE saved_view (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL CHECK (length(trim(name)) > 0),
    icon        TEXT NOT NULL DEFAULT '',
    filter_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(filter_json)),
    sort_json   TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(sort_json)),
    group_by    TEXT NOT NULL DEFAULT '',
    layout      TEXT NOT NULL DEFAULT 'cards' CHECK (layout IN ('cards', 'table')),
    pinned      INTEGER NOT NULL DEFAULT 1 CHECK (pinned IN (0, 1)),
    pos         INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;
CREATE INDEX idx_saved_view_pos ON saved_view(pos);

-- Starter views; ordinary rows that can be edited or deleted.
INSERT INTO saved_view (name, icon, filter_json, sort_json, pinned, pos) VALUES
    ('Playing now', '🎮', '{"in":{"status":["playing","playing_evergreen"]}}', '[{"field":"status","dir":"asc"},{"field":"manual","dir":"asc"}]', 1, 1),
    ('Up next',     '⏭️', '{"in":{"status":["up_next"]}}',                      '[{"field":"manual","dir":"asc"}]', 1, 2),
    ('Backlog',     '📚', '{"in":{"status":["backlog"]}}',                      '[{"field":"title","dir":"asc"}]',  1, 3),
    ('On hold',     '⏸️', '{"in":{"status":["hold"]}}',                         '[{"field":"last_year","dir":"desc"}]', 0, 4),
    ('Endless',     '♾️', '{"in":{"status":["evergreen","playing_evergreen"]}}', '[{"field":"last_year","dir":"desc"}]', 0, 5);
