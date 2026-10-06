# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

DeltaBacklog is a self-hosted videogame tracker: one Go binary (SQLite via `modernc.org/sqlite`, no CGO) with a vanilla-JS PWA embedded via `go:embed`. It replaces the user's NocoDB videogame table and must run on a Raspberry Pi 3B+ (armv7, ~900MB RAM shared with other containers), so keep it light: no frontend build step, no heavy dependencies.

## Commands
- Go is not on PATH on the dev machine: `export PATH="$PATH:$HOME/sdk/go/bin"` (Git Bash) or `$env:Path += ";$env:USERPROFILE\sdk\go\bin"` (PowerShell).
- `go vet ./... && go test ./...` for the backend; `node --test web/test/*.test.mjs` for frontend logic (pure modules only: `filters.js`, `sort.js`, `order.js`). Tests live in `web/test/`, outside the embedded `web/static/`.
- `go run ./cmd/deltabacklog` serves on `:8081` with data in `./data`. `web/static` is compiled into the binary, so restart after any frontend edit.
- Import from NocoDB: see README ("Import from NocoDB"); `cmd/import` is idempotent.
- Cross-build for the Pi: `GOOS=linux GOARCH=arm GOARM=7`.

## Data model decisions
- Migrations are numbered `.sql` files in `internal/db/migrations/` (tracked with `PRAGMA user_version`); add a new file, never edit an applied one. Tables are `STRICT`.
- No ratings, by user choice. Do not reintroduce them.
- `game.queue_pos` is one global manual order shared by all views. Reorder by reusing the slots the moved games already occupy (`SlotOrder` in Go, `slotOrder` in `web/static/js/order.js`: keep the two in sync).
- "Years played" is rows in `play` (a calendar year, or period `childhood`/`always`), not text.
- Saved views store `filter_json` / `sort_json`; their shape is owned by `web/static/js/filters.js`.

## Workflow
- Branches: `main` plus `features/<name>`, merged by pull request with merge commits; never commit straight to `main`. PRs are stacked on the previous feature branch; the user merges them (do not run `gh pr merge`).
- Secrets: IGDB/SteamGridDB keys go in `.env` (gitignored), never in the repo. Run `git grep` for keys before pushing.
- UI is mobile-first (phone is the main client): check layouts at 390px and 1280px, tap targets at least 44px, no horizontal scroll.
- After UI changes the browser may show a stale app (service worker): hard refresh or use 127.0.0.1 instead of localhost.
