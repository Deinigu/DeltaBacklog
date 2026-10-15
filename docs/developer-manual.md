# DeltaBacklog: developer manual

For anyone (or any future Claude session) changing the code. The user-facing description is in the [user manual](user-manual.md). `CLAUDE.md` at the repository root holds the short list of rules that are easy to get wrong; this manual is the long version.

**Contents**
1. [Overview and principles](#1-overview-and-principles)
2. [Repository layout](#2-repository-layout)
3. [Running it](#3-running-it)
4. [Data model](#4-data-model)
5. [HTTP API](#5-http-api)
6. [Backend packages](#6-backend-packages)
7. [Frontend](#7-frontend)
8. [External services](#8-external-services)
9. [Testing](#9-testing)
10. [Workflow](#10-workflow)
11. [Build and deploy](#11-build-and-deploy)
12. [Known gaps](#12-known-gaps)

---

## 1. Overview and principles

DeltaBacklog replaces a NocoDB table of videogames with a small purpose-built app. It runs on a **Raspberry Pi 3B+ (armv7, about 900MB RAM shared with other containers)**, and the phone is the main client.

```
 phone / PC browser
   │  HTTP (LAN only, no login)
   ▼
 ┌───────────────────────────── one Go binary ─────────────────────────────┐
 │  web/        embedded static PWA (go:embed): index.html, js/*.js, css    │
 │  internal/api   JSON API + /covers/*  ──►  internal/db  (SQLite, WAL)    │
 │       │                                          ▲                        │
 │       ├──► internal/meta   IGDB, SteamGridDB, Steam (outbound HTTPS)      │
 │       └──► internal/covers download, resize to JPEG, store in data/covers │
 └───────────────────────────────────────────────────────────────────────────┘
   data/app.db      the whole database          data/covers/<id>.jpg  cover images
```

Principles that shape every decision:
- **Light.** One static binary, pure-Go SQLite (`modernc.org/sqlite`, no CGO, so cross-compiling is trivial), vanilla JavaScript with **no build step and no framework**. The whole library (about 600 games) is sent to the browser in one JSON response and filtered there, so filtering is instant.
- **Mobile first.** Design for 390px, then scale up. Tap targets of at least 44px, no horizontal page scroll, no hover-only actions.
- **Own the data.** Covers are downloaded once and served from the Pi; the app keeps working when IGDB or SteamGridDB are unreachable.
- **Normalized data.** Every repeated value lives in one row of a lookup table. Nothing is stored as comma-separated text.
- **User choices.** No ratings and no "number in the series". Do not add them back.

## 2. Repository layout

```
cmd/deltabacklog/   the server and its subcommands (main.go: .env loading, wiring; `check`, `backup`, `backup-screenshots`, `sync-igdb`, `fetch-posters`)
cmd/import/         one-off importer from a NocoDB CSV export (also downloads covers)
internal/db/        open, migrate (embedded migrations/*.sql), GetOrCreate, FTS index, TitleSort, NormalizeTheme
internal/importer/  CSV → normalized schema (idempotent)
internal/api/       HTTP handlers: games, games_write, add (create/delete/cover/lookup), facets, views, rankings
internal/meta/      IGDB, SteamGridDB and link resolution clients; TitleScore (fuzzy title match)
internal/igdblink/  the IGDB sync: matching buckets, resumable runner, review queue, `sync-igdb` command
internal/covers/    cover download (strict: no redirects) + resize + size guard; `poster.go`: the 2:3 portrait posters
internal/posters/   poster maker (IGDB cover, SteamGridDB 600x900, crop of the cover), its background queue and `fetch-posters`
internal/shots/     screenshot files: content sniffing, bounded decode + thumbnail, EXIF, incremental backup copy
web/web.go          go:embed of web/static
web/static/         index.html, styles.css, sw.js, manifest, icon, js/*.js
web/test/           Node unit tests for the pure frontend modules (outside the embedded folder)
deploy/             deploy.sh (build + upload), install.sh (runs on the Pi), the systemd units and backup.sh
docs/               these manuals; docs/img screenshots; docs/tools/screenshots.mjs regenerates them from the demo library in docs/tools/demo-data
```

## 3. Running it

Go 1.27 and Node 20+ must be on your PATH (Node is only for the frontend tests and the screenshot tool).

```sh
gofmt -l .                         # must print nothing (CI fails otherwise)
go vet ./... && go test ./...
node --test web/test/*.test.mjs
go run ./cmd/deltabacklog          # :8081, data in ./data, keys from ./.env
```

`web/static` is compiled into the binary: **restart after any frontend change**.

**Demo library** (to try the app or take screenshots without your own data; 40 well-known games with generated placeholder covers, add `-env <your .env>` to download real ones):

```sh
go run ./docs/tools/demo-data -data /tmp/demo
LISTEN_ADDR=127.0.0.1:18090 DATA_DIR=/tmp/demo go run ./cmd/deltabacklog
```

**Configuration** (`.env` in the working directory, or real environment variables, which win; `.env` is git-ignored, `.env.example` is committed):

| Variable | Meaning |
|---|---|
| `LISTEN_ADDR` | default `:8081` |
| `DATA_DIR` | default `./data` (holds `app.db` and `covers/`) |
| `SCREENSHOTS_DIR` | where screenshots are stored; default `$DATA_DIR/screenshots` (created at start-up). **When set, the program never creates this folder**, only the per-game subfolders inside it: see "Screenshots" in section 5 and the external drive in section 11 |
| `SCREENSHOT_MAX_MB`, `SCREENSHOT_REQUEST_MAX_MB` | largest accepted screenshot (default 25) and largest upload request (default 200) |
| `SCREENSHOTS_DECODE_BUDGET_MB` | most memory the server may need to decode one screenshot for its thumbnail (default 48); bigger pictures need the browser's own thumbnail |
| `ALLOWED_HOSTS` | extra `Host` names, comma separated (see "Security" in section 5); empty by default |
| `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET` | a Twitch developer application; enables lookups |
| `STEAMGRIDDB_API_KEY` | enables SteamGridDB covers |
| `GOMEMLIMIT` | Go soft memory limit (for example `96MiB`). The runtime reads it only from the real environment at start-up, so `loadDotEnv` applies a value found in `.env` itself with `debug.SetMemoryLimit` |

Without keys the app works; lookups answer `configured: false` and the UI offers manual entry.
The version printed at start-up and in the usage text is `main.version` (`dev` by default); the release workflow sets it from the tag with `-ldflags "-X main.version=<tag>"`, and `deploy/third-party-licenses.sh` writes the `THIRD_PARTY_LICENSES.txt` that goes into each release archive.

**Port 8081 may be a real, live server (it is the default port).** Test servers must use another port (for example `LISTEN_ADDR=127.0.0.1:18090`), be addressed as `127.0.0.1:<port>` (never `localhost`) and never receive writes aimed at 8081. See `CLAUDE.md`.

**Other commands:** `deltabacklog` with any other first argument than `backup`, `backup-screenshots`, `check`, `sync-igdb` or `fetch-posters` prints usage and exits 2 (a typo never starts the server); `deltabacklog backup <file>` writes a consistent copy of `$DATA_DIR/app.db` exactly as it is (it never migrates it, so a newer binary can back up an older database before upgrading it, and a missing database is an error, never a new empty one); `deltabacklog check <file>` verifies a database or backup read-only (exit code 1 when it is damaged or empty); `deltabacklog backup-screenshots <dir>` copies the files of `SCREENSHOTS_DIR` that `<dir>` does not have yet (same relative paths, through a temporary name; it never overwrites or deletes anything in `<dir>`, and a missing screenshot folder is not an error).

**Importing from NocoDB:** see the README ("Import from NocoDB"). `cmd/import` is idempotent (keyed by the old row id, stored in `game.source_id`) and re-running it retries failed cover downloads.

## 4. Data model

SQLite, `STRICT` tables, `PRAGMA foreign_keys=ON`, WAL, one connection (`SetMaxOpenConns(1)`).

**Migrations** are numbered files in `internal/db/migrations/`, applied in order inside a transaction and tracked by `PRAGMA user_version`. **Add a new file; never edit an applied one.**

| # | What it does |
|---|---|
| 001 | the schema, seed statuses and tiers |
| 002 | drops `game.rating` (no ratings) and rebuilds `v_game_flat` |
| 003 | `saved_view` and five starter views |
| 004 | `game.cover_updated_at` (the cover's cache key) |
| 005 | drops `game.series_order` and rebuilds `v_game_flat` |
| 006 | IGDB link: `game.igdb_synced_at`, `igdb_checked_at`, `igdb_not_found`, and the `sync_candidate` table |
| 007 | IGDB enrichment: `game.time_to_beat_hastily/normally/completely` (minutes), `game.igdb_enriched_at`, lookup tables `game_mode` and `perspective` with their junctions, and `game_link` (store links) |
| 008 | `game.igdb_series`: IGDB's collection name for a linked game (never shown; the series rule below uses it) |
| 009 | `screenshot`: the per-game gallery (metadata only; the files are on disk, see "Screenshots" in section 5) |
| 010 | rankings: `ranking`, `ranking_tier`, `ranking_entry`; `game.poster_file`, `poster_src`, `poster_updated_at`; copies every per-year tier list into a ranking "Tier list <year>" (see "Rankings" in section 5) |

(`tier` and `tierlist_entry` are part of 001. Since 010 nothing reads or writes them: they stay in the file as they were, so no data is lost and a downgrade still finds its tier lists. A later migration may drop them.)

**Tables**

```
status(id, code, label, sort, color)           9 rows; order = display order of shelves
platform, genre, theme, ownership, series, company, player, game_mode, perspective(id, name UNIQUE NOCASE)   lookup values
game(id, title, title_sort, status_id, ownership_id, series_id, developer_id, release_year,
     vr, favorite, completed, notes, cover_url, cover_file, cover_updated_at,
     igdb_id UNIQUE, igdb_synced_at, igdb_checked_at, igdb_not_found, igdb_enriched_at, igdb_series,
     time_to_beat_hastily, time_to_beat_normally, time_to_beat_completely (minutes, NULL = unknown), steamgriddb_id,
     queue_pos, source_id UNIQUE, added_at, updated_at)
sync_candidate(id, game_id, igdb_id, name, year, score, platforms, slug, status pending/accepted/rejected)   proposals waiting in the review queue; UNIQUE(game_id, igdb_id)
game_platform / game_genre / game_theme / game_player / game_game_mode / game_perspective (game_id, x_id)   many-to-many, indexed both ways
game_link(game_id, kind steam/gog/epic/official, url http(s))   store links from IGDB, at most one per kind; PK (game_id, kind); cascades with the game
screenshot(id, game_id, file, thumb, width, height, bytes, taken_at NULL, caption NULL, pos, added_at)   index (game_id, pos); cascades with the game
play(id, game_id, year NULL, period NULL)      one row per year played, or period 'childhood' / 'always'
saved_view(id, name, icon, filter_json, sort_json, group_by, layout, pinned, pos, ...)
game_fts                                       FTS5 (title, series, notes), kept by db.IndexGame (the UI searches client-side; not used by it yet)
tier(id, name UNIQUE, color, sort)             S, A, B, C, D, F (seeded in 001; unused since 010)
tierlist_entry(year, game_id, tier_id, pos)    the old per-year tier list (unused since 010; its rows were copied into rankings)
ranking(id, name, description NULL, year NULL, pos, created_at, updated_at)   a custom tier list; `pos` is the order on the list page; `year` (1970 to 2100) marks "the ranking of that year": Year in review's **Rank this year** opens the first ranking with that year (migration 010 sets it on the converted tier lists; a copy has none) and adds the year's games to its pool
ranking_tier(id, ranking_id, label, color, pos)    a tier of one ranking (label up to 80 characters, color #rrggbb); cascades with the ranking
ranking_entry(ranking_id, game_id, tier_id NULL, pos)   PK (ranking_id, game_id); tier_id NULL is the ranking's pool of unranked games; ON DELETE SET NULL for the tier, CASCADE for the ranking and the game
game.poster_file / poster_src / poster_updated_at   portrait box art (file p<id>.jpg in the covers folder); src is igdb, sgdb or crop; updated_at is the cache key (`poster_v` in the API)
v_game_flat                                    flattened read view (group_concat of the links)
```

**Rules and conventions**
- Lookup values are created on demand with `db.GetOrCreate(tx, table, name)` (case-insensitive; the table name is checked against an allow-list).
- `status.code` (`playing`, `playing_evergreen`, `up_next`, `backlog`, `hold`, `evergreen`, `finished`, `dropped`, `not_interested`) is what the API and the views use; labels are display only.
- **Manual order:** `game.queue_pos` is one global order shared by all views. Reordering takes the ids a view shows in their new order and gives them **the positions those games already occupy** (`SlotOrder` in `internal/api/games_write.go`, `slotOrder` in `web/static/js/order.js`: keep the two in sync), so the other games never move. A game that changes status, or is created, goes to the end (`max(queue_pos)+1`).
- **Years played** are `play` rows. Moving a game to `playing`, `playing_evergreen` or `finished` inserts the current year (`recordCurrentYear`, ignored if present). An explicit `years` list in the same request is applied first, so it cannot erase that year.
- **Themes** are folded by `db.NormalizeTheme` (`"4X (explore, ...)"` becomes `"4X"`) on **every** write path (importer, create, patch).
- **IGDB data is read-only in the app and score-free.** Game modes, perspectives, store links and time to beat are written only by `sync-igdb --enrich` (there is no UI to edit them); IGDB's aggregated/total rating and hypes are never requested or stored (owner decision). The enrichment never changes `updated_at` (the Stats page reads it).
- **Changing the IGDB link clears the enrichment.** A `PATCH` that gives a game another `igdb_id`, or `0`/null, sets `igdb_enriched_at`, `igdb_series` and `time_to_beat_*` to NULL and deletes the game's `game_game_mode`, `game_perspective` and `game_link` rows in the same transaction (applyFieldEdits): that data belonged to the old IGDB game. The same id again clears nothing.
- **`igdb_id`** links a game to IGDB (unique). A patch that would give one id to two games answers 409 and applies nothing.
- **Cover cache key:** `cover_updated_at` changes only when the image changes and has millisecond resolution (`%f`), so two changes in one second get different versions. `updated_at` changes on every edit and must not be used as a cache key (editing a game would re-download its cover).
- `title_sort` (`db.TitleSort`) lowercases, drops a leading article and punctuation.

## 5. HTTP API

JSON in and out. Errors are `{"error": "..."}` with a status code (every handler and the guard; only a path or method no route knows gets Go's plain-text 404/405). Request bodies are size-limited. There is no authentication: the app is for the home network only.

**Security.** `protect` in `cmd/deltabacklog/guard.go` wraps the root handler (tests in `guard_test.go`). In order: (1) the `Host` header must be an IP literal, `localhost`, `deltabacklog.local`, the machine hostname, `<hostname>.local` or an `ALLOWED_HOSTS` name, else 421 (DNS rebinding); `GET`/`HEAD /healthz` is exempt; (2) `http.CrossOriginProtection` rejects cross-origin unsafe requests with 403 (browsers send `Sec-Fetch-Site`/`Origin`; curl and the app itself pass); (3) POST/PUT/PATCH under `/api/` need `Content-Type: application/json`, else 415, **except** `POST /api/games/<id>/screenshots`, the one route that takes `multipart/form-data` (`isShotUpload`; every other route still answers 415 to it, and step 2 already refused other sites). Every frontend write must send that header (`send`/`request` in `api.js`, the tier list `PUT` in `yearpage.js`). There is still no authentication; see `SECURITY.md`.

| Method and path | Purpose |
|---|---|
| `GET /api/games` | every game with its links (see below) |
| `GET /api/facets` | filter values with counts: `status` (key, label, color, count), `platform`, `genre`, `theme`, `mode`, `perspective`, `player`, `ownership`, `series`, `year` |
| `POST /api/games` | create a game |
| `PATCH /api/games/{id}` | change any editable field |
| `DELETE /api/games/{id}` | delete (204); links, plays and the cover file go with it |
| `POST /api/games/reorder` | body `[id, id, ...]` in the desired order; answers `{id: position}` |
| `POST /api/games/{id}/cover` | body `{"url": ...}` replaces the cover |
| `GET /api/lookup?q=` | IGDB search: `{configured, query, results:[candidate]}` |
| `GET /api/link/queue` | the IGDB review queue: `{items:[{game_id, title, year, platforms[], status, cover, `candidates:[{igdb_id, name, year, score, platforms[], slug}]}]}`; best proposals first, then games IGDB did not know |
| `POST /api/link/accept` | body `{game_id, igdb_id}`: links the game (a proposal or a lookup pick) and closes its other proposals; 204, `404` unknown game, `409 {code:"taken", id}` when another game already has that IGDB id, `409 {code:"already_linked", igdb_id}` when this game already has a *different* link (a link is never replaced; the same id again is a 204 no-op) |
| `POST /api/link/none` | body `{game_id}`: "not on IGDB"; the sync never searches that game again; 204 |
| `GET /api/covers/search?q=&source=&igdb_id=` | cover options, `source` = `steamgriddb` (default) or `igdb` |
| `GET /api/rankings` | `[{id, name, description, year, pos, updated_at, games, ranked, tiers:[{label,color}], preview:[game_id]}]` in list order; `preview` is the first 6 ranked games, best tier first |
| `POST /api/rankings` | body `{name, description?, year?, preset?: "sabcdf" (default) \| "empty", game_ids?}`; 201 with the full ranking; 400 for a blank or over-80-character name, an unknown preset or game, more than 2000 games, or 200 rankings already |
| `GET /api/rankings/{id}` | `{id, name, description, year, created_at, updated_at, tiers:[{id,label,color,pos}], entries:[{game_id, tier_id\|null, pos, poster, poster_v}], posters_pending}` (the editor reads the posters from the entries while the background worker runs); entries come tier by tier (in tier order) then the pool; asking also queues the posters the ranking's games still lack |
| `PATCH /api/rankings/{id}` | body `{name?, description?, year?}` (an empty description clears it, year 0 clears the year); the ranking |
| `DELETE /api/rankings/{id}` | 204; tiers and entries cascade, the games are untouched |
| `POST /api/rankings/{id}/duplicate` | optional body `{name?}` (default "<name> (copy)"); copies tiers and placements; 201 |
| `PUT /api/rankings/order` | body: every ranking id once, in the new order (400 otherwise); the list |
| `POST /api/rankings/{id}/tiers` | body `{label, color, index?}`; 201 with the tier; at most 30 per ranking |
| `PATCH /api/rankings/{id}/tiers/{tid}` | body `{label?, color?}`; the tier. A tier of another ranking is 404 |
| `DELETE /api/rankings/{id}/tiers/{tid}` | 204; its games go to the end of the pool in their order; the other tiers are renumbered |
| `PUT /api/rankings/{id}/tiers/order` | body: every tier id of the ranking once; the ranking |
| `POST /api/rankings/{id}/entries` | body `{game_ids, tier_id?}`: adds games (to the pool, or the end of a tier), skipping those already in; `{added:[ids]}`; queues their posters; 400 for an unknown game or tier |
| `POST /api/rankings/{id}/move` | body `{game_ids, tier_id\|null, index?}`: moves games already in the ranking to a tier (null = pool) at `index` (default: the end, counted without the moved games); the others keep their order; the ranking |
| `DELETE /api/rankings/{id}/entries/{gid}` | 204; 404 when the game is not in the ranking |
| `PUT /api/rankings/{id}/board` | what the editor saves: `{tiers:[{id,label,color,games:[ids]}], pool:[ids]}` (both keys required). Sets label, colour, order and games of the listed tiers and the pool; see Rankings below |
| `GET/POST /api/views`, `PATCH/DELETE /api/views/{id}`, `PUT /api/views/order` | saved views |
| `GET /api/games/{id}/screenshots` | `{screenshots:[{id, game_id, src, thumb, width, height, bytes, taken_at, caption, pos, added_at}], available}`; `available` is false while the storage folder is missing or not writable |
| `POST /api/games/{id}/screenshots` | multipart upload, one or more `file` parts (an optional `modified` part before a file carries `File.lastModified` in ms). Answers `{results:[{name, screenshot | error}]}` (one bad file does not stop the others) with `200`, one entry per file in order. An optional `thumb` part right after a file carries a small JPEG made by the browser (used only when the original is too big to decode here, see section 5). When the request limit cuts the upload short the status is still `200`: the files before it are stored and one last entry (without a name) says "the upload is larger than N MB". At most 50 `file` parts and 200 parts in all: more answers `413 {"error":"too many files in one request","results":[...]}` (the results are those stored before the cut) and reading stops. Other answers: `503 {"error":"screenshot storage not available"}`, `404` |
| `PUT /api/games/{id}/screenshots/order` | body `[id, ...]` naming every screenshot of the game once; answers the fresh list |
| `GET /api/screenshots` | every screenshot of the library, one page at a time, for the Screenshots page: `{screenshots:[{...the per-game fields, game_title, date}], total, offset, limit, years, available}`. `date` is `taken_at`, else `added_at`; `years` (every year that has a screenshot, newest first) and `available` come with the first page only (`offset=0`). Paging is by offset, so a picture added or deleted between two pages can show twice or be skipped until reload. Query: `sort=newest` (default, by `date` descending) or `sort=game` (game title, then the game's own order), `q` (part of the game title or the caption), `year`, `game_id`, `limit` (default 120, at most 500), `offset`; a bad value is `400`. Database rows only (no file is touched), so it works with the drive unmounted (`available:false`). `internal/api/shotlist.go` |
| `PATCH /api/screenshots/{id}` | body `{"caption": "..."}` (at most 500 characters, `""` clears) |
| `DELETE /api/screenshots/{id}` | deletes the row and both files (204) |
| `GET /screenshots/<game id>/<file>` | the original or the thumbnail; `?download=1` adds `Content-Disposition: attachment` |
| `GET /covers/<file>` | cover images (cached for 30 days; the URL carries `?v=cover_v`) |
| `GET /healthz` | `ok` when the database answers |

**Game (read model):** `id, title, status` (code)`, ownership, series, developer, release_year, vr, favorite, completed, notes, cover` (file name)`, queue_pos, igdb_id, added_at, updated_at, cover_v, platforms[], genres[], themes[], players[], years[]` (calendar years, ascending)`, periods[]`, and from the IGDB enrichment (empty until it ran): `ttb_hastily, ttb_normally, ttb_completely` (minutes or `null`), `modes[]`, `perspectives[]`, `links[]` (`{kind: steam|gog|epic|official, url}`).

**`PATCH /api/games/{id}`** accepts any of: `status, favorite, vr, completed, title, notes, ownership, series, developer, release_year, platforms[], genres[], themes[], players[], years[], periods[], igdb_id`. Absent fields are untouched. Text lookups (`ownership, series, developer`) clear with `""`, numbers (`release_year`, `igdb_id`) with `0`, lists replace the whole set (`years` only the calendar years, `periods` only the periods). Validation: non-empty title (at most 200 characters), `release_year` 1950 to 2100, `years` 1970 to 2100, `periods` of `childhood` or `always`, at most 50 list items, names at most 120 characters. Answers the updated game, `400` for invalid input, `404`, or `409` for an `igdb_id` that another game has.

**`POST /api/games`** takes the same fields plus `cover_url` (and requires `title`; `status` defaults to `backlog`). Answers `201 {"game": {...}, "cover_error": ""}`; a cover that fails to download does **not** fail the creation (the message is in `cover_error`). `409 {"id": <existing>}` when the `igdb_id` already exists.

**Screenshots** (`internal/api/screenshots.go`, `internal/shots`). The original is stored byte for byte (nothing stripped) as `<SCREENSHOTS_DIR>/<game id>/<32 hex>.<jpg|png|webp>` next to a 480px JPEG thumbnail `<32 hex>_t.jpg`; the name is random, never the client's. Upload: `MultipartReader` streams each part to a temporary file (no buffering in memory; `MaxBytesReader` caps the request), the type is decided by sniffing the first bytes (JPEG, PNG, WebP; SVG, HTML, GIF and the rest are refused whatever the name or `Content-Type` says), `image.DecodeConfig` reads the size (a hard sanity cap of 250 MP, computed in `int64`), and `shots.DecodeCost` estimates the peak memory of decoding the whole picture from that plus a tiny header parse: a JPEG costs one byte per sample (4:2:0 is 1.5 B/px, 4:4:4 is 3, a 4-component one, or a 3-component one stored as RGB (Adobe transform 0, or component ids R, G, B), adds 4 B/px for the extra RGBA/CMYK image) and a progressive one adds 4 bytes per sample for its coefficient blocks; a PNG costs 1, 2, 4 or 8 B/px by colour model (16-bit RGBA is 8); a WebP 4. Measured on a 40 MP JPEG: baseline 4:2:0 about 60 MB, progressive 4:2:0 about 285 MB, progressive 4:4:4 about 570 MB (the Pi's `MemoryMax` is 160 MB). **Budget:** `SCREENSHOTS_DECODE_BUDGET_MB` (default 48). Within it, **one image at a time** (`decodeMu`) is decoded and drawn with `draw.ApproxBiLinear` into a fresh small RGBA (`draw.BiLinear` allocates a scratch buffer of cropped height x 480 x 32 bytes that the estimate does not count: 148 MB for a 47 MP grayscale JPEG) that fits in 480x960 (a picture taller than 1:2 or wider than 4:1 is centre-cropped to that ratio; the grid uses `object-fit: cover`). Over it, the untouched original is still stored but the server does not decode it: the browser makes the thumbnail (`createImageBitmap` with `resizeWidth`, then a canvas JPEG of at most 480x960) and sends it as a `thumb` part after the `file` part; `shots.WriteClientThumb` accepts it only if it is a JPEG of at most 512 KB and 480x960 that decodes (cheap at that size). Without a valid `thumb` part the file is removed and the result says it is too big to preview here. The refusal result carries `needs_thumb: true` with the picture's `width` and `height` (as shown, EXIF applied); the UI then makes the thumbnail with the same crop rule as the server (`thumbPlan` in `shots.js`, kept in sync with `thumbPlan` in `shots.go`), using `createImageBitmap` with a source rectangle and a target size so the browser never builds a big bitmap, and sends the file again with the `thumb` part (an API client can send the `thumb` part right away; the server ignores it when it can make its own). If the browser cannot make one the user is told so. EXIF (JPEG only, `exif.go`): orientation (applied to the thumbnail; the browser applies it to the original) and `DateTimeOriginal` (`taken_at`, no zone); without EXIF `taken_at` is the file's last-modified time sent by the client. Serving accepts only names matching `shots.NameRE`, so no path trick reaches another file; both kinds get `X-Content-Type-Options: nosniff` and a one-year immutable cache (names never change content). Deleting a game removes `<SCREENSHOTS_DIR>/<id>/` after the transaction committed; deleting one screenshot removes its two files. **Storage rule:** `shotStorageOK` needs `SCREENSHOTS_DIR` to exist as a folder and be writable (a probe file) at every upload and every delete (a delete answers 503 before touching the row, so no file is orphaned); the list request uses `shotStorageReadable`, which only checks the folder and `access(W_OK)` (no write, so opening a drawer does not wake an idle drive). A game's folder is created only after the content passed the type check, so a refused upload leaves no empty folder. At start-up, when the storage is available, `SweepShots` tidies the numeric game folders, touching only files older than an hour and **never deleting an original** (after a database restore the drive may hold the only copy of pictures the older database does not list): it removes `*.tmp` files and thumbnails whose original is gone; an original without a row whose game exists is **re-adopted** (a row is inserted again from `Inspect`: size, EXIF date; the thumbnail is made again if it is missing and the picture fits the decoding budget, otherwise it shows as "Missing"); an original of a game this database does not know (or a file that is not a readable image) is **moved** with its thumbnail to `<SCREENSHOTS_DIR>/.orphans/<game id>/`. The sweep stops at the first database error, so an incomplete list of rows can never turn pictures into orphans. Anything else on the drive is left alone, and the result is logged. Limits of re-adoption: game ids are reused (no AUTOINCREMENT), so a file is adopted only if its modification time is not older than the game's `added_at` minus 10 minutes (a picture cannot predate its game); an older one belongs to an earlier game with that id and is moved to `.orphans/`. The adopted picture's `taken_at` is the EXIF date or else the file's modification time (the upload time, not the photo's date). An over-budget original whose thumbnail file was lost keeps showing "Missing": nothing regenerates it, because only the browser can make that thumbnail (upload it again if it matters). `WriteThumb` runs `runtime.GC()` before it releases `decodeMu`, so two decodes in a row never keep two bitmaps in the heap. The browser makes the thumbnail before the upload for files over 8 MB (`BIG_UPLOAD`, size read with an `Image`), so a big photo is sent once; the `needs_thumb` retry remains for smaller files that still exceed the budget. The server never creates `SCREENSHOTS_DIR` itself, only the per-game subfolders; `main` creates it only for the default `$DATA_DIR/screenshots`. So an external drive that is not mounted (an empty mount point on the SD card, or a path that does not exist) can never fill the SD card: uploads answer 503 "screenshot storage not available", the list says `available: false` (the drawer shows a notice and missing thumbnails show "Missing"), and everything else keeps working. At start-up a warning is logged.

**Rankings** (`internal/api/rankings.go`, migration 010). Validation: name and tier label 1 to 80 characters (trimmed), description up to 500, colour `#rrggbb` (stored lower case), at most 200 rankings, 30 tiers per ranking, 2000 games per ranking; every JSON body is limited (64KB, 4KB for tiers, 256KB for the board). Positions are only an order (gaps allowed, ties broken by game id); the endpoints that move things rewrite the affected lists densely. `ranking_entry.tier_id` must belong to the same ranking: the API checks it (the foreign key only knows the table). Deleting a tier first appends its entries to the pool, then deletes the tier. **`PUT .../board` is deliberately partial** because the editor saves through a debounced queue and other requests (adding a tier or a game) can overtake a save: it only touches what it lists. A tier it does not list keeps its place (and is moved after the listed ones in the order), a game it does not list stays in its tier after the listed games, a game id that is not in the ranking is ignored (a game removed meanwhile is not resurrected), and so is a tier id that no longer exists (a delete overtook the save; its games are already in the pool), but a tier id that belongs to another ranking, a game listed twice, a bad label or colour, or a missing key is a 400 and changes nothing. Tiers are added and deleted only through their own endpoints, never by the board save. The old tier list endpoints (`/api/tierlist/{year}`) are gone.

**Cache and other processes.** The cached `/api/games` and `/api/facets` responses are also dropped when `PRAGMA data_version` changes (`respCache.version`, read on every `get`): it changes when another connection or process commits, so `fetch-posters` or `sync-igdb` run next to the live service show up at once, without a restart (test: `TestGamesCacheSeesWritesFromAnotherConnection`).

**Migration 010 and the old tier lists.** For every year that has rows in `tierlist_entry` it creates the ranking "Tier list <year>" with `year` set (ordered newest year first), copies the six `tier` rows into it (name -> label, same colour and order), copies the entries (also those of games that lost that year since: nothing is dropped) and puts the other games played that year in the pool. The old tables are left alone. Test: `internal/db/rankings_migration_test.go` builds a version 9 database with tier lists, custom tier names and a ghost entry and checks all of it, plus the cascades.

**Posters** (`internal/covers/poster.go`, `internal/posters`). A poster is a centre-cropped 2:3 JPEG, at most 300 px wide (never scaled up), stored as `p<game id>.jpg` in the covers folder, so it is served by `/covers/` with the same cache headers, deleted with the game, and cached by the service worker's `covers-v2` cache (keyed by path; an entry is used only when its URL carries the requested `?v=`); `game.poster_file` names it and `poster_updated_at` is its cache key (`poster`, `poster_v` in `/api/games`, `posterURL()` in `api.js`). Sources, in order (`posters.Maker.Make`): (1) the IGDB cover of the game's `igdb_id` (`meta.IGDBCoverURL`, size `t_cover_big_2x`), (2) a SteamGridDB 600x900 static grid for the best title match (`meta.SteamGridPortrait`), (3) a centre crop of the wide cover already on disk (no network). A source that has nothing is skipped; `poster_src` records which one won (`igdb`, `sgdb`, `crop`). A source that **fails for now** (network error, 429, a 5xx, a download that fails to connect) makes `Make` return `ErrTemporary`: no poster is stored, not even the crop, so a later try can find the real one (the queue waits ten times longer and does not retry that game until the program restarts; `fetch-posters` just reports it, run it again). A **permanently bad** download (a 4xx such as 404, a file that is not an image, too large or too small: `covers.ErrBadImage`, `covers.StatusError.Permanent`) does not count as temporary: the next source is tried and then the crop. The crop is only made when the sources really have nothing, or are not configured. Downloads go through the same host allow-list as client-supplied covers (`coverHostAllowed`), `covers.FetchPosterStrict` (no redirects, 15 MB cap) and the same 16 megapixel decode cap as covers. Making them: adding games to a ranking (and opening a ranking) hands the games without a poster to `posters.Queue`, a **single background goroutine** that makes one poster at a time with a pause after each (800 ms; IGDB allows 4 requests a second), never blocks a request, bounded at 1024 waiting games, and remembers games that failed until the program restarts (so a game with no source is not retried on every visit). `GET /api/rankings/{id}` reports `posters_pending`; the editor polls it every 2.5 s, copies the new posters into the loaded games (`applyPosters`, which returns the changed ids) and swaps the picture in those tiles only (`refreshTiles`); the poll never rebuilds the board. When a poster is stored the queue drops the cached `/api/games` response (`OnMade`). `deltabacklog fetch-posters [--dry-run] [--limit N] [--upgrade] [--db path]` makes the poster of every game that has none, one every second (`--upgrade` also redoes the `crop` ones, for example after `sync-igdb` linked more games); `--dry-run` uses no network and writes nothing. Memory: one image is decoded at a time (a poster source is about 500x750), so the run fits the Pi's `GOMEMLIMIT=96MiB` (measured: 40 games from the crop source, peak working set 64 MB with `GOMEMLIMIT=96MiB`).

**Safety limits** (enforced server-side, covered by tests)
- A client-supplied cover URL (and every poster URL the server fetches) must be on `cdn2.steamgriddb.com`, `cdn.steamgriddb.com` or `images.igdb.com`; redirects are never followed; images larger than 16 megapixels are rejected (computed in `int64` because the Pi is 32-bit).
- A pasted link is only fetched from Steam, GOG, Epic or IGDB, every redirect is re-checked against that list, and the page must answer 200. (The UI no longer advertises links; the resolver remains in `internal/meta`.)
- Screenshots: 25 MB per file and 200 MB per request by default (`SCREENSHOT_MAX_MB`, `SCREENSHOT_REQUEST_MAX_MB`), 50 files and 200 parts per request, decoding budget 48 MB (`SCREENSHOTS_DECODE_BUDGET_MB`), caption 500 characters, order list 64KB.
- Request body limits: 64KB for games, views and rankings, 256KB for reorder and for a ranking board, 4KB for a tier, a cover URL and the link calls.
- `GET /covers/` and any other folder path answer 404: the cover folder is never listed.

## 6. Backend packages

- **`internal/db`**: `Open` (pragmas, single connection, migrations), `Migrate`, `GetOrCreate`, `IndexGame` (refresh one FTS row), `TitleSort`, `NormalizeTheme`.
- **`internal/importer`**: `Import(db, csv, options)` splits comma lists (respecting parentheses), parses `Years_played` (`2025,2026`, `Childhood`, `Always`), maps the NocoDB attachment JSON to a cover URL, and returns a report with warnings plus the list of covers to download.
- **`internal/api`**: handlers use a small `pathID`, `writeJSON`/`writeJSONStatus` (content type is set **before** the status) and `writeErr`. `LoadGames` builds the whole read model with one query per link table. `applyFieldEdits` is the single place that writes editable fields (used by patch and create), inside the caller's transaction, and re-indexes the FTS row.
- **`internal/meta`**: `Client` with overridable base URLs (so tests use local fakes). IGDB access tokens are cached until just before they expire. `SearchIGDB` (up to 8 candidates; when IGDB's title search finds nothing it retries with a `where alternative_names.name ~ *"title"*` query, then the part before `:` or `-`, then the title without spaces, which rescues localized titles and spacing differences), `IGDBImages` (artworks and screenshots with their real sizes, then the box cover), `SearchCovers` (SteamGridDB, wide shapes first), `ResolveLink`. A non-200 answer is a `*meta.HTTPError` (the sync reads its `Status` to back off on 429). `TitleScore(a, b)` rates two titles 0 to 100 ignoring case, accents, punctuation, a leading "the" and roman numerals; titles with different numbers ("Resident Evil 2" and "3") are capped at 59, and a title that starts the other ("Dark Souls III" and "... The Ringed City") scores at least 75.
- **`internal/igdblink`**: the IGDB sync (see section 8). Phase 2 is `enrich.go`: `Enrich` reads the linked games from IGDB in batches (`meta.Details`, `meta.TimeToBeat`) and `enrichGame` stores one game inside the batch's transaction. Phase 1: `Classify` sorts one game's IGDB results into *confident* (exactly one candidate with `meta.TitleScore` >= 90 **and** a shared platform), *uncertain* (best score 60 to 89, no shared platform, or several equally good candidates; at most five stored) or *not found*. A candidate is scored against its name and its IGDB alternative names (localized titles), whichever is best. When several candidates are strong, a single one whose name equals the title exactly (`meta.SameName`: case and accents folded, "the" kept) wins. Uncertain and not-found games also keep IGDB's first three results as weak proposals (score under 60 is greyed in the UI), because they cost nothing and save a manual search. `db.FileURI` escapes `%`, `#` and `?` in database paths (they would otherwise be read as URI query or fragment). `Run` is the resumable runner (single goroutine, a 250 ms pause plus the request itself so about 1.4 searches/s, doubling backoff on 429), `Queue`/`Accept`/`NotOnIGDB` are the review queue, `Main` is the `sync-igdb` command (`--enrich` runs phase 2). Only the link is stored: no IGDB scores or ratings.
- **`internal/shots`**: `Sniff` (magic bytes), `Inspect` (type, size, pixel cap, EXIF), `WriteThumb`, `NameRE`, `SyncNew` (the incremental copy behind `backup-screenshots`: copies files missing from the destination through a temporary name, never overwrites or deletes). `exif.go` reads only the APP1 segment and returns defaults on anything odd.
- **`internal/covers`**: `Fetch` (importer) and `FetchStrict` (client URLs, no redirects), `Save` (decode, size guard, scale to at most 480px wide, write `<id>.jpg` atomically).

## 7. Frontend

Plain ES modules loaded from `index.html`; hash routing (`#/library`, `#/view/<id>`, `#/views`, `#/years[/<year>]`, `#/series`, `#/stats`, `#/link`). The phone's More sheet lists the pages in `MORE_PAGES` order (`more.js`: Series, Stats, Year in review, Views, then Link IGDB); the desktop sidebar sorts them by `SIDEBAR_ORDER` in `main.js` (Year in review, Series, Stats, Views, Link IGDB). No dependencies, no build.

**Modules (`web/static/js/`)**

| Module | Role |
|---|---|
| `main.js`, `more.js` | router, navigation (sidebar on desktop; on phones the bottom bar holds Library, the first two pinned views and **More**, a sheet from `more.js` listing Series, Stats, Year in review and Views; the same links stay in the sidebar and are hidden on phones with CSS), theme, "+" button, service worker |
| `api.js` | the `store` (games, statuses, views), `load()`, edits (`patchGame`, `reorderGames`, `createGame`, `deleteGame`, `setCover`), view calls, lookups, screenshot calls (`uploadShot` is XHR for progress) |
| `library.js` | the Library and saved-view page: toolbar, filter panel, cards, reorder mode, table mounting |
| `filters.js` | **pure**: filter state, `matches`, include/exclude cycling, (de)serialisation to URL and view JSON, `facetSignature` |
| `sort.js` | **pure**: sort fields, comparator, group-by, and the sort sheet |
| `order.js`, `reorder.js` | slot reuse maths (pure) and the drag list |
| `table.js` | windowed table with inline editing, column picker, CSV export; `csv.js` is the pure CSV writer |
| `drawer.js` | game details, quick changes, edit mode, delete |
| `gallery.js`, `gallerypage.js` | the Screenshots page (`#/screenshots`). `gallery.js` is **pure** (the API query, grouping neighbouring pictures by game, the "4+" label of a cut run, viewer stepping across pages). `gallerypage.js` loads pages from `GET /api/screenshots` (the newest request wins; Show more is one at a time and never overlaps a first-page load), appends new pictures instead of redrawing, and reloads what is on screen when `screenshots-changed` (sent by `shotsview.js` after an upload, delete, caption or reorder) or `games-changed` fires |
| `shotviewer.js` | the full-screen viewer frame shared by the drawer (`shotsview.js`: caption, move, delete) and the Screenshots page (game name, Open game, step across pages): bar, picture, prev/next, swipe, keys, focus trap, date/size line and download button. Fix viewer behaviour here, once |
| `shots.js`, `shotsview.js` | the drawer's Screenshots section. `shots.js` is **pure** (move earlier/later, viewer stepping, byte and date formatting, picking JPEG/PNG/WebP files, the upload summary). `shotsview.js` draws the lazy thumbnail grid, uploads one file at a time with `XMLHttpRequest` (progress per file, the error per file stays visible), accepts drops on desktop, and the viewer's caption, move and delete (the frame is `shotviewer.js`). Delete is hidden at once and sent after 6 seconds, so the toast's Undo is a local cancel |
| `editpatch.js` | **pure**: form state from a game, validation, and the minimal patch of what changed |
| `form.js`, `picker.js` | shared form controls and the multi-select sheet |
| `add.js` | add sheet, IGDB match picker, cover picker, and `makeSheet`, the shared scrim + sheet. `makeSheet` focuses the sheet on open and returns focus to the opener on close (not to a text field). Re-render with `sheet.render(fn)`, which keeps focus on the control with the same `data-fk` (else the primary footer button, else the sheet); announce with `sheet.say(text)`, one persistent `role=status` region. New sheets should use both instead of `replaceChildren` alone |
| `ttb.js` | **pure**: time to beat: which estimate to use (`ttbMinutes`: usual, else quick, else complete), `isShort` (under 10 h; unknown is never short), `formatTime`, `totalTime` |
| `pick.js` | **pure**: Surprise me rules: pool (Up next, else Backlog; never finished/dropped/not interested/evergreen unless filtered), mood narrowing and chip counts, seedable RNG, pick without repeats, the status "Play it" sets (`playTarget`) |
| `surprise.js` | the Surprise me sheet (mood, result card, skip list in `sessionStorage`); opened from the Library/view header |
| `views.js` | the "Views" management page |
| `yearreview.js` | **pure**: year grouping (`groupOf`), breakdowns, year strip and neighbours, route parsing, the name of a year's ranking (`yearRankingName`, `findYearRanking`) |
| `yearpage.js` | the `#/years/<year>` page (stats, the games by outcome, and **Rank this year**, which opens the ranking named "Tier list <year>" or creates it with that year's games) |
| `rankboard.js` | **pure**, tested (`fitExportLayout` widens the exported picture until a phone canvas can draw it): the board state (`{tiers:[{id,label,color,games}], pool}`), `place` / `shift` / `carry` / tier edits and the undo of a tier delete, `dropIndex` (where a dragged tile lands in a wrapped row), tile sizes (S/M/L, phone and desktop), `wrapText`, `exportLayout` (the PNG's geometry), `textOn` (readable text on a colour) |
| `rankingspage.js`, `rankingedit.js` | `#/rankings` (the list: new, rename, duplicate, delete, reorder) and `#/rankings/<id>` (the board: dark rows with a coloured wrapping label cell, poster tiles, the pool, tier sheet, selection bar, keyboard, S/M/L and Titles kept in `localStorage`). Saves through `savequeue.js` and `PUT .../board`; tiers and games are added and removed through their own endpoints (awaited, then the board is saved) |
| `rankdrag.js` | pointer-event dragging of tiles (mouse at once, touch after a long press, edge auto-scroll, a click after a drag is swallowed) |
| `gamepicker.js` | the "Add games" sheet: search and the Library's filters (`filters.js`) over the whole library, "select these N" |
| `tierpng.js` | the canvas PNG export of a ranking (client-side, no library, full-size 90x135 posters, falls back to the wide cover, then a placeholder). `savequeue.js` (**pure**, tested) is the autosave: debounce, one request at a time, last state wins, no retry of a 400, a final keepalive request when the page goes away |
| `stats.js`, `statspage.js` | the `#/stats` page: `stats.js` is **pure** (backlog size, finished per year, time in status, top genres, finished vs dropped, chart scale); `statspage.js` draws it (HTML bars, inline SVG chart with a screen-reader table). Computed client-side from `store.games`; no endpoint, no tables |
| `series.js`, `seriesview.js` | **pure** series grouping, progress counts and the "3 of 5" position (definitions of finished / played / left are in the header of `series.js` and in the user manual, section 12); and the `#/series` page. Client-side only: it groups `store.games` by series name (case-insensitive) and redraws on `games-changed` |
| `link.js`, `linkqueue.js` | the "Link with IGDB" review screen (route `#/link`; the navigation shows its link while the queue is not empty) and its **pure** logic (score tone, progress text, skipped games) |
| `platforms.js`, `ownership.js`, `years.js` | pure helpers: IGDB platform mapping, ownership emojis, card year chips |
| `util.js` | `h()` element builder, dialogs, toast, icons |

**State and events.** `store.games` is one array held in memory, mutated in place. Editing is **optimistic**: `patchGame` applies the change locally, then sends it; edits to one game are queued so a slow answer can never overwrite a newer edit; a failure restores the game from the server. Events on `window`:
- `games-changed` (`detail.id` = the game; `detail.reorder` when only positions moved): views rebuild only that card and refresh counts. If a new filter value appeared (a new genre, platform, year...) the library asks for a full rebuild.
- `games-reloaded`: a game was added or deleted; the router re-renders the page.
- `views-changed`: saved views changed; the navigation is rebuilt.

**Formats**
- Saved view `filter`: `{"q": "", "in": {"genre": ["RPG", "Indie"]}, "ex": {"status": ["finished"]}, "all": ["genre"], "flags": ["favorite"]}`. Facet keys: `status, platform, genre, theme, year, ownership, mode, perspective, player`; flags: `short` (time to beat under 10 h; has a `test` in `FLAGS` instead of reading a game field), `favorite, vr, completed`.
- `sort`: `[{"field": "status", "dir": "asc"}, ...]` (fields in `SORT_FIELDS`); `group_by`: a key of `GROUP_FIELDS` or `""`; `layout`: `cards` or `table`.
- The Library (not a saved view) keeps the same state in the URL query, e.g. `#/library?genre=RPG&ex_status=finished&all=genre&sort=title:asc&layout=table`.

**Table.** Rows have a fixed height; only the visible ones exist in the DOM (`paint`). Each row remembers a signature of what it shows, so a refresh rebuilds only rows whose data changed (a click that started on an unchanged row still completes). The row being edited is never destroyed by a refresh. Column widths and visibility are kept in `localStorage` (a per-device convenience).

**Service worker (`sw.js`).** Network first for the app shell and the API (the cache is only an offline fallback), cache first for `/covers/*` (their URLs carry the cover version; the `covers-v2` cache holds one entry per cover file, keyed by path, so a new version replaces the old one, and an older entry is served only when the network fails), never cache `GET /api/screenshots` (every search and page is another URL), cache first for screenshot **thumbnails** only (`*_t.jpg`, newest 400 kept); an original under `/screenshots/` is never cached (the worker does not answer it). **Rule: bump `VERSION` whenever any shell file changes (HTML, CSS or a module), and add every new module to `SHELL_FILES`**; a stale `VERSION` leaves old clients on the old cache. Both are easy to forget when adding a page; `TestServiceWorkerCachesTheWholeShell` (`web/web_test.go`) fails when a module is missing from `SHELL_FILES` (the `VERSION` bump is still on you). The server sends `Cache-Control: no-cache` for the shell.

**Style.** `styles.css` uses CSS variables for light and dark (`prefers-color-scheme`, with a `data-theme` override). Breakpoints: below 600px phone layout, from 900px sidebar plus filter panel.

## 8. External services

- **IGDB** (Twitch client credentials, token cached): game details, candidate search, artworks and screenshots. Its genre names are what the library already uses. `platforms.js` maps IGDB platform names onto the library's names and pre-selects the platform the library has the most games on.
- **IGDB sync (phase 1: link every game).** `deltabacklog sync-igdb [--dry-run] [--db path] [--limit n]` (reads the keys from `.env`; the database defaults to `$DATA_DIR/app.db`). It searches IGDB by title for each game with no `igdb_id`, no earlier search and no "not on IGDB" mark, and buckets the answer (section 6). *Confident* matches set `igdb_id` and `igdb_synced_at`; *uncertain* ones are stored in `sync_candidate` for the review screen; every searched game gets `igdb_checked_at`, in the same transaction as its outcome, so a stopped run (Ctrl-C, repeated 429s, no network) simply continues next time. A confident match whose IGDB id another game already has goes to review instead (`igdb_id` is unique). A failed search leaves the game unchecked, so it is tried again. `--dry-run` copies the database with `VACUUM INTO` into a temporary folder (opened read-only, so an unmigrated file is not migrated), runs on the copy, prints a report (counts and examples per bucket) and deletes it: the database file is never written. A real run first writes a full backup, `app-before-igdb-link-<time>.db`, next to the database; **every** run does, including each `--limit` run, so mind the SD card. Pacing: 250 ms between searches *on top of* each request, so in practice about 1.4 requests per second (below IGDB's 4/s limit): about 7 minutes for 600 games. Restart the server afterwards: it caches `/api/games`, and the command runs in another process. Phase 1 stores nothing but the link; phase 2 (next item) reads the details. IGDB scores are never fetched or stored (owner decision).
- **IGDB sync (phase 2: enrich the linked games).** `deltabacklog sync-igdb --enrich [--dry-run] [--refresh] [--db path] [--limit n]`. For every game with an `igdb_id` and no `igdb_enriched_at` it asks IGDB, 20 games per request (`where id = (...)`), for the facts (`/games`: release date, genres, themes, platforms, developer, collection, game modes, player perspectives, website URLs) and for `/game_time_to_beats` (seconds; stored as minutes, rounded). **Fill only empty fields:** release year, developer, genres, themes, series and platform are written only while the game has none (the owner's values are never changed, and a lookup row such as a company or series is created only when a value is actually written, so IGDB's spelling never lands next to the owner's). The platform is filled only when IGDB names exactly one (a game's platform is where the owner has it, not where it was released) *and* it resolves to a platform the library already has through `PlatformKey` (the Go twin of `web/static/js/platforms.js`; the two alias tables must stay in sync, `TestPlatformAliasesMatchTheJS` checks it); the enrichment never creates a platform, and the report counts the ones it left. **Series** (`enrich_lookup.go`): IGDB names a collection after almost every standalone game, so a collection is used only when its folded name (`meta.FoldKey`: case, accents, `&` = `and`, letters and digits) equals a series the library has (the owner's spelling is kept), or when at least two library games share it (a pass after the batches, over every enriched game, so it also completes pairs across runs; IGDB's name is kept in `igdb_series` for that). Franchises are never used (they are not even requested). A `PATCH` that carries `series` (any value, also `""`) sets `igdb_series` to NULL: the owner has decided, and the pass never touches that game again. The report counts the series set on games read in this run ("series") separately from those set on games of earlier runs because a game read now completed their collection (`SeriesCompleted`). The other proposals are only listed in the report. **Plausibility:** a time over 1000 h is stored as NULL, and a game whose quick/usual/complete times contradict each other (quick > usual > complete) gets none. **New data** (time to beat, modes, perspectives, store links) is IGDB's: a later `--refresh` run re-reads the already enriched games and replaces **only** this data (a time IGDB no longer reports is kept); it never fills year, developer, genres, themes, platform or series, because a field the owner emptied on purpose looks like one that was never set. Store links are recognised by host (Steam, GOG, Epic) or IGDB's `official` website type, http(s) only, one per kind. A batch is written in one transaction together with its `igdb_enriched_at` marks, so a stopped run (Ctrl-C, 429s, no network) continues where it stopped; a failed request leaves its games unmarked (5 failures in a row stop the run); a linked game IGDB no longer returns is marked done and counted as missing. Pacing: 250 ms between requests (a batch is two requests, so about 4 requests per second at most), one goroutine, doubling backoff from 2 s on 429, or IGDB's `Retry-After` (up to a minute) when it sends one (6 retries, then the run stops, resumable). `--dry-run` works on a temporary `VACUUM INTO` copy like phase 1 (inside one transaction that is rolled back, so the series pass sees the whole run) and prints counts of what would be filled; a real run first writes `app-before-igdb-enrich-<time>.db` next to the database (every run does). `updated_at` is not touched. Restart the server afterwards (it caches `/api/games`). Nothing about scores is requested; `meta/enrich_test.go` fails if a query mentions rating, hypes, follows or score.
- **SteamGridDB**: `search/autocomplete` for the best (exact-title preferred) match, then `grids/game/{id}` with `dimensions=460x215,920x430`, falling back to any shape. For rankings, `SteamGridPortrait` asks for `dimensions=600x900` and takes the first grid; IGDB's `IGDBCoverURL` reads only `cover.image_id` (one small query) and builds the `t_cover_big_2x` URL.
- **Steam** (`appdetails`, no key): used only by the link resolver.

## 9. Testing

| What | How |
|---|---|
| Backend | `go vet ./... && go test ./...` (in-memory databases, `httptest` fake IGDB/SteamGridDB/Steam/image servers) |
| Pure frontend modules | `node --test web/test/*.test.mjs`: one file per pure module (`filters`, `sort`, `reorder` (also covers `order.js`), `editpatch`, `csv`, `colors`, `ownership`, `platforms`, `years`, `api`, `more`, `pick`, `series`, `stats`, `yearreview`, `rankboard`, `savequeue`, `linkqueue`, `ttb`, `shots`). Pages that draw the DOM (`*page.js`, `seriesview.js`, `surprise.js`, `link.js`) are checked in a browser |
| Backend, by area | `internal/api` (games, views, rankings, link endpoints), `internal/posters` (the poster maker, queue and `fetch-posters` with an offline fake source), `internal/covers` (poster crop and limits), `internal/db` (migrations, backup/check), `internal/meta`, `internal/igdblink`, `internal/importer`, `internal/covers`, `internal/shots` (sniffing, EXIF, rotation, incremental copy), `internal/api/screenshots_test.go` (streaming upload, size limits, content sniffing, traversal names, decompression bomb, cascade delete removes files, missing storage never created), `deploy/scripts_test.go` (`sh -n`, the free-space guard, the drop-in generation, the database copy to the drive; skipped where there is no `sh`) |
| IGDB sync | `internal/igdblink` tests use an offline fake IGDB (buckets, dry run writes nothing, backup, resume, 429 backoff, review queue, migration 006; `enrich_test.go`: fill-only-empty (also no new lookup rows), series rules, platform mapping and Go/JS alias parity, outlier times, Retry-After, batching, resume after throttling, backoff doubling, refresh, dry run, migration 007 on a populated database); `internal/meta/enrich_test.go` (query shape, no scores, store-link classification); `internal/api/enrich_test.go` (read model and facets); `internal/api/link_test.go` covers the three endpoints |
| Real services | `set -a; . ./.env; set +a; DELTABACKLOG_LIVE=1 go test ./internal/meta -run TestLive -v` |
| In a browser | headless Chromium driven by puppeteer against a **test server with a copy of the data** on a spare port; `docs/tools/screenshots.mjs` is a working example (it opens the main screens and saves the manual's images). By default it builds and serves the demo library from `docs/tools/demo-data` (40 well-known games, no personal data; generated placeholder covers, or the real covers downloaded into a throwaway folder when IGDB / SteamGridDB keys are in `.env` or `ENV_FILE`), so the images never show anyone's real collection. The images contain cover art that belongs to IGDB / SteamGridDB and the game publishers, shown for illustration; no cover files are committed |

Before merging a PR: all of the above pass; changes that touch the UI were looked at in a browser at 390px and 1280px (light and dark where it matters), with no horizontal page scroll. Every bug fix comes with a test that fails without it.

## 10. Workflow

- Branches: `main` plus `features/<name>`; changes reach `main` through pull requests with merge commits, never by committing to `main`.
- **Do not stack a PR on another unmerged PR for long.** A PR merged into its base branch is not in `main` (this has happened); merge each PR into `main` and branch the next one from `main`.
- Commit messages: a short summary line, a body saying what and why, and the `Co-Authored-By` line given by the session.
- **Secrets** (IGDB and SteamGridDB keys) live only in `.env`. `git grep` for keys before pushing.
- A change in behaviour updates the matching section of the **user manual** and, if it touches structure, the API or the data model, the **developer manual**, in the same PR.

## 11. Build and deploy

The program is **cross-compiled on the PC** and uploaded; the Pi never holds the source and never builds anything (compiling the SQLite library needs most of a 1GB Pi and minutes of CPU, which would starve the other services). It runs directly under **systemd**, with no container, to keep the memory use at about 15MB.

**Deploy or update (from the PC, Go on PATH, `ssh raspberry` working, or `DEPLOY_HOST=user@host`):**

```sh
bash deploy/deploy.sh                       # build + upload + install; data and settings are kept
bash deploy/deploy.sh --seed <dir> --env .env   # first install: <dir> has app.db and covers/; --env installs the keys
```

`deploy.sh` builds `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0` (version stamped from `git describe`), uploads to `/tmp/deltabacklog-deploy`, runs `install.sh` with sudo and deletes the upload. `install.sh` is idempotent: it never overwrites an existing database, restarts the service only when the program (or settings) changed, and waits until `/healthz` answers. When the program changed and a database exists, it first writes `/var/backups/deltabacklog/app-before-upgrade-<time>.db` (with the new binary, as the service user; `backup` never migrates, so this is the database as the old version left it), because the new version may migrate the database when it starts; if that backup fails, the program is not replaced. These copies are pruned with the nightly ones after 14 days.

**What is on the Pi**

| Path | What |
|---|---|
| `/opt/deltabacklog/deltabacklog` | the program; `backup.sh` next to it |
| `/var/lib/deltabacklog/` | the data: `app.db`, `covers/` (owner `deltabacklog`, mode 750) |
| `/var/backups/deltabacklog/` | nightly backups `app-YYYY-MM-DD.db`, last 14 days; `screenshots/` is the incremental copy of the screenshots (never pruned) |
| `$SCREENSHOTS_DIR` (default `/var/lib/deltabacklog/screenshots`) | the screenshots; on the Pi's external drive: `/mnt/ventodrive/deltabacklog/screenshots` (next to a `backups/` folder) |
| `/etc/deltabacklog.env` | settings and the IGDB / SteamGridDB keys (root only, mode 600); `ALLOWED_HOSTS=name1,name2` goes here when the app is reached by a name other than `deltabacklog.local`, an IP address or the Pi's hostname (otherwise 421 "unknown host"); restart the service after editing |
| `/etc/systemd/system/deltabacklog.service`, `deltabacklog-backup.service`, `deltabacklog-backup.timer`, `deltabacklog-name.service` | the units (sources in `deploy/`) |

**Starting at boot.** `deltabacklog.service` is enabled (`WantedBy=multi-user.target`) with `Restart=always` (a crashed process is back in about 4 seconds, tested with `kill -9`). It runs as the unprivileged system user `deltabacklog` with `NoNewPrivileges`, a read-only file system except its data folder, and `Nice=5`. The Pi's kernel does not enforce systemd memory limits (the memory cgroup is off on Raspbian), so the real cap is `GOMEMLIMIT=96MiB` in the settings file.

**Friendly address (`http://deltabacklog.local`).** `deltabacklog-name.service` runs `/opt/deltabacklog/name.sh` (source `deploy/name.sh`), which does two things: it announces the name `deltabacklog.local` with `avahi-publish` (mDNS, pointing at the source address of the default route, `ip -4 route get 1.1.1.1`), and it keeps two `iptables` NAT rules that send port 80 to 8081. The PREROUTING rule matches only traffic **addressed to the Pi** (`-m addrtype --dst-type LOCAL`), so containers on the Pi going out to port 80 elsewhere are untouched; the OUTPUT rule covers the Pi's own requests to itself. The app still listens only on `:8081`. The rules are added when the unit starts and removed when it stops (`ExecStopPost`, which also removes the first version of the rule that had no address match), so they come back at every boot and disappear when the unit stops (for example `systemctl disable --now`; `disable` alone keeps them until the next boot). Anything else listening on port 80 on the Pi (for example another web UI) is shadowed for traffic addressed to the Pi.

`install.sh` installs `avahi-daemon`, `avahi-utils` and `iptables` with apt (running `apt-get update` first, only when something is missing), installs `name.sh` and the unit, and enables it. **Re-running it also updates the name unit**: when the unit file or `name.sh` changed it clears (for a changed `name.sh`, the old script removes its own rules first) the old rules and restarts the unit, otherwise it just makes sure it is running. This step is optional: if it fails the install still succeeds and prints a warning. The unit was first installed by hand on the maintainer's Pi, so the first deploy of this version replaces it (the old rule is removed by `name.sh rules-del`). Caveats: mDNS needs a client that understands `.local` (Windows 10+, macOS, iOS and Linux with avahi do; **some Android versions do not**, and then `http://<ip>:8081` is the fallback), and the name keeps the address it had when the unit started, so give the Pi a fixed IP in the router.

**Backups.** `deltabacklog-backup.timer` runs `backup.sh` every night at 03:30 (with `Persistent=true`, so a missed run happens at the next boot). It calls `deltabacklog backup <file>`, which uses SQLite's `VACUUM INTO` for a consistent copy while the app runs, never overwrites an existing file and writes under a temporary name. Covers are not backed up (they can be downloaded again).

```sh
ssh <user>@<pi-address>   # the same host you gave as DEPLOY_HOST
sudo systemctl start deltabacklog-backup.service            # a backup right now
sudo /opt/deltabacklog/deltabacklog check /var/backups/deltabacklog/app-2026-10-07.db   # integrity, schema version, counts (read-only)
# restore: stop, put the copy in place, fix the owner, start
sudo systemctl stop deltabacklog
sudo cp /var/backups/deltabacklog/app-2026-10-07.db /var/lib/deltabacklog/app.db
sudo rm -f /var/lib/deltabacklog/app.db-wal /var/lib/deltabacklog/app.db-shm
sudo chown deltabacklog:deltabacklog /var/lib/deltabacklog/app.db
sudo systemctl start deltabacklog
```

**Screenshots on the external drive.** The SD card is small; screenshots live on the hard drive. Put `SCREENSHOTS_DIR=/mnt/ventodrive/deltabacklog/screenshots` in `/etc/deltabacklog.env` and run `deploy.sh` (or `install.sh`) again; a later `deploy.sh --env` keeps that line. `install.sh` is idempotent and does this: (1) writes the drop-ins `/etc/systemd/system/deltabacklog.service.d/screenshots.conf` (`ReadWritePaths=-<its parent>`, the drive's `deltabacklog` folder, which exists whenever the drive is mounted) and `deltabacklog-backup.service.d/screenshots.conf` (the same, because the backup also writes `<parent>/backups`), because `ProtectSystem=strict` leaves everything but the data folder read-only; the leading `-` makes a missing path harmless, and both drop-ins grant the drive's `deltabacklog` folder (the parent of the screenshots folder, which exists whenever the drive is mounted) rather than the screenshots folder itself; they also carry `After=<mount unit>` (from `systemd-escape --path --suffix=mount` of the mount point found by `findmnt`, mounted or in `fstab`), which only orders the start after the mount when both start at boot: there is **no** `Requires`/`Wants`/`RequiresMountsFor`, so the service still starts without the drive and answers "screenshot storage not available" until it is back (restart the service after mounting if it still says so; after a reboot, verify with `curl -s 127.0.0.1:8081/api/games/<id>/screenshots`, which must contain `"available":true`). When a drop-in is created or changes, `install.sh` restarts the service, also without `--env` or a new program; drop-ins are removed again when the folder is inside the data folder; (2) creates `<screenshots dir>` and `<parent>/backups` owned by `deltabacklog` **only when the folder and its parent are really on another mounted drive** (`on_other_mount`: `findmnt` must report a target other than `/`), so an unmounted drive's empty mount point on the SD card is never filled; it prints a warning instead. `SCREENSHOTS_DIR` must be a **folder inside the drive**: if it is the mount point itself (for example `/mnt/ventodrive`), `install.sh` refuses and exits, because the `backups` folder would be created beside it, on the SD card. The app itself never creates `SCREENSHOTS_DIR` (see section 5). Move existing screenshots (none on the first install) with `rsync -a /var/lib/deltabacklog/screenshots/ /mnt/ventodrive/deltabacklog/screenshots/` while the service is stopped.

**Backups with the drive.** Every night `backup.sh` does three things, and the last two never fail the database backup (a problem is logged and skipped): (1) the database copy in `/var/backups/deltabacklog` (SD card, 14 days); (2) when `SCREENSHOTS_DIR` is set and `<parent>/backups` exists, the same file there too, on the drive, 14 days, so a dead SD card does not take the database with it (the folder is never created by the script; `install.sh` makes it); (3) the screenshots: `deltabacklog backup-screenshots --min-free-gb N /var/backups/deltabacklog/screenshots` copies the files missing there, so the originals (drive) and the copy (SD card) are on **different devices**. Copies are never deleted, even for screenshots deleted in the app (retention is "forever"; disk use grows with the library: about the size of `<screenshots dir>`; clear `/var/backups/deltabacklog/screenshots` by hand if you do not want deleted pictures kept). The free-space guard lives in the Go subcommand (`backup.sh` passes `SHOTS_MIN_FREE_GB`, default 5, set it in `/etc/deltabacklog.env`): before each file it asks `statfs` (Linux only; elsewhere the check is skipped) for the free space of the backup disk and leaves the file when copying it would drop below the margin, with a log line at the end saying how many were left; smaller files that still fit are copied. Because it checks the files actually copied, pictures kept in the backup after being deleted in the app cannot make it underestimate. Restore after a lost SD card or a new install: install the program, put the database back (from the drive's `backups/` or the SD one) as in the steps above, set `SCREENSHOTS_DIR` to the drive's folder (the originals and thumbnails are intact there). The restored database may be older than some pictures on the drive: on the first start the sweep adds those pictures back (log line "re-adopted"), and never deletes them; pictures of games the restored database does not know are moved to `.orphans/<game id>/` on the drive. Lost the drive instead: stop the service, `cp -a /var/backups/deltabacklog/screenshots/. <new screenshots dir>/` (owner `deltabacklog`) and start it. To copy the pictures off the Pi: `rsync -a <pi>:/mnt/ventodrive/deltabacklog/screenshots/ ./screenshots-backup/`.

**Day to day:** `journalctl -u deltabacklog -f` (log), `systemctl status deltabacklog`, `systemctl list-timers deltabacklog-backup.timer`.

**Never test against the Pi's real database.** For experiments copy `app.db` to a temporary folder and run a second instance on another port (this is how the write timings below were measured), then delete it.

**HTTP only, so no service worker on the Pi.** Browsers only allow service workers and "Install app" on HTTPS or `localhost`. Served as `http://deltabacklog.local` or `http://<pi-address>:8081` the app works fully, but the offline cache is inactive, and "Add to Home screen" creates a plain shortcut. Real installation needs HTTPS (for example a reverse proxy with a certificate); not set up.

**Performance (measured on the Pi 3B+ from the PC over the LAN, with a real browser)**

What makes it fast:
- The two big reads (`/api/games`, `/api/facets`) are built once, gzip-compressed once, kept in memory with an `ETag`, and rebuilt in the background 400ms after the last write (`internal/api/cache.go`; `Server.Warm`). Reads cost no database work; a repeat visit is a 304 without a body.
- Editing one game loads only that game's rows (`loadGames(d, id)`), not the whole library.
- Static files are compressed and fingerprinted once at startup (`web/web.go`), served with `no-cache` + `ETag` (new deploys show at once; repeat visits are 304s), and `index.html` is sent with a `modulepreload` link for every module so the browser fetches the scripts in parallel instead of one import level at a time.
- Covers load only when they are about to scroll into view (`lazyImg` in `util.js`, an `IntersectionObserver`). The browser's own `loading="lazy"` was not enough: Chrome requested all 599 covers (15MB) on every revisit or reload.

| | Before | After |
|---|---|---|
| `GET /api/games`, requests per second | 9 | about 1,500 (gzip) |
| `GET /api/games`, time per request | 136 ms (930 ms with 8 clients) | 1 to 10 ms |
| `GET /api/games`, bytes on the wire | 285 KB | 22 KB |
| first request after a restart or a write | 210 to 280 ms | 3 ms (warmed in the background) |
| repeat visit, whole page | 15.6 MB, 624 requests | 5 KB, 39 requests (304) |
| first card on screen: LAN / 4G-like phone / 3G-like phone (CPU throttled 4x) | 2.9 s / 0.9 s / 1.8 s | 0.18 s / 0.9 s / 1.8 s |
| edit (PATCH), create, reorder | | 7 to 25 ms |
| memory of the process | | about 15 to 19 MB |

Under a sustained load of 8 clients the Pi's CPU reaches its thermal limit; ordinary use is far below that.

## 12. Known gaps

- Games imported from NocoDB have no `igdb_id` until `deltabacklog sync-igdb` (or **Fill from IGDB**) links them; duplicate detection falls back to the title. Run `deltabacklog sync-igdb` then `deltabacklog sync-igdb --enrich` to fill in the rest; until then the game mode, perspective and short-game filters are hidden and the Stats page says it has no times.
- Not built yet: a JSON export.
- Posters need IGDB or SteamGridDB keys to be good: without them (or for a game neither knows) the poster is a crop of the small wide cover, which is blurry at full tile size. `deltabacklog fetch-posters --upgrade` replaces those after the keys are set or more games are linked. Choosing another poster by hand from the game's details is not built.
- The old `tier` and `tierlist_entry` tables are unused but not dropped (migration 010 only copies them).
- No authentication (intended: home network only) and no HTTPS (see section 11: no offline cache or app install on the Pi).
- The pasted-link resolver (Steam, GOG, Epic, IGDB pages) exists in the backend but is not offered in the UI; only Steam links were tried against the real site.
