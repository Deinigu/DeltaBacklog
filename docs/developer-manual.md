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
cmd/deltabacklog/   the server and its subcommands (main.go: .env loading, wiring; `check`, `backup`, `sync-igdb`)
cmd/import/         one-off importer from a NocoDB CSV export (also downloads covers)
internal/db/        open, migrate (embedded migrations/*.sql), GetOrCreate, FTS index, TitleSort, NormalizeTheme
internal/importer/  CSV → normalized schema (idempotent)
internal/api/       HTTP handlers: games, games_write, add (create/delete/cover/lookup), facets, views
internal/meta/      IGDB, SteamGridDB and link resolution clients; TitleScore (fuzzy title match)
internal/igdblink/  the IGDB sync: matching buckets, resumable runner, review queue, `sync-igdb` command
internal/covers/    cover download (strict: no redirects) + resize + size guard
web/web.go          go:embed of web/static
web/static/         index.html, styles.css, sw.js, manifest, icon, js/*.js
web/test/           Node unit tests for the pure frontend modules (outside the embedded folder)
deploy/             deploy.sh (build + upload), install.sh (runs on the Pi), the systemd units and backup.sh
docs/               these manuals; docs/img screenshots; docs/tools/screenshots.mjs regenerates them from the demo library in docs/tools/demo-data
```

## 3. Running it

Go 1.27 and Node 20+ must be on your PATH (Node is only for the frontend tests and the screenshot tool).

```sh
go vet ./... && go test ./...
node --test web/test/*.test.mjs
go run ./cmd/deltabacklog          # :8081, data in ./data, keys from ./.env
```

`web/static` is compiled into the binary: **restart after any frontend change**.

**Configuration** (`.env` in the working directory, or real environment variables, which win; `.env` is git-ignored, `.env.example` is committed):

| Variable | Meaning |
|---|---|
| `LISTEN_ADDR` | default `:8081` |
| `DATA_DIR` | default `./data` (holds `app.db` and `covers/`) |
| `ALLOWED_HOSTS` | extra `Host` names, comma separated (see "Security" in section 5); empty by default |
| `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET` | a Twitch developer application; enables lookups |
| `STEAMGRIDDB_API_KEY` | enables SteamGridDB covers |
| `GOMEMLIMIT` | Go soft memory limit (for example `96MiB`). The runtime reads it only from the real environment at start-up, so `loadDotEnv` applies a value found in `.env` itself with `debug.SetMemoryLimit` |

Without keys the app works; lookups answer `configured: false` and the UI offers manual entry.
The version printed at start-up and in the usage text is `main.version` (`dev` by default); the release workflow sets it from the tag with `-ldflags "-X main.version=<tag>"`, and `deploy/third-party-licenses.sh` writes the `THIRD_PARTY_LICENSES.txt` that goes into each release archive.

**Port 8081 may be a real, live server (it is the default port).** Test servers must use another port (for example `LISTEN_ADDR=127.0.0.1:18090`), be addressed as `127.0.0.1:<port>` (never `localhost`) and never receive writes aimed at 8081. See `CLAUDE.md`.

**Other commands:** `deltabacklog` with any other first argument than `backup`, `check` or `sync-igdb` prints usage and exits 2 (a typo never starts the server); `deltabacklog backup <file>` writes a consistent copy of the database; `deltabacklog check <file>` verifies a database or backup read-only (exit code 1 when it is damaged or empty).

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

(`tier` and `tierlist_entry` are part of 001.)

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
play(id, game_id, year NULL, period NULL)      one row per year played, or period 'childhood' / 'always'
saved_view(id, name, icon, filter_json, sort_json, group_by, layout, pinned, pos, ...)
game_fts                                       FTS5 (title, series, notes), kept by db.IndexGame (the UI searches client-side; not used by it yet)
tier(id, name UNIQUE, color, sort)             S, A, B, C, D, F (seeded in 001; fixed, not editable in the UI)
tierlist_entry(year, game_id, tier_id, pos)    PK (year, game_id); one row per ranked game of a year; cascades with the game
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
- **Cover cache key:** `cover_updated_at` changes only when the image changes. `updated_at` changes on every edit and must not be used as a cache key (editing a game would re-download its cover).
- `title_sort` (`db.TitleSort`) lowercases, drops a leading article and punctuation.

## 5. HTTP API

JSON in and out. Errors are `{"error": "..."}` with a status code. Request bodies are size-limited. There is no authentication: the app is for the home network only.

**Security.** `protect` in `cmd/deltabacklog/guard.go` wraps the root handler (tests in `guard_test.go`). In order: (1) the `Host` header must be an IP literal, `localhost`, `deltabacklog.local`, the machine hostname, `<hostname>.local` or an `ALLOWED_HOSTS` name, else 421 (DNS rebinding); `GET`/`HEAD /healthz` is exempt; (2) `http.CrossOriginProtection` rejects cross-origin unsafe requests with 403 (browsers send `Sec-Fetch-Site`/`Origin`; curl and the app itself pass); (3) POST/PUT/PATCH under `/api/` need `Content-Type: application/json`, else 415. Every frontend write must send that header (`send`/`request` in `api.js`, the tier list `PUT` in `yearpage.js`). There is still no authentication; see `SECURITY.md`.

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
| `GET /api/tierlist/{year}` | `{year, tiers:[{id,name,color}], entries:[{game_id,tier_id}]}`, entries ordered by tier then position |
| `PUT /api/tierlist/{year}` | body `{"entries":[{game_id,tier_id}]}` (the `entries` key is required: 400 when missing or null, `[]` clears) replaces the whole list of that year (position = order inside each tier); answers the saved list |
| `GET/POST /api/views`, `PATCH/DELETE /api/views/{id}`, `PUT /api/views/order` | saved views |
| `GET /covers/<file>` | cover images (cached for 30 days; the URL carries `?v=cover_v`) |
| `GET /healthz` | `ok` when the database answers |

**Game (read model):** `id, title, status` (code)`, ownership, series, developer, release_year, vr, favorite, completed, notes, cover` (file name)`, queue_pos, igdb_id, added_at, updated_at, cover_v, platforms[], genres[], themes[], players[], years[]` (calendar years, ascending)`, periods[]`, and from the IGDB enrichment (empty until it ran): `ttb_hastily, ttb_normally, ttb_completely` (minutes or `null`), `modes[]`, `perspectives[]`, `links[]` (`{kind: steam|gog|epic|official, url}`).

**`PATCH /api/games/{id}`** accepts any of: `status, favorite, vr, completed, title, notes, ownership, series, developer, release_year, platforms[], genres[], themes[], players[], years[], periods[], igdb_id`. Absent fields are untouched. Text lookups (`ownership, series, developer`) clear with `""`, numbers (`release_year`, `igdb_id`) with `0`, lists replace the whole set (`years` only the calendar years, `periods` only the periods). Validation: non-empty title (at most 200 characters), `release_year` 1950 to 2100, `years` 1970 to 2100, `periods` of `childhood` or `always`, at most 50 list items, names at most 120 characters. Answers the updated game, `400` for invalid input, `404`, or `409` for an `igdb_id` that another game has.

**`POST /api/games`** takes the same fields plus `cover_url` (and requires `title`; `status` defaults to `backlog`). Answers `201 {"game": {...}, "cover_error": ""}`; a cover that fails to download does **not** fail the creation (the message is in `cover_error`). `409 {"id": <existing>}` when the `igdb_id` already exists.

**Tier lists** (`internal/api/tierlist.go`). Year must be 1970 to 2100 (400 otherwise). A `PUT` is one transaction and is rejected with 400 (nothing changes) when a game has no `play` row for that year, a tier does not exist, a game is listed twice, or there are more than 1000 entries. `GET` returns only entries whose game still has a play in that year, so removing a year from a game hides its entry without a trigger, and a `PUT` only replaces the entries of games that still have a play in that year (hidden entries survive it and return with the year) (the patch of `years` deletes and re-inserts rows, which a trigger would misread). No migration was needed: `tier` and `tierlist_entry` already come from 001. Tier lists do not touch the games cache.

**Safety limits** (enforced server-side, covered by tests)
- A client-supplied cover URL must be on `cdn2.steamgriddb.com`, `cdn.steamgriddb.com` or `images.igdb.com`; redirects are never followed; images larger than 16 megapixels are rejected (computed in `int64` because the Pi is 32-bit).
- A pasted link is only fetched from Steam, GOG, Epic or IGDB, every redirect is re-checked against that list, and the page must answer 200. (The UI no longer advertises links; the resolver remains in `internal/meta`.)
- Request body limits: 64KB for games and views, 256KB for reorder, 4KB for a cover URL.

## 6. Backend packages

- **`internal/db`**: `Open` (pragmas, single connection, migrations), `Migrate`, `GetOrCreate`, `IndexGame` (refresh one FTS row), `TitleSort`, `NormalizeTheme`.
- **`internal/importer`**: `Import(db, csv, options)` splits comma lists (respecting parentheses), parses `Years_played` (`2025,2026`, `Childhood`, `Always`), maps the NocoDB attachment JSON to a cover URL, and returns a report with warnings plus the list of covers to download.
- **`internal/api`**: handlers use a small `pathID`, `writeJSON`/`writeJSONStatus` (content type is set **before** the status) and `writeErr`. `LoadGames` builds the whole read model with one query per link table. `applyFieldEdits` is the single place that writes editable fields (used by patch and create), inside the caller's transaction, and re-indexes the FTS row.
- **`internal/meta`**: `Client` with overridable base URLs (so tests use local fakes). IGDB access tokens are cached until just before they expire. `SearchIGDB` (up to 8 candidates; when IGDB's title search finds nothing it retries with a `where alternative_names.name ~ *"title"*` query, then the part before `:` or `-`, then the title without spaces, which rescues localized titles and spacing differences), `IGDBImages` (artworks and screenshots with their real sizes, then the box cover), `SearchCovers` (SteamGridDB, wide shapes first), `ResolveLink`. A non-200 answer is a `*meta.HTTPError` (the sync reads its `Status` to back off on 429). `TitleScore(a, b)` rates two titles 0 to 100 ignoring case, accents, punctuation, a leading "the" and roman numerals; titles with different numbers ("Resident Evil 2" and "3") are capped at 59, and a title that starts the other ("Dark Souls III" and "... The Ringed City") scores at least 75.
- **`internal/igdblink`**: the IGDB sync (see section 8). Phase 2 is `enrich.go`: `Enrich` reads the linked games from IGDB in batches (`meta.Details`, `meta.TimeToBeat`) and `enrichGame` stores one game inside the batch's transaction. Phase 1: `Classify` sorts one game's IGDB results into *confident* (exactly one candidate with `meta.TitleScore` >= 90 **and** a shared platform), *uncertain* (best score 60 to 89, no shared platform, or several equally good candidates; at most five stored) or *not found*. A candidate is scored against its name and its IGDB alternative names (localized titles), whichever is best. When several candidates are strong, a single one whose name equals the title exactly (`meta.SameName`: case and accents folded, "the" kept) wins. Uncertain and not-found games also keep IGDB's first three results as weak proposals (score under 60 is greyed in the UI), because they cost nothing and save a manual search. `db.FileURI` escapes `%`, `#` and `?` in database paths (they would otherwise be read as URI query or fragment). `Run` is the resumable runner (single goroutine, a 250 ms pause plus the request itself so about 1.4 searches/s, doubling backoff on 429), `Queue`/`Accept`/`NotOnIGDB` are the review queue, `Main` is the `sync-igdb` command (`--enrich` runs phase 2). Only the link is stored: no IGDB scores or ratings.
- **`internal/covers`**: `Fetch` (importer) and `FetchStrict` (client URLs, no redirects), `Save` (decode, size guard, scale to at most 480px wide, write `<id>.jpg` atomically).

## 7. Frontend

Plain ES modules loaded from `index.html`; hash routing (`#/library`, `#/view/<id>`, `#/views`, `#/years[/<year>]`, `#/series`, `#/stats`, `#/link`). The phone's More sheet lists the pages in `MORE_PAGES` order (`more.js`: Series, Stats, Year in review, Views, then Link IGDB); the desktop sidebar sorts them by `SIDEBAR_ORDER` in `main.js` (Year in review, Series, Stats, Views, Link IGDB). No dependencies, no build.

**Modules (`web/static/js/`)**

| Module | Role |
|---|---|
| `main.js`, `more.js` | router, navigation (sidebar on desktop; on phones the bottom bar holds Library, the first two pinned views and **More**, a sheet from `more.js` listing Series, Stats, Year in review and Views; the same links stay in the sidebar and are hidden on phones with CSS), theme, "+" button, service worker |
| `api.js` | the `store` (games, statuses, views), `load()`, edits (`patchGame`, `reorderGames`, `createGame`, `deleteGame`, `setCover`), view calls, lookups |
| `library.js` | the Library and saved-view page: toolbar, filter panel, cards, reorder mode, table mounting |
| `filters.js` | **pure**: filter state, `matches`, include/exclude cycling, (de)serialisation to URL and view JSON, `facetSignature` |
| `sort.js` | **pure**: sort fields, comparator, group-by, and the sort sheet |
| `order.js`, `reorder.js` | slot reuse maths (pure) and the drag list |
| `table.js` | windowed table with inline editing, column picker, CSV export; `csv.js` is the pure CSV writer |
| `drawer.js` | game details, quick changes, edit mode, delete |
| `editpatch.js` | **pure**: form state from a game, validation, and the minimal patch of what changed |
| `form.js`, `picker.js` | shared form controls and the multi-select sheet |
| `add.js` | add sheet, IGDB match picker, cover picker, and `makeSheet`, the shared scrim + sheet. `makeSheet` focuses the sheet on open and returns focus to the opener on close (not to a text field). Re-render with `sheet.render(fn)`, which keeps focus on the control with the same `data-fk` (else the primary footer button, else the sheet); announce with `sheet.say(text)`, one persistent `role=status` region. New sheets should use both instead of `replaceChildren` alone |
| `ttb.js` | **pure**: time to beat: which estimate to use (`ttbMinutes`: usual, else quick, else complete), `isShort` (under 10 h; unknown is never short), `formatTime`, `totalTime` |
| `pick.js` | **pure**: Surprise me rules: pool (Up next, else Backlog; never finished/dropped/not interested/evergreen unless filtered), mood narrowing and chip counts, seedable RNG, pick without repeats, the status "Play it" sets (`playTarget`) |
| `surprise.js` | the Surprise me sheet (mood, result card, skip list in `sessionStorage`); opened from the Library/view header |
| `views.js` | the "Views" management page |
| `yearreview.js` | **pure**: year grouping (`groupOf`), breakdowns, year strip and neighbours, route parsing, tier list state (`tierState`, `place`, `toEntries`) |
| `yearpage.js`, `tierpng.js` | the `#/years/<year>` page (stats, tier list with pointer-event drag, long-press on touch, edge auto-scroll, tap-to-assign menu, debounced autosave) and the canvas PNG export. `savequeue.js` (**pure**, tested) is the autosave: debounce, one request at a time, last state wins, no retry of a 400, a final keepalive request when the page goes away. The page follows `games-changed` so a game that lost the year is never sent |
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

**Service worker (`sw.js`).** Network first for the app shell and the API (the cache is only an offline fallback), cache first for `/covers/*` (their URLs carry the cover version). **Rule: bump `VERSION` whenever any shell file changes (HTML, CSS or a module), and add every new module to `SHELL_FILES`**; a stale `VERSION` leaves old clients on the old cache. Both are easy to forget when adding a page. The server sends `Cache-Control: no-cache` for the shell.

**Style.** `styles.css` uses CSS variables for light and dark (`prefers-color-scheme`, with a `data-theme` override). Breakpoints: below 600px phone layout, from 900px sidebar plus filter panel.

## 8. External services

- **IGDB** (Twitch client credentials, token cached): game details, candidate search, artworks and screenshots. Its genre names are what the library already uses. `platforms.js` maps IGDB platform names onto the library's names and pre-selects the platform the library has the most games on.
- **IGDB sync (phase 1: link every game).** `deltabacklog sync-igdb [--dry-run] [--db path] [--limit n]` (reads the keys from `.env`; the database defaults to `$DATA_DIR/app.db`). It searches IGDB by title for each game with no `igdb_id`, no earlier search and no "not on IGDB" mark, and buckets the answer (section 6). *Confident* matches set `igdb_id` and `igdb_synced_at`; *uncertain* ones are stored in `sync_candidate` for the review screen; every searched game gets `igdb_checked_at`, in the same transaction as its outcome, so a stopped run (Ctrl-C, repeated 429s, no network) simply continues next time. A confident match whose IGDB id another game already has goes to review instead (`igdb_id` is unique). A failed search leaves the game unchecked, so it is tried again. `--dry-run` copies the database with `VACUUM INTO` into a temporary folder (opened read-only, so an unmigrated file is not migrated), runs on the copy, prints a report (counts and examples per bucket) and deletes it: the database file is never written. A real run first writes a full backup, `app-before-igdb-link-<time>.db`, next to the database; **every** run does, including each `--limit` run, so mind the SD card. Pacing: 250 ms between searches *on top of* each request, so in practice about 1.4 requests per second (below IGDB's 4/s limit): about 7 minutes for 600 games. Restart the server afterwards: it caches `/api/games`, and the command runs in another process. Phase 1 stores nothing but the link; phase 2 (next item) reads the details. IGDB scores are never fetched or stored (owner decision).
- **IGDB sync (phase 2: enrich the linked games).** `deltabacklog sync-igdb --enrich [--dry-run] [--refresh] [--db path] [--limit n]`. For every game with an `igdb_id` and no `igdb_enriched_at` it asks IGDB, 20 games per request (`where id = (...)`), for the facts (`/games`: release date, genres, themes, platforms, developer, collection, game modes, player perspectives, website URLs) and for `/game_time_to_beats` (seconds; stored as minutes, rounded). **Fill only empty fields:** release year, developer, genres, themes, series and platform are written only while the game has none (the owner's values are never changed, and a lookup row such as a company or series is created only when a value is actually written, so IGDB's spelling never lands next to the owner's). The platform is filled only when IGDB names exactly one (a game's platform is where the owner has it, not where it was released) *and* it resolves to a platform the library already has through `PlatformKey` (the Go twin of `web/static/js/platforms.js`; the two alias tables must stay in sync, `TestPlatformAliasesMatchTheJS` checks it); the enrichment never creates a platform, and the report counts the ones it left. **Series** (`enrich_lookup.go`): IGDB names a collection after almost every standalone game, so a collection is used only when its folded name (`meta.FoldKey`: case, accents, `&` = `and`, letters and digits) equals a series the library has (the owner's spelling is kept), or when at least two library games share it (a pass after the batches, over every enriched game, so it also completes pairs across runs; IGDB's name is kept in `igdb_series` for that). Franchises are never used (they are not even requested). A `PATCH` that carries `series` (any value, also `""`) sets `igdb_series` to NULL: the owner has decided, and the pass never touches that game again. The report counts the series set on games read in this run ("series") separately from those set on games of earlier runs because a game read now completed their collection (`SeriesCompleted`). The other proposals are only listed in the report. **Plausibility:** a time over 1000 h is stored as NULL, and a game whose quick/usual/complete times contradict each other (quick > usual > complete) gets none. **New data** (time to beat, modes, perspectives, store links) is IGDB's: a later `--refresh` run re-reads the already enriched games and replaces **only** this data (a time IGDB no longer reports is kept); it never fills year, developer, genres, themes, platform or series, because a field the owner emptied on purpose looks like one that was never set. Store links are recognised by host (Steam, GOG, Epic) or IGDB's `official` website type, http(s) only, one per kind. A batch is written in one transaction together with its `igdb_enriched_at` marks, so a stopped run (Ctrl-C, 429s, no network) continues where it stopped; a failed request leaves its games unmarked (5 failures in a row stop the run); a linked game IGDB no longer returns is marked done and counted as missing. Pacing: 250 ms between requests (a batch is two requests, so about 4 requests per second at most), one goroutine, doubling backoff from 2 s on 429, or IGDB's `Retry-After` (up to a minute) when it sends one (6 retries, then the run stops, resumable). `--dry-run` works on a temporary `VACUUM INTO` copy like phase 1 (inside one transaction that is rolled back, so the series pass sees the whole run) and prints counts of what would be filled; a real run first writes `app-before-igdb-enrich-<time>.db` next to the database (every run does). `updated_at` is not touched. Restart the server afterwards (it caches `/api/games`). Nothing about scores is requested; `meta/enrich_test.go` fails if a query mentions rating, hypes, follows or score.
- **SteamGridDB**: `search/autocomplete` for the best (exact-title preferred) match, then `grids/game/{id}` with `dimensions=460x215,920x430`, falling back to any shape.
- **Steam** (`appdetails`, no key): used only by the link resolver.

## 9. Testing

| What | How |
|---|---|
| Backend | `go vet ./... && go test ./...` (in-memory databases, `httptest` fake IGDB/SteamGridDB/Steam/image servers) |
| Pure frontend modules | `node --test web/test/*.test.mjs`: one file per pure module (`filters`, `sort`, `reorder` (also covers `order.js`), `editpatch`, `csv`, `colors`, `ownership`, `platforms`, `years`, `api`, `more`, `pick`, `series`, `stats`, `yearreview`, `savequeue`, `linkqueue`, `ttb`). Pages that draw the DOM (`*page.js`, `seriesview.js`, `surprise.js`, `link.js`) are checked in a browser |
| Backend, by area | `internal/api` (games, views, tier lists, link endpoints), `internal/db` (migrations, backup/check), `internal/meta`, `internal/igdblink`, `internal/importer`, `internal/covers` |
| IGDB sync | `internal/igdblink` tests use an offline fake IGDB (buckets, dry run writes nothing, backup, resume, 429 backoff, review queue, migration 006; `enrich_test.go`: fill-only-empty (also no new lookup rows), series rules, platform mapping and Go/JS alias parity, outlier times, Retry-After, batching, resume after throttling, backoff doubling, refresh, dry run, migration 007 on a populated database); `internal/meta/enrich_test.go` (query shape, no scores, store-link classification); `internal/api/enrich_test.go` (read model and facets); `internal/api/link_test.go` covers the three endpoints |
| Real services | `set -a; . ./.env; set +a; DELTABACKLOG_LIVE=1 go test ./internal/meta -run TestLive -v` |
| In a browser | headless Chromium driven by puppeteer against a **test server with a copy of the data** on a spare port; `docs/tools/screenshots.mjs` is a working example (it opens the main screens and saves the manual's images). By default it builds and serves the demo library from `docs/tools/demo-data` (40 well-known games, generated covers, no personal data), so the images never show anyone's real collection |

Before opening a PR: all of the above pass; changes that touch the UI were looked at in a browser at 390px and 1280px (light and dark where it matters), with no horizontal page scroll. Every bug fix comes with a test that fails without it.

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

`deploy.sh` builds `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0`, uploads to `/tmp/deltabacklog-deploy`, runs `install.sh` with sudo and deletes the upload. `install.sh` is idempotent: it never overwrites an existing database, restarts the service only when the program (or settings) changed, and waits until `/healthz` answers.

**What is on the Pi**

| Path | What |
|---|---|
| `/opt/deltabacklog/deltabacklog` | the program; `backup.sh` next to it |
| `/var/lib/deltabacklog/` | the data: `app.db`, `covers/` (owner `deltabacklog`, mode 750) |
| `/var/backups/deltabacklog/` | nightly backups `app-YYYY-MM-DD.db`, last 14 days |
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
- No authentication (intended: home network only) and no HTTPS (see section 11: no offline cache or app install on the Pi).
- The pasted-link resolver (Steam, GOG, Epic, IGDB pages) exists in the backend but is not offered in the UI; only Steam links were tried against the real site.
