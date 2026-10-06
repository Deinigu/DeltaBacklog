# DeltaBacklog

A lightweight, mobile-first videogame tracker for a Raspberry Pi 3B+ (armv7): Go + SQLite, no external services at runtime except optional metadata lookups (IGDB / SteamGridDB).

## Status
Work in progress, built feature branch by feature branch (see `features/*`, merged into `main`).

- [x] Normalized SQLite schema, NocoDB importer, read API (`/api/games`, `/api/facets`, `/covers/*`)
- [x] Library view: instant search, faceted filters (URL-backed), mobile-first UI, PWA shell
- [ ] Shelves, table view, quick add, years and tierlists, packaging

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
