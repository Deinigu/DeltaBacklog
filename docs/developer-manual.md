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
cmd/deltabacklog/   the server (main.go: .env loading, wiring)
cmd/import/         one-off importer from a NocoDB CSV export (also downloads covers)
internal/db/        open, migrate (embedded migrations/*.sql), GetOrCreate, FTS index, TitleSort, NormalizeTheme
internal/importer/  CSV → normalized schema (idempotent)
internal/api/       HTTP handlers: games, games_write, add (create/delete/cover/lookup), facets, views
internal/meta/      IGDB, SteamGridDB and link resolution clients
internal/covers/    cover download (strict: no redirects) + resize + size guard
web/web.go          go:embed of web/static
web/static/         index.html, styles.css, sw.js, manifest, icon, js/*.js
web/test/           Node unit tests for the pure frontend modules (outside the embedded folder)
docs/               these manuals; docs/img screenshots; docs/tools/screenshots.mjs regenerates them
```

## 3. Running it

Go is not on the dev machine's PATH: `export PATH="$PATH:$HOME/sdk/go/bin"` (Git Bash).

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
| `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET` | a Twitch developer application; enables lookups |
| `STEAMGRIDDB_API_KEY` | enables SteamGridDB covers |

Without keys the app works; lookups answer `configured: false` and the UI offers manual entry.

**Port 8081 belongs to the owner's own live server.** Test servers must use another port (for example `LISTEN_ADDR=127.0.0.1:18090`), be addressed as `127.0.0.1:<port>` (never `localhost`) and never receive writes aimed at 8081. See `CLAUDE.md`.

**Other commands:** `deltabacklog backup <file>` writes a consistent copy of the database; `deltabacklog check <file>` verifies a database or backup read-only (exit code 1 when it is damaged or empty).

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

**Tables**

```
status(id, code, label, sort, color)           9 rows; order = display order of shelves
platform, genre, theme, ownership, series, company, player(id, name UNIQUE NOCASE)   lookup values
game(id, title, title_sort, status_id, ownership_id, series_id, developer_id, release_year,
     vr, favorite, completed, notes, cover_url, cover_file, cover_updated_at,
     igdb_id UNIQUE, steamgriddb_id, queue_pos, source_id UNIQUE, added_at, updated_at)
game_platform / game_genre / game_theme / game_player (game_id, x_id)   many-to-many, indexed both ways
play(id, game_id, year NULL, period NULL)      one row per year played, or period 'childhood' / 'always'
saved_view(id, name, icon, filter_json, sort_json, group_by, layout, pinned, pos, ...)
game_fts                                       FTS5 (title, series, notes), kept by db.IndexGame (the UI searches client-side; not used by it yet)
tier, tierlist_entry                           created for tier lists, not used by the UI yet
v_game_flat                                    flattened read view (group_concat of the links)
```

**Rules and conventions**
- Lookup values are created on demand with `db.GetOrCreate(tx, table, name)` (case-insensitive; the table name is checked against an allow-list).
- `status.code` (`playing`, `playing_evergreen`, `up_next`, `backlog`, `hold`, `evergreen`, `finished`, `dropped`, `not_interested`) is what the API and the views use; labels are display only.
- **Manual order:** `game.queue_pos` is one global order shared by all views. Reordering takes the ids a view shows in their new order and gives them **the positions those games already occupy** (`SlotOrder` in `internal/api/games_write.go`, `slotOrder` in `web/static/js/order.js`: keep the two in sync), so the other games never move. A game that changes status, or is created, goes to the end (`max(queue_pos)+1`).
- **Years played** are `play` rows. Moving a game to `playing`, `playing_evergreen` or `finished` inserts the current year (`recordCurrentYear`, ignored if present). An explicit `years` list in the same request is applied first, so it cannot erase that year.
- **Themes** are folded by `db.NormalizeTheme` (`"4X (explore, ...)"` becomes `"4X"`) on **every** write path (importer, create, patch).
- **`igdb_id`** links a game to IGDB (unique). A patch that would give one id to two games answers 409 and applies nothing.
- **Cover cache key:** `cover_updated_at` changes only when the image changes. `updated_at` changes on every edit and must not be used as a cache key (editing a game would re-download its cover).
- `title_sort` (`db.TitleSort`) lowercases, drops a leading article and punctuation.

## 5. HTTP API

JSON in and out. Errors are `{"error": "..."}` with a status code. Request bodies are size-limited. There is no authentication: the app is for the home network only.

| Method and path | Purpose |
|---|---|
| `GET /api/games` | every game with its links (see below) |
| `GET /api/facets` | filter values with counts: `status` (key, label, color, count), `platform`, `genre`, `theme`, `player`, `ownership`, `series`, `year` |
| `POST /api/games` | create a game |
| `PATCH /api/games/{id}` | change any editable field |
| `DELETE /api/games/{id}` | delete (204); links, plays and the cover file go with it |
| `POST /api/games/reorder` | body `[id, id, ...]` in the desired order; answers `{id: position}` |
| `POST /api/games/{id}/cover` | body `{"url": ...}` replaces the cover |
| `GET /api/lookup?q=` | IGDB search: `{configured, query, results:[candidate]}` |
| `GET /api/covers/search?q=&source=&igdb_id=` | cover options, `source` = `steamgriddb` (default) or `igdb` |
| `GET/POST /api/views`, `PATCH/DELETE /api/views/{id}`, `PUT /api/views/order` | saved views |
| `GET /covers/<file>` | cover images (cached for 30 days; the URL carries `?v=cover_v`) |
| `GET /healthz` | `ok` when the database answers |

**Game (read model):** `id, title, status` (code)`, ownership, series, developer, release_year, vr, favorite, completed, notes, cover` (file name)`, queue_pos, igdb_id, added_at, updated_at, cover_v, platforms[], genres[], themes[], players[], years[]` (calendar years, ascending)`, periods[]`.

**`PATCH /api/games/{id}`** accepts any of: `status, favorite, vr, completed, title, notes, ownership, series, developer, release_year, platforms[], genres[], themes[], players[], years[], periods[], igdb_id`. Absent fields are untouched. Text lookups (`ownership, series, developer`) clear with `""`, numbers (`release_year`, `igdb_id`) with `0`, lists replace the whole set (`years` only the calendar years, `periods` only the periods). Validation: non-empty title (at most 200 characters), `release_year` 1950 to 2100, `years` 1970 to 2100, `periods` of `childhood` or `always`, at most 50 list items, names at most 120 characters. Answers the updated game, `400` for invalid input, `404`, or `409` for an `igdb_id` that another game has.

**`POST /api/games`** takes the same fields plus `cover_url` (and requires `title`; `status` defaults to `backlog`). Answers `201 {"game": {...}, "cover_error": ""}`; a cover that fails to download does **not** fail the creation (the message is in `cover_error`). `409 {"id": <existing>}` when the `igdb_id` already exists.

**Safety limits** (enforced server-side, covered by tests)
- A client-supplied cover URL must be on `cdn2.steamgriddb.com`, `cdn.steamgriddb.com` or `images.igdb.com`; redirects are never followed; images larger than 16 megapixels are rejected (computed in `int64` because the Pi is 32-bit).
- A pasted link is only fetched from Steam, GOG, Epic or IGDB, every redirect is re-checked against that list, and the page must answer 200. (The UI no longer advertises links; the resolver remains in `internal/meta`.)
- Request body limits: 64KB for games and views, 256KB for reorder, 4KB for a cover URL.

## 6. Backend packages

- **`internal/db`**: `Open` (pragmas, single connection, migrations), `Migrate`, `GetOrCreate`, `IndexGame` (refresh one FTS row), `TitleSort`, `NormalizeTheme`.
- **`internal/importer`**: `Import(db, csv, options)` splits comma lists (respecting parentheses), parses `Years_played` (`2025,2026`, `Childhood`, `Always`), maps the NocoDB attachment JSON to a cover URL, and returns a report with warnings plus the list of covers to download.
- **`internal/api`**: handlers use a small `pathID`, `writeJSON`/`writeJSONStatus` (content type is set **before** the status) and `writeErr`. `LoadGames` builds the whole read model with one query per link table. `applyFieldEdits` is the single place that writes editable fields (used by patch and create), inside the caller's transaction, and re-indexes the FTS row.
- **`internal/meta`**: `Client` with overridable base URLs (so tests use local fakes). IGDB access tokens are cached until just before they expire. `SearchIGDB` (up to 8 candidates), `IGDBImages` (artworks and screenshots with their real sizes, then the box cover), `SearchCovers` (SteamGridDB, wide shapes first), `ResolveLink`.
- **`internal/covers`**: `Fetch` (importer) and `FetchStrict` (client URLs, no redirects), `Save` (decode, size guard, scale to at most 480px wide, write `<id>.jpg` atomically).

## 7. Frontend

Plain ES modules loaded from `index.html`; hash routing (`#/library`, `#/view/<id>`, `#/views`, `#/stats`). No dependencies, no build.

**Modules (`web/static/js/`)**

| Module | Role |
|---|---|
| `main.js` | router, navigation (bottom bar on phones, sidebar on desktop), theme, "+" button, service worker |
| `api.js` | the `store` (games, statuses, views), `load()`, edits (`patchGame`, `reorderGames`, `createGame`, `deleteGame`, `setCover`), view calls, lookups |
| `library.js` | the Library and saved-view page: toolbar, filter panel, cards, reorder mode, table mounting |
| `filters.js` | **pure**: filter state, `matches`, include/exclude cycling, (de)serialisation to URL and view JSON, `facetSignature` |
| `sort.js` | **pure**: sort fields, comparator, group-by, and the sort sheet |
| `order.js`, `reorder.js` | slot reuse maths (pure) and the drag list |
| `table.js` | windowed table with inline editing, column picker, CSV export; `csv.js` is the pure CSV writer |
| `drawer.js` | game details, quick changes, edit mode, delete |
| `editpatch.js` | **pure**: form state from a game, validation, and the minimal patch of what changed |
| `form.js`, `picker.js` | shared form controls and the multi-select sheet |
| `add.js` | add sheet, IGDB match picker, cover picker |
| `pick.js` | **pure**: Surprise me rules: pool (Up next, else Backlog; never finished/dropped/not interested/evergreen unless filtered), mood narrowing and chip counts, seedable RNG, pick without repeats |
| `surprise.js` | the Surprise me sheet (mood, result card, skip list in `sessionStorage`); opened from the Library/view header |
| `views.js` | the "Views" management page |
| `stats.js`, `statspage.js` | the `#/stats` page: `stats.js` is **pure** (backlog size, finished per year, time in status, top genres, finished vs dropped, chart scale); `statspage.js` draws it (HTML bars, inline SVG chart with a screen-reader table). Computed client-side from `store.games`; no endpoint, no tables |
| `series.js`, `seriesview.js` | **pure** series grouping, progress counts and the "3 of 5" position (definitions of finished / played / left are in the header of `series.js` and in the user manual, section 10b); and the `#/series` page. Client-side only: it groups `store.games` by series name (case-insensitive) and redraws on `games-changed` |
| `platforms.js`, `ownership.js`, `years.js` | pure helpers: IGDB platform mapping, ownership emojis, card year chips |
| `util.js` | `h()` element builder, dialogs, toast, icons |

**State and events.** `store.games` is one array held in memory, mutated in place. Editing is **optimistic**: `patchGame` applies the change locally, then sends it; edits to one game are queued so a slow answer can never overwrite a newer edit; a failure restores the game from the server. Events on `window`:
- `games-changed` (`detail.id` = the game; `detail.reorder` when only positions moved): views rebuild only that card and refresh counts. If a new filter value appeared (a new genre, platform, year...) the library asks for a full rebuild.
- `games-reloaded`: a game was added or deleted; the router re-renders the page.
- `views-changed`: saved views changed; the navigation is rebuilt.

**Formats**
- Saved view `filter`: `{"q": "", "in": {"genre": ["RPG", "Indie"]}, "ex": {"status": ["finished"]}, "all": ["genre"], "flags": ["favorite"]}`. Facet keys: `status, platform, genre, theme, year, ownership, player`; flags: `favorite, vr, completed`.
- `sort`: `[{"field": "status", "dir": "asc"}, ...]` (fields in `SORT_FIELDS`); `group_by`: a key of `GROUP_FIELDS` or `""`; `layout`: `cards` or `table`.
- The Library (not a saved view) keeps the same state in the URL query, e.g. `#/library?genre=RPG&ex_status=finished&all=genre&sort=title:asc&layout=table`.

**Table.** Rows have a fixed height; only the visible ones exist in the DOM (`paint`). Each row remembers a signature of what it shows, so a refresh rebuilds only rows whose data changed (a click that started on an unchanged row still completes). The row being edited is never destroyed by a refresh. Column widths and visibility are kept in `localStorage` (a per-device convenience).

**Service worker (`sw.js`).** Network first for the app shell and the API (the cache is only an offline fallback), cache first for `/covers/*` (their URLs carry the cover version). Bump `VERSION` and add new files to `SHELL_FILES` when you add a module. The server sends `Cache-Control: no-cache` for the shell.

**Style.** `styles.css` uses CSS variables for light and dark (`prefers-color-scheme`, with a `data-theme` override). Breakpoints: below 600px phone layout, from 900px sidebar plus filter panel.

## 8. External services

- **IGDB** (Twitch client credentials, token cached): game details, candidate search, artworks and screenshots. Its genre names are what the library already uses. `platforms.js` maps IGDB platform names onto the library's names and pre-selects the platform the library has the most games on.
- **SteamGridDB**: `search/autocomplete` for the best (exact-title preferred) match, then `grids/game/{id}` with `dimensions=460x215,920x430`, falling back to any shape.
- **Steam** (`appdetails`, no key): used only by the link resolver.

## 9. Testing

| What | How |
|---|---|
| Backend | `go vet ./... && go test ./...` (in-memory databases, `httptest` fake IGDB/SteamGridDB/Steam/image servers) |
| Pure frontend modules | `node --test web/test/*.test.mjs` |
| Real services | `set -a; . ./.env; set +a; DELTABACKLOG_LIVE=1 go test ./internal/meta -run TestLive -v` |
| In a browser | headless Chromium driven by puppeteer against a **test server with a copy of the data** on a spare port; `docs/tools/screenshots.mjs` is a working example (it opens the main screens and saves the manual's images) |

Before opening a PR: all of the above pass; changes that touch the UI were looked at in a browser at 390px and 1280px (light and dark where it matters), with no horizontal page scroll. Every bug fix comes with a test that fails without it.

## 10. Workflow

- Branches: `main` plus `features/<name>`; changes reach `main` through pull requests with merge commits, never by committing to `main`.
- **Do not stack a PR on another unmerged PR for long.** A PR merged into its base branch is not in `main` (this has happened); merge each PR into `main` and branch the next one from `main`.
- Commit messages: a short summary line, a body saying what and why, and the `Co-Authored-By` line given by the session.
- **Secrets** (IGDB and SteamGridDB keys) live only in `.env`. `git grep` for keys before pushing.
- A change in behaviour updates the matching section of the **user manual** and, if it touches structure, the API or the data model, the **developer manual**, in the same PR.

## 11. Build and deploy

The program is **cross-compiled on the PC** and uploaded; the Pi never holds the source and never builds anything (compiling the SQLite library needs most of a 1GB Pi and minutes of CPU, which would starve the other services). It runs directly under **systemd**, with no container, to keep the memory use at about 15MB.

**Deploy or update (from the PC, Go on PATH, `ssh raspberry` working):**

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
| `/etc/deltabacklog.env` | settings and the IGDB / SteamGridDB keys (root only, mode 600) |
| `/etc/systemd/system/deltabacklog.service`, `deltabacklog-backup.service`, `deltabacklog-backup.timer` | the units (sources in `deploy/`) |

**Starting at boot.** `deltabacklog.service` is enabled (`WantedBy=multi-user.target`) with `Restart=always` (a crashed process is back in about 4 seconds, tested with `kill -9`). It runs as the unprivileged system user `deltabacklog` with `NoNewPrivileges`, a read-only file system except its data folder, and `Nice=5`. The Pi's kernel does not enforce systemd memory limits (the memory cgroup is off on Raspbian), so the real cap is `GOMEMLIMIT=96MiB` in the settings file.

**Backups.** `deltabacklog-backup.timer` runs `backup.sh` every night at 03:30 (with `Persistent=true`, so a missed run happens at the next boot). It calls `deltabacklog backup <file>`, which uses SQLite's `VACUUM INTO` for a consistent copy while the app runs, never overwrites an existing file and writes under a temporary name. Covers are not backed up (they can be downloaded again).

```sh
ssh raspberry
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

**HTTP only, so no service worker on the Pi.** Browsers only allow service workers and "Install app" on HTTPS or `localhost`. Served as `http://192.168.1.50:8081` the app works fully, but the offline cache is inactive, and "Add to Home screen" creates a plain shortcut. Real installation needs HTTPS (for example a reverse proxy with a certificate); not set up.

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

- Games imported from NocoDB have no `igdb_id` until they are matched with **Fill from IGDB**; duplicate detection falls back to the title.
- Not built yet: Year in review and tier lists (the tier tables exist), a JSON export.
- No authentication (intended: home network only) and no HTTPS (see section 11: no offline cache or app install on the Pi).
- The pasted-link resolver (Steam, GOG, Epic, IGDB pages) exists in the backend but is not offered in the UI; only Steam links were tried against the real site.
