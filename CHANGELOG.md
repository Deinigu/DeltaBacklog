# Changelog

All notable changes to DeltaBacklog are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once it has a first release. Dates are when the pull request was merged.

## [Unreleased]

### Usability

Fixes to how the app works and fits on a phone; the look is unchanged.

- The **+** button is shown only on the Library, saved views and shelves (the `n` key still adds a game anywhere); on a PC its place is kept clear of the last row.
- Library and views on a phone: the title has its own row (no more "Playing n…"), the buttons under it are labelled (*Surprise me*, *Table*, *Reorder*, *Views*), and only the search row stays pinned while scrolling.
- The reorder handle and its arrows are 44px wide on phones; the strip of active filters and the Years strip fade at the edge to show they scroll, and the current year is centred.
- Ranking board: no empty gap under the back link. Rankings list: unusable arrows are hidden (not greyed), **Delete** is red and apart from Rename and Duplicate.
- Series: the filters and text above the tiles take fewer rows (*Left to play*, *Not in library*, *Refresh*); a series with many games you do not have lists the first six, then **Show all**.
- Upcoming: **Add** and **Hide** sit under the text on phones, so the rows line up (as on Similar). Similar no longer says "Press Refresh" twice.
- Stats headline: the caption sits under the number instead of wrapping after it. Years: a zero tile is quiet instead of as loud as a real number.
- Empty states are one short sentence with their button (Library: *Clear filters*; Similar and Upcoming: the IGDB refresh; Series, Views and Screenshots: *Open the Library*).
- Dates on the Series page use the English format like the other pages.

### Added

