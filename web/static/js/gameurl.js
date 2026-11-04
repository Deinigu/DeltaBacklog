// The open game in the address: `#/library?game=123` (any page). Pure string work, unit-tested (web/test/gameurl.test.mjs).
// The other parameters of the hash (the Library's filters) are left exactly as they are.

/** The game id in a hash like "#/library?q=x&game=123", or null. */
export function gameIdFromHash(hash) {
  const q = String(hash || '').split('?')[1];
  if (!q) return null;
  for (const part of q.split('&')) {
    const m = /^game=(\d+)$/.exec(part);
    if (m && Number(m[1]) > 0) return Number(m[1]);
  }
  return null;
}

/** The hash with `game=<id>` set, or removed when `id` is null. Everything else stays as it was. */
export function hashWithGame(hash, id) {
  const [path, ...rest] = String(hash || '').split('?');
  const parts = rest.join('?').split('&').filter((p) => p && !/^game=/.test(p));
  if (id != null) parts.push(`game=${id}`);
  return parts.length ? `${path}?${parts.join('&')}` : path;
}
