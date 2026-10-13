# Changelog

All notable changes to DeltaBacklog are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once it has a first release. Dates are when the pull request was merged.

## [Unreleased]

### Added
- **Screenshots** for every game: upload several pictures from a phone or computer in the game's details (drag and drop on desktop), thumbnail grid, full-screen viewer with swipe, captions, reordering, download of the original and delete with undo. Originals are stored untouched (JPEG, PNG, WebP, content-checked) next to a generated thumbnail; migration 009.
- `SCREENSHOTS_DIR` (and `SCREENSHOT_MAX_MB`, `SCREENSHOT_REQUEST_MAX_MB`): screenshots can live on an external drive. The app never creates that folder, so an unmounted drive cannot fill the SD card; uploads then say "screenshot storage not available".
- Nightly backup also copies new screenshots incrementally (`deltabacklog backup-screenshots`), only while the backup disk keeps `SHOTS_MIN_FREE_GB` free, and saves the database copy on the external drive too. `install.sh` writes the systemd drop-ins for the drive.

### Fixed
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
