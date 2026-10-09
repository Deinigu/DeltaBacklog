# Contributing to DeltaBacklog

Thanks for taking a look. DeltaBacklog is a small project with one maintainer, so a few ground rules keep it pleasant for everyone. Please also read the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- **Bugs and small fixes**: just open a pull request (an issue first is welcome, not required).
- **New features**: open an issue first and describe the problem you want to solve. This avoids work on something that does not fit.
- **Questions and ideas**: an issue is fine.
- **Security problems**: do not open a public issue; see [SECURITY.md](SECURITY.md).

## What fits (and what does not)

The project is deliberately small. It must keep running on a **Raspberry Pi 3B+** (armv7, about 900 MB RAM shared with other services), and that shapes the rules:

- **No frontend build step.** The app is vanilla JavaScript (ES modules) in `web/static`, embedded in the binary. No bundler, no framework, no npm dependencies in the app.
- **No heavy dependencies.** One static Go binary, pure-Go SQLite (`modernc.org/sqlite`, **no CGO**). Think twice before adding a module; say why in the PR if you must.
- **Light on memory and CPU.** Avoid anything that needs a lot of RAM or startup work.
- **Mobile first.** Design for a 390 px phone, then scale up: tap targets of at least 44 px, no horizontal scroll, no hover-only actions.
- **No ratings and no "number in the series", by design.** Please do not send changes that add them back (migrations 002 and 005 removed them on purpose).
- **Single user, trusted network.** Authentication and multi-user support are out of scope for the core app.

## Development setup

You need **Go 1.27+** and **Node 20+** (Node is only for the frontend unit tests and the screenshot tool). Nothing else, and no CGO toolchain.

```sh
git clone https://github.com/<you>/DeltaBacklog.git      # your fork
cd DeltaBacklog
go run ./cmd/deltabacklog                                # http://localhost:8081, data in ./data
```

The web files are compiled into the binary: **restart the server after editing anything in `web/static`**. If the browser still shows the old page (the service worker caches it), hard-refresh or open `http://127.0.0.1:<port>` instead of `localhost`.

To work with a realistic library without your own data, build the demo one (40 well-known games with generated placeholder covers; add `-env <your .env>` to download real ones):

```sh
go run ./docs/tools/demo-data -data /tmp/demo
LISTEN_ADDR=127.0.0.1:18090 DATA_DIR=/tmp/demo go run ./cmd/deltabacklog
```

Lookups (IGDB, SteamGridDB) need free API keys in a git-ignored `.env`; see the [README](README.md#configuration). You can work on everything else without them.

**Ports:** 8081 is the default and may be somebody's real server. Use another port for tests and address it as `127.0.0.1:<port>`.

## Tests

Run these before opening a PR; CI runs the same:

```sh
go vet ./... && go test ./...          # backend
node --test web/test/*.test.mjs        # frontend logic (the pure modules)
```

- Every bug fix comes with a test that fails without it.
- Backend tests use in-memory databases and fake IGDB / SteamGridDB servers (`httptest`); they never touch the network.
- Frontend tests live in `web/test/` (outside the embedded folder) and cover the pure modules. Pages that draw the DOM are checked by hand in a browser: for UI changes, look at **390 px and 1280 px**, light and dark.
- Database changes are **new numbered files** in `internal/db/migrations/`. Never edit a migration that was already released.
- `SlotOrder` (Go) and `slotOrder` (`web/static/js/order.js`) implement the same queue reordering and must change together.

The [developer manual](docs/developer-manual.md) describes the architecture, data model, API and frontend modules.

## Pull requests

1. Fork the repository and create a branch from `main` (for example `fix/card-overflow` or `feature/csv-import`).
2. Keep the change focused: one fix or feature per PR, no unrelated reformatting.
3. **Update the docs in the same PR**: behaviour changes go in `docs/user-manual.md`; API, data model or structure changes go in `docs/developer-manual.md`. Add a line under *Unreleased* in [CHANGELOG.md](CHANGELOG.md).
4. Write a short commit message: a summary line, then what and why.
5. Open the PR against `main` and fill in the template. Say how you tested it, with a screenshot for UI changes. Screenshots must not show private data: `docs/tools/screenshots.mjs` regenerates the manual's images from the demo library (with IGDB/SteamGridDB keys it downloads the real covers into a throwaway folder; cover files are never committed, and the images show cover art that belongs to its owners, for illustration).
6. The maintainer merges with a merge commit once CI is green. Reviews may take a while; this is a spare-time project.

Never commit secrets: keys belong in `.env`, which is git-ignored. Run `git grep` for them before you push.

## License

By contributing you agree that your work is released under the [MIT license](LICENSE).
