# Security

DeltaBacklog is a single-user app for a home network. **It has no authentication**: anyone who can reach the port can read and change the library. Do not expose it to the internet; put it behind a VPN or an authenticating reverse proxy (it must forward the original `Host` header, and that public name must be listed in `ALLOWED_HOSTS`, or the app answers 421) if you need remote access.

## What is mitigated

- **Cross-site writes (CSRF).** A web page opened in a browser on your network cannot create, change or delete data. Unsafe requests (POST, PUT, PATCH, DELETE) are checked with Go's `http.CrossOriginProtection` (`Sec-Fetch-Site` / `Origin`) and rejected with 403 when they come from another origin. Same-origin requests from the app and non-browser clients (curl, the backup timer) keep working.
- **Content type.** POST, PUT and PATCH under `/api/` must be `Content-Type: application/json` (415 otherwise), so a simple cross-site form or `text/plain` request is never read as JSON.
- **DNS rebinding.** The `Host` header must be an IP address, `localhost`, `deltabacklog.local`, the machine's hostname or `<hostname>.local`, or a name listed in `ALLOWED_HOSTS` (comma separated); anything else gets 421. `/healthz` (GET and HEAD only) answers for any host so monitors and deploy scripts work.

## What is not mitigated

- No login: anyone on the LAN (or anyone who can reach the port) can use the app and its API directly.
- No HTTPS: traffic is plain HTTP.
- A page on the same machine under the same host name and port is same-origin by definition.

## Reporting

Open a GitHub issue, or for something sensitive contact the maintainer privately through their GitHub profile.
