# Changelog

## Unreleased

### Security
- Cross-origin POST/PUT/PATCH/DELETE requests are rejected (403) with `http.CrossOriginProtection`.
- JSON write routes under `/api/` require `Content-Type: application/json` (415 otherwise).
- Host allow-list against DNS rebinding (421); extra names via the new `ALLOWED_HOSTS` variable. `/healthz` answers for any host.
- Error bodies from the guard are JSON (`{"error": ...}`); cross-site and same-site `GET /api/*` are rejected too, so other sites cannot trigger lookups.
- Added `SECURITY.md`.
