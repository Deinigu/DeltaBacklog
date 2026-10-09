# Changelog

All notable changes to DeltaBacklog are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once it has a first release.

## [Unreleased]

### Added
- Open-source readiness: MIT license, contributing guide, code of conduct, security policy, issue and pull request templates, CI and release workflows, Dependabot configuration.
- A demo library (`docs/tools/demo-data`, 40 well-known games with generated covers) and a screenshot tool that uses it by default, so the documentation shows no personal data.
- The friendly address `http://deltabacklog.local` on the Raspberry Pi (mDNS name plus port 80 forwarding), installed by `deploy/install.sh`.
- Rewritten README with quick start, configuration table, backup and restore, FAQ and roadmap; user and developer manuals brought up to date.

### Changed
- `cmd/import` no longer defaults to a private NocoDB address; `-nocodb` is empty unless given.

## 2026-10-09 (pre-release)

### Added
- **Link with IGDB**: `deltabacklog sync-igdb [--dry-run]` links every game to its IGDB entry; uncertain matches wait in a review screen (PR #25).
- Surprise me: eight review fixes (PR #24).

## 2026-10-08 (pre-release)

### Added
- **Year in review and tier lists**: what you played in a year, split by outcome, platform and genre, and an S-to-F tier list exportable as a picture (PR #23).
- **Stats** page: backlog by platform, finished per year, waiting time, genres, finished against dropped (PR #22).
- **Series progress**: a progress bar per series, its games in release order, filter for series with games left (PR #21).
- **Surprise me**: a random pick from the current view, with mood filters (PR #20).
- Raspberry Pi packaging: cross-compiling `deploy.sh`, systemd service, nightly backups with `backup` and `check` commands (PR #19).

## 2026-10-07 (pre-release)

### Added
- Change a game's cover from SteamGridDB or IGDB artwork (PR #13).
- Cards show years played as a timeline, platform tags and a status accent (PR #12).
- Editing every field on a phone, quick add with IGDB and SteamGridDB lookups, ownership emojis, sorting with several levels, saved views, shelves and a table layout with inline editing and CSV export (PR #11).

## 2026-10-06 (pre-release)

### Added
- Normalized SQLite schema, the importer for the author's NocoDB export and the read API (PR #1).
- Mobile-first library view (PR #3).

### Removed
- Ratings, and the "number in the series": removed on purpose and not coming back (PR #3).

[Unreleased]: https://github.com/Deinigu/DeltaBacklog/commits/main
