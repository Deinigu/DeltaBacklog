-- Ratings are not tracked. The view references the column, so it is rebuilt around the drop.
DROP VIEW v_game_flat;

ALTER TABLE game DROP COLUMN rating;

CREATE VIEW v_game_flat AS
SELECT g.id, g.title, s.code AS status, s.label AS status_label, o.name AS ownership,
       se.name AS series, g.series_order, d.name AS developer, g.release_year,
       g.vr, g.favorite, g.completed, g.notes, g.cover_file, g.queue_pos,
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
