# Security policy

## Design: a single-user app for a trusted network

DeltaBacklog is built to run on a home network, for one person or one household. **It has no authentication, no user accounts and no TLS**, by design:

- Anyone who can reach the port can read **and change or delete** the whole library.
- It serves plain HTTP on `LISTEN_ADDR` (default `:8081`, every network interface).

**Do not expose it to the internet.** Do not forward its port on your router. If you need access from outside, put it behind a reverse proxy that adds HTTPS and authentication (for example Caddy, Traefik or nginx with basic auth or an identity-aware proxy), or reach your home network with a VPN such as WireGuard or Tailscale. To keep it on one machine, set `LISTEN_ADDR=127.0.0.1:8081`.

What the app does to limit damage even on a trusted network: the service on the Raspberry Pi runs as an unprivileged user with systemd sandboxing, the API keys (IGDB, SteamGridDB) are read from environment variables or a git-ignored `.env` and never sent to the browser, covers are only downloaded from an allow-list of known hosts without following redirects, and image sizes are bounded.

## Supported versions

Only the latest release (and `main`) receives security fixes. The project is young; please update before reporting.

| Version | Supported |
|---|---|
| `main` | yes |
| latest release | yes |
| older releases | no |

## Reporting a vulnerability

Please report it **privately** with GitHub's *private vulnerability reporting*: on the repository page open **Security → Report a vulnerability**. Please do not open a public issue or pull request for it.

Include what you found, how to reproduce it, and the version or commit. You can expect an acknowledgement within about a week; this is a one-person, spare-time project, so please be patient. Once a fix is released the report is disclosed, and you are credited if you wish.

Known limitation, not a vulnerability report: the app has no authentication, so **any web page opened in a browser on your network can currently send requests to it and change the library** (a malicious page can add games, reorder the queue or change covers, and a DNS-rebinding page may read and delete data). The app does not yet check where a request comes from. Do not browse untrusted sites from a machine that can reach it while it holds data you care about, keep backups, and never expose it to the internet. Protection against such cross-site requests is planned; this document will describe it once it exists.

Out of scope: the missing authentication and TLS described above.