- **Deals**: a **Deals** page (`#/deals`, More menu on a phone) with the best price right now and the historical low of the games on your Wishlist, in your Backlog and Up next, from IsThereAnyDeal (ITAD). Games on sale come first (biggest cut), then games that are not on sale but sit at or near their low; a **Historical low** or **Near low (within 10%)** badge (only when the low was a real discount or the game is on sale now), the shop, the regular price, the cut, the end date when ITAD gives one, and a **View deal** link to the shop (a new tab). Filter chips for Wishlist / Backlog / Up next, three sorts (biggest discount, closest to the low, lowest price), a *Last checked* note and a country setting (default `ES`, so EUR). A game's details show the same price line, and **Wrong game?** / **Pick the game** to fix a match by searching ITAD by title (or say there is none). Games are matched by their Steam link's app id when they have one, else by an exact title; a game with no exact match is listed apart rather than guessed. Prices are read only by **Refresh prices** (a background run with progress, one at a time, 50 games per request, stops with what it stored on a 429), never on a page load. Needs `ITAD_API_KEY` in `.env` (on the Pi: `/etc/deltabacklog.env`); without it the page says it is not set up. Migration 016: `setting`, `deal_match`, `deal_price`. New endpoints `GET /api/deals`, `POST /api/deals/refresh`, `GET /api/deals/search`, `PUT /api/deals/country`, `PUT /api/games/{id}/deal-match`.
- **Similar games**: a **Similar games** page (`#/similar`, More menu on a phone) with the games IGDB lists as similar (`similar_games`) to the games you finished or marked as favourites and that you do not have, ranked by how many of your games suggest them (then by how recently you played the newest one), each with its poster, year, *Because you played X, Y and Z*, **Add** (the add form pre-filled), **Wishlist** (the same form on the Wishlist status) and **Hide** (with Undo; a **Hidden** list brings them back). A family member you own (a remaster or port of a game you have) is left out, a remake or a standalone expansion is not; games already on your wishlist are listed apart. A finished or favourite game's details show a small *Similar games* row from the same cache. The list is cached in the database (migration 015: `similar_game`, `similar_link`, `similar_fetch`, `similar_hidden`) and read from IGDB only by **Refresh from IGDB** on the page (a background run with progress, one at a time, stops with what it stored when IGDB keeps answering 429) and by `sync-igdb --enrich`, never on a page load. The posters are the same `i<id>.jpg` files the Series page keeps; each cache's cleanup leaves the files the other names. The Hide list is its own (hiding a suggestion does not hide the game on the Series page).- **Wishlist and Upcoming releases**: a **Wishlist** status (after Backlog; filters, saved views, table, details and add form all know it) and an **Upcoming** page (`#/upcoming`, More menu on a phone) with the games that are not out yet, grouped by month (This month, Next month, ..., then Later / TBA), from the Wishlist and from the series' IGDB collections (games you do not have, minus the Hide list), with poster, date, platforms and series, **Add** (pre-filled, status Wishlist) and **Hide**, and a **Recently released** section (last 60 days). IGDB's first release date is stored with its precision (day, month, quarter or year; migration 014: `game.first_release_date`, `release_date_format`, and the same plus `platforms` on `igdb_collection_game`), so "2027" is never shown as a day. Filled on add and relink, by `sync-igdb --enrich` and by **Refresh from IGDB** (never on a page load). A Wishlist game does not count as a library game on the Series page (it is greyed on its series' shelf with an **On your wishlist** mark, and is left out of the series counts and of the Stats). The date and format are in `GET /api/games` (`release_date`, `release_format`) and in the games of `GET /api/series/missing` (`date`, `format`, `platforms`).
- **A visual Series page**: a grid of series tiles (a fan of the series' posters, progress bar, *2/4 finished*); tapping a tile opens the series on its own route (`#/series/<name>`, so the phone's back button returns to the tiles) as a poster shelf in release order, with ✓ / ▶ marks for finished and played games.
- **Games you do not have yet, on the Series page**: the games of the IGDB collection behind each series that are not in the library appear in release order as greyed "Not in your library" posters with **Add** (opens the add form pre-filled from IGDB, with your own series name) and **Hide** (with Undo and a **Hidden (n)** list to show them again). Only real games (main games, standalone expansions, remakes, remasters). The collection of a series comes from its linked games (the one most of them share, preferring the one named like the series); a game counts as owned with its other IGDB entries of the same game (original, expanded edition, remaster, port; not its sequels, remakes or standalone expansions), so those are not listed; the lists and their small posters are cached on the server (migration 013: `game_collection`, `game.igdb_parent_id`, `igdb_collection`, `igdb_collection_game`, `series_hidden`) and read only by **Refresh from IGDB** on the page or `sync-igdb --enrich`, never on a page load. New endpoints `GET /api/series/missing`, `POST /api/series/refresh`, `/api/series/hide`, `/api/series/unhide` and `GET /api/igdb/{id}`. A **Not in your library** filter shows only series with something to add.
- **`PUBLIC_NAME=off`** in `/etc/deltabacklog.env`: `install.sh` then disables and stops `deltabacklog-name.service` (no `deltabacklog.local`, no port 80 rules) instead of enabling it, for an app reached only through a reverse proxy (with `LISTEN_ADDR=127.0.0.1:8081`). A `deploy.sh --env` now keeps `ALLOWED_HOSTS` and `PUBLIC_NAME` (and, with `PUBLIC_NAME=off`, `LISTEN_ADDR`) from the installed settings, as it kept `SCREENSHOTS_DIR`. Without the setting nothing changes.
- **Works under a sub-path behind a reverse proxy** (for example `http://deltaraspberry.local/backlog/`, with the proxy stripping `/backlog`), as well as at the root as before, with no setting: every URL the app uses (API, covers, screenshots, the service worker and its caches, the web app manifest) is now relative to the page. Screenshot `src`/`thumb` in the API are relative paths (`screenshots/<id>/<file>`). The proxy's host name goes in `ALLOWED_HOSTS`.
- **Filter and sort by time to beat**: a **Time to beat** block in the filters (quick ranges, your own From/Under in hours, "No time known" to show or hide games without a time, and which estimate to use), in the Library, saved views and the rankings' game picker; a **Time to beat** sort and table column. It replaces the *Short game (under 10 h)* switch: saved views and links that used it now read *Time to beat under 10 h*.
- **Choose the poster** (the portrait picture of the rankings and the tier list PNG): **Change poster** in a game's details, and in the bar of a ranking board when a game is selected. The sheet offers the IGDB cover and several SteamGridDB portrait grids side by side, a pasted image link (same allowed hosts as covers), **Crop of the wide cover** and **Fetch automatically again**. If SteamGridDB cannot be reached (it is sometimes blocked) the IGDB options still appear at once and the sheet says SteamGridDB can't be reached right now. A poster you chose is never replaced by the background queue or `fetch-posters --upgrade`, and a new wide cover does not touch it (migration 011 adds `game.poster_user`). New endpoint `POST /api/games/{id}/poster`; `GET /api/covers/search` takes `shape=poster`.
- **Time to beat from HowLongToBeat, editable, only in the details**: the game details have a new **Time to beat** block with three tiles (Main story, Main + extras, Completionist), a source note (*from IGDB*, *from HowLongToBeat* or *set by you*), **Edit times** (hours, `7h30` or `45m`) and **Get from HowLongToBeat** (search by title and year, one tap fills and saves; a clear message when the site cannot be reached or has changed). The ⏱ time is gone from the Library and Surprise me cards. Times you set or took from HowLongToBeat are never overwritten by the IGDB enrichment or cleared by relinking IGDB. New `GET /api/hltb/search`, `ttb_hastily/normally/completely` (and `ttb_source`) accepted by `PATCH`/`POST /api/games`, `game.time_to_beat_src` (migration 012).
- **Add a game with every field**: the add form now also has years played, played with, favorite, VR, 100% completed and notes (the less common ones under "More details"). The server's `POST /api/games` used to ignore favorite, VR and 100% on create; it now stores them.
- The years-played picker offers the years from the earliest year in your library to this year (the last 10 while the library has none) instead of going back to 1990; older years can still be typed (a year outside 1970-2100 now says so). The add form asks before discarding details you typed.
- **Screenshots page** (More menu on a phone, sidebar on a PC): every screenshot of every game in one gallery, grouped by game with its cover or newest first, with search by game or caption, a year filter, lazy thumbnails and "Show more" paging. The full-screen viewer goes through all matching pictures across games and can open the game. New endpoint `GET /api/screenshots`.
- **Rankings** replace the per-year tier list: TierMaker-style custom rankings with as many tiers as you want, your own long labels and colours, a dark board of big portrait posters, drag and drop (touch: long press), tap-to-select with buttons, keyboard moves, S/M/L tile size, and PNG export. Add any set of games from the whole library with search and filters (for example everything played in 2025). Year in review has a "Rank this year" button instead of the embedded tier list. Your old tier lists became rankings called "Tier list <year>" (migration 010; the old tables stay unused).
- **Posters**: portrait box art for the rankings, made once per game in the background (IGDB cover, else a SteamGridDB 600x900 grid, else a crop of the wide cover) and served like covers. `deltabacklog fetch-posters [--dry-run] [--limit N] [--upgrade]` makes them all at once.
- **Screenshots** for every game: upload several pictures from a phone or computer in the game's details (drag and drop on desktop), thumbnail grid, full-screen viewer with swipe, captions, reordering, download of the original and delete with undo. Originals are stored untouched (JPEG, PNG, WebP, content-checked) next to a generated thumbnail; migration 009.
- `SCREENSHOTS_DIR` (and `SCREENSHOT_MAX_MB`, `SCREENSHOT_REQUEST_MAX_MB`): screenshots can live on an external drive. The app never creates that folder, so an unmounted drive cannot fill the SD card; uploads then say "screenshot storage not available".
- Nightly backup also copies new screenshots incrementally (`deltabacklog backup-screenshots`), only while the backup disk keeps `SHOTS_MIN_FREE_GB` free, and saves the database copy on the external drive too. `install.sh` writes the systemd drop-ins for the drive.

### Fixed
- IGDB search no longer misses well-known games buried among their sequels, bundles and DLC (God of War 2005 and Portal 2007 were not offered). It now fetches 50 hits plus all (up to 200) games named exactly like the query, shows the best 12 with exact names first (DLC and mods last; then the game on most stores; then main game before remake and port; then older; at most 6 namesakes so sequels still show), and understands a year in the query (`God of War 2005`, `Portal (2007)`) as a hint. `sync-igdb` uses the same search and sees every namesake, so it never links on a shortened list. All IGDB requests of one process are paced to 4 per second in one place.
- The ranking editor no longer rebuilds the whole board every few seconds while posters are being made (it flashed, and could swallow a tap on a tier name, so renaming or deleting a tier seemed not to work): new posters are swapped in tile by tile, and a redraw is held back while a tier sheet is open. The rankings list shows Rename, Duplicate and Delete as words.
- Leaving the library and then resizing the window across 600px (or rotating a phone) no longer rewrites the address to `#/library` while another page is shown: the library now removes its screen-width listener when it closes.
- Changing a game's cover now updates the open game view at once instead of showing the old picture until it is closed and reopened. The cover version has millisecond resolution (two changes in one second can no longer share a cache key), and the service worker drops older cached versions of a cover.
- Screenshots (review fixes): decoding is limited by a memory budget (`SCREENSHOTS_DECODE_BUDGET_MB`, default 48; the thumbnail scaler no longer allocates a large scratch buffer) instead of a pixel count, so a large photo can no longer exhaust the Pi's memory; over the budget the original is kept untouched with a thumbnail made by the browser. Thumbnails always fit in 480x960 (very tall or wide pictures are cropped). An upload request is limited to 50 files and 200 parts, and a request cut short by the size limit answers 200 with per-file results. The backup's free-space check now runs per file inside `backup-screenshots --min-free-gb`. Deleting a screenshot while the drive is missing is refused instead of orphaning its files, a start-up sweep removes only temporary files and orphan thumbnails, re-adds pictures a restored database no longer lists and moves pictures of unknown games, or older than their game (ids are reused), to `.orphans/` (it never deletes an original), and listing no longer writes to the drive. The install adds `After=<mount unit>` to the drop-ins, restarts the service when they change and refuses a `SCREENSHOTS_DIR` that is a mount point. The undo of a delete survives closing the drawer or the page, and the viewer keeps keyboard focus inside it and gives it back to the thumbnail.
- `deltabacklog backup` no longer migrates the database before copying it, and fails instead of creating and backing up an empty database when `DATA_DIR` is wrong.
- The Raspberry Pi install writes `app-before-upgrade-<time>.db` before a new version replaces the program (a new version may migrate the database when it starts).
- Every API error is JSON (`{"error": ...}`), as documented; `/covers/` no longer lists the cover folder.
- The game detail view takes keyboard focus when it opens and gives it back when it closes.
- With the Theme button set against the system theme, scrollbars and form controls now follow the app's theme.
- Quick add maps the IGDB platform names `NSW` and `PS` the way the IGDB sync does.

### Changed
- The repository is now a public showcase, not a community project: README status note and "How this was built" section, CONTRIBUTING explains that contributions are not accepted, and the code of conduct, issue forms and pull request template were removed. Dependabot runs monthly with grouped updates.
- Deploys are stamped with a version from `git describe`; idle connections are closed after 2 minutes; CI fails on unformatted Go code.

## 2026-10-09 (pre-release)

### Security
- Cross-origin POST/PUT/PATCH/DELETE requests are rejected (403) with `http.CrossOriginProtection`; cross-site and same-site `GET /api/*` are rejected too, so other sites cannot trigger lookups.
- JSON write routes under `/api/` require `Content-Type: application/json` (415 otherwise).
- Host allow-list against DNS rebinding (421); extra names via the new `ALLOWED_HOSTS` variable. `/healthz` answers for any host. Error bodies from the guard are JSON (PR #29).

### Added
- **IGDB enrichment**: `deltabacklog sync-igdb --enrich` fills empty fields of linked games and adds time to beat, game modes, perspectives and store links, shown on cards and in the detail view, with a Quick game filter (PR #27).
- Open-source readiness: MIT license, contributing guide, code of conduct, security policy, issue and pull request templates, CI and release workflows, Dependabot configuration (PR #28).
- The friendly address `http://deltabacklog.local` on the Raspberry Pi (mDNS name plus port 80 forwarding), installed by `deploy/install.sh`; manuals, README and screenshots brought up to date (PR #26).
- A demo library (`docs/tools/demo-data`, 40 well-known games, generated placeholder covers or real ones downloaded when keys are set) and a screenshot tool that uses it by default, so the documentation shows no personal data.
- `deltabacklog` knows its version: it is printed in the startup log and in the usage text (`dev` for source builds, the tag for release binaries).
- Release archives include `THIRD_PARTY_LICENSES.txt` with the notices of the Go modules and standard library linked into the binary.
- Rewritten README with quick start, configuration table, backup and restore, FAQ and roadmap.

### Changed
- `GOMEMLIMIT` set in `.env` is now applied (the Go runtime only reads it from the real environment at start-up).
- `cmd/import` no longer defaults to a private NocoDB address; `-nocodb` is empty unless given.
- The documentation screenshots come from the demo library, with real cover art from IGDB and SteamGridDB (illustration only; no cover files are committed).

## 2026-10-08 (pre-release)

### Added
- **Link with IGDB**: `deltabacklog sync-igdb [--dry-run]` links every game to its IGDB entry; uncertain matches wait in a review screen (PR #25).
- **Year in review and tier lists**: what you played in a year, split by outcome, platform and genre, and an S-to-F tier list exportable as a picture (PR #23).
- **Stats** page: backlog by platform, finished per year, waiting time, genres, finished against dropped (PR #22).
- **Series progress**: a progress bar per series, its games in release order, filter for series with games left (PR #21).
- **Surprise me**: a random pick from the current view, with mood filters (PR #20).
- Raspberry Pi packaging: cross-compiling `deploy.sh`, systemd service, nightly backups with `backup` and `check` commands, review fixes and a UX pass (PR #19).

### Fixed
- Eight findings from the Surprise me review (PR #24).

## 2026-10-07 (pre-release)

### Added
- Change a game's cover from SteamGridDB or IGDB artwork (PR #13).
- Cards show years played as a timeline, platform tags and a status accent (PR #12).
- Editing every field on a phone, quick add with IGDB and SteamGridDB lookups, ownership emojis, a table layout with inline editing and CSV export (PRs #7 to #11).
- Manual drag-to-reorder and quick status changes (PR #6).

## 2026-10-06 (pre-release)

### Added
- Normalized SQLite schema, the importer for the author's NocoDB export and the read API (PR #1).
- Mobile-first library view with instant search and faceted filters (PRs #2 and #3).
- Multi-level sorting and grouping (PR #4); saved views with include/exclude filters and any/all (PR #5).

### Removed
- Ratings, and the "number in the series": removed on purpose and not coming back (PR #3).

[Unreleased]: https://github.com/Deinigu/DeltaBacklog/commits/main
