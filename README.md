# DeltaBacklog

A lightweight, mobile-first videogame tracker for a Raspberry Pi 3B+ (armv7): Go + SQLite, no external services at runtime except optional metadata lookups (IGDB / SteamGridDB).

## Status
Work in progress, built feature branch by feature branch (see `features/*`, merged into `main`).

- [x] Normalized SQLite schema, NocoDB importer, read API (`/api/games`, `/api/facets`, `/covers/*`)
- [x] Library view: instant search, faceted filters (URL-backed), mobile-first UI, PWA shell
- [x] Multi-level sort (any fields, asc/desc, saved in the URL) and group-by
- [x] Saved views: save, edit, pin, reorder, duplicate, delete (with undo); include/exclude chips and Any/All matching
- [x] Change a game's status or favorite from its detail view; drag-to-reorder manual order (touch friendly)
- [x] Table layout: sortable, resizable, hideable columns; inline editing of every field; CSV export; saved views remember cards vs table
- [x] Quick add: search IGDB by title or Steam/GOG/Epic/IGDB link, pre-filled form, SteamGridDB cover picker; change cover and delete from the detail view
- [x] Edit every field of a game from its detail view (phone friendly), refill its metadata from IGDB, link it to IGDB
- [ ] Surprise me, years and tierlists, series, stats, packaging

## Develop
```sh
go test ./...
go run ./cmd/deltabacklog          # serves on :8081, data in ./data
```

## Import from NocoDB
Export the old table on the Pi, then import it (idempotent: already-imported rows are skipped, failed covers are retried):
```sh
ssh raspberry 'docker exec root_db psql -U postgres -d root_db -c "\copy (select id,\"Title\",\"Platform\",\"Status\",\"Style\",\"VR\",\"Genre\",\"Boxart\",\"Completed\",\"Years_played\",\"Series\",\"Notas\",\"Ownership\",\"Played_with\",\"Favorito\",nc_order from pXXXXXXXXXXXXXX.\"Games_Library\" order by id) to stdout with csv header"' > games.csv
go run ./cmd/import -csv games.csv -data ./data -nocodb http://192.168.1.50:8080
```

## Configuration
Copy `.env.example` to `.env`. Never commit it.

## Quick add (IGDB / SteamGridDB)
Copy `.env.example` to `.env` and fill in `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET` (a Twitch developer app) and `STEAMGRIDDB_API_KEY`. Without them the app works, but games must be added manually. The `.env` file is gitignored and loaded from the working directory (real environment variables win).
