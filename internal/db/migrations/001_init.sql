-- Lookup tables: every multi-valued or repeated text value lives in exactly one row.
CREATE TABLE status (
    id    INTEGER PRIMARY KEY,
    code  TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL UNIQUE,
    sort  INTEGER NOT NULL,
    color TEXT NOT NULL
) STRICT;

CREATE TABLE platform  (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE genre     (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE theme     (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE ownership (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE series    (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
CREATE TABLE company   (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;
-- People (or "Solo") a game is played with.
CREATE TABLE player    (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE) STRICT;

CREATE TABLE game (
    id             INTEGER PRIMARY KEY,
    title          TEXT NOT NULL CHECK (length(trim(title)) > 0),
    title_sort     TEXT NOT NULL,
    status_id      INTEGER NOT NULL REFERENCES status(id),
    ownership_id   INTEGER REFERENCES ownership(id),
    series_id      INTEGER REFERENCES series(id) ON DELETE SET NULL,
    series_order   INTEGER,
    developer_id   INTEGER REFERENCES company(id) ON DELETE SET NULL,
    release_year   INTEGER CHECK (release_year BETWEEN 1950 AND 2100),
    vr             INTEGER NOT NULL DEFAULT 0 CHECK (vr IN (0, 1)),
    favorite       INTEGER NOT NULL DEFAULT 0 CHECK (favorite IN (0, 1)),
    -- 100% completion flag; "finished" is expressed by the status.
    completed      INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0, 1)),
    rating         INTEGER CHECK (rating BETWEEN 1 AND 10),
    notes          TEXT NOT NULL DEFAULT '',
    cover_url      TEXT,
    cover_file     TEXT,
    igdb_id        INTEGER UNIQUE,
    steamgriddb_id INTEGER,
    queue_pos      INTEGER,
    source_id      INTEGER UNIQUE, -- id in the old NocoDB table, makes the import idempotent
    added_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX idx_game_status_queue ON game(status_id, queue_pos);
CREATE INDEX idx_game_series       ON game(series_id);
CREATE INDEX idx_game_release_year ON game(release_year);
CREATE INDEX idx_game_favorite     ON game(id) WHERE favorite = 1;
CREATE INDEX idx_game_title_sort   ON game(title_sort);

CREATE TABLE game_platform (
    game_id     INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    platform_id INTEGER NOT NULL REFERENCES platform(id),
    PRIMARY KEY (game_id, platform_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_platform_rev ON game_platform(platform_id, game_id);

CREATE TABLE game_genre (
    game_id  INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genre(id),
    PRIMARY KEY (game_id, genre_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_genre_rev ON game_genre(genre_id, game_id);

CREATE TABLE game_theme (
    game_id  INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    theme_id INTEGER NOT NULL REFERENCES theme(id),
    PRIMARY KEY (game_id, theme_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_theme_rev ON game_theme(theme_id, game_id);

CREATE TABLE game_player (
    game_id   INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    player_id INTEGER NOT NULL REFERENCES player(id),
    PRIMARY KEY (game_id, player_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_game_player_rev ON game_player(player_id, game_id);

-- One row per year (or named period such as childhood) a game was played.
CREATE TABLE play (
    id      INTEGER PRIMARY KEY,
    game_id INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    year    INTEGER CHECK (year BETWEEN 1970 AND 2100),
    period  TEXT CHECK (period IN ('childhood', 'always')),
    CHECK ((year IS NULL) <> (period IS NULL))
) STRICT;
CREATE UNIQUE INDEX uq_play_year   ON play(game_id, year)   WHERE year IS NOT NULL;
CREATE UNIQUE INDEX uq_play_period ON play(game_id, period) WHERE period IS NOT NULL;
CREATE INDEX idx_play_year ON play(year, game_id);

CREATE TABLE tier (
    id    INTEGER PRIMARY KEY,
    name  TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL,
    sort  INTEGER NOT NULL
) STRICT;

CREATE TABLE tierlist_entry (
    year    INTEGER NOT NULL CHECK (year BETWEEN 1970 AND 2100),
    game_id INTEGER NOT NULL REFERENCES game(id) ON DELETE CASCADE,
    tier_id INTEGER NOT NULL REFERENCES tier(id) ON DELETE CASCADE,
    pos     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (year, game_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX idx_tierlist_tier ON tierlist_entry(year, tier_id, pos);

-- Search-as-you-type. Maintained by the application (db.IndexGame) because series lives in another table.
CREATE VIRTUAL TABLE game_fts USING fts5(title, series, notes, tokenize = "unicode61 remove_diacritics 2");

CREATE VIEW v_game_flat AS
SELECT g.id, g.title, s.code AS status, s.label AS status_label, o.name AS ownership,
       se.name AS series, g.series_order, d.name AS developer, g.release_year,
       g.vr, g.favorite, g.completed, g.rating, g.notes, g.cover_file, g.queue_pos,
       (SELECT group_concat(p.name, ', ') FROM game_platform x JOIN platform p ON p.id = x.platform_id WHERE x.game_id = g.id) AS platforms,
       (SELECT group_concat(p.name, ', ') FROM game_genre x    JOIN genre p    ON p.id = x.genre_id    WHERE x.game_id = g.id) AS genres,
       (SELECT group_concat(p.name, ', ') FROM game_theme x    JOIN theme p    ON p.id = x.theme_id    WHERE x.game_id = g.id) AS themes,
       (SELECT group_concat(p.name, ', ') FROM game_player x   JOIN player p   ON p.id = x.player_id   WHERE x.game_id = g.id) AS players,
       (SELECT group_concat(coalesce(CAST(year AS TEXT), period), ', ') FROM (SELECT year, period FROM play WHERE game_id = g.id ORDER BY year, period)) AS years_played
FROM game g
JOIN status s ON s.id = g.status_id
LEFT JOIN ownership o ON o.id = g.ownership_id
LEFT JOIN series se   ON se.id = g.series_id
LEFT JOIN company d   ON d.id = g.developer_id;

INSERT INTO status (code, label, sort, color) VALUES
    ('playing',           'Playing',             1, '#22c55e'),
    ('playing_evergreen', 'Playing - Evergreen', 2, '#14b8a6'),
    ('up_next',           'Up Next',             3, '#3b82f6'),
    ('backlog',           'Backlog',             4, '#a855f7'),
    ('hold',              'Hold',                5, '#f59e0b'),
    ('evergreen',         'Evergreen',           6, '#06b6d4'),
    ('finished',          'Finished',            7, '#84cc16'),
    ('dropped',           'Dropped',             8, '#ef4444'),
    ('not_interested',    'Not Interested',      9, '#6b7280');

INSERT INTO tier (name, color, sort) VALUES
    ('S', '#ff7f7f', 1), ('A', '#ffbf7f', 2), ('B', '#ffdf7f', 3),
    ('C', '#bfff7f', 4), ('D', '#7fbfff', 5), ('F', '#bf7fff', 6);
