// Series progress: group games by series and count how far through each one the user is. Pure (no DOM): unit-tested.
// Definitions (see the user manual): a game is *finished* when its status is Finished; it is *played* when it is
// finished, has any year or period played, or its status says it was started (Playing, Evergreen, Hold, Dropped).

const STARTED = new Set(['playing', 'playing_evergreen', 'evergreen', 'hold', 'dropped']);
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

export const isFinished = (g) => g.status === 'finished';

export const isPlayed = (g) => isFinished(g) || STARTED.has(g.status) || g.years.length > 0 || g.periods.length > 0;

/** Still worth playing: never played, and not marked Not Interested. */
export const isLeft = (g) => !isPlayed(g) && g.status !== 'not_interested';

/** Games of one series: release year ascending (unknown year last), then title. */
export function compareInSeries(a, b) {
  const ya = a.release_year ?? Infinity, yb = b.release_year ?? Infinity;
  if (ya !== yb) return ya < yb ? -1 : 1;
  return collator.compare(a.title, b.title) || a.id - b.id;
}

const keyOf = (name) => name.trim().toLowerCase();

/**
 * Every series that has at least one game of the library, as
 *  { key, name, games (ordered), total, finished, played, left, noYear }
 * `noYear` counts the games without a release year (the order is then only partly known).
 * `played` includes the finished games. Series are ordered by name; names that differ only in case are one series.
 */
export function buildSeries(games) {
  const map = new Map();
  for (const g of games) {
    if (!g.series || !g.series.trim()) continue;
    const key = keyOf(g.series);
    let s = map.get(key);
    if (!s) map.set(key, (s = { key, name: g.series.trim(), games: [] }));
    s.games.push(g);
  }
  const out = [...map.values()];
  for (const s of out) {
    s.games.sort(compareInSeries);
    s.total = s.games.length;
    s.finished = s.games.filter(isFinished).length;
    s.played = s.games.filter(isPlayed).length;
    s.left = s.games.filter(isLeft).length;
    s.noYear = s.games.filter((g) => g.release_year == null).length;
  }
  return out.sort((a, b) => collator.compare(a.name, b.name));
}

/** Series that still have a game left to play. */
export const withLeft = (series) => series.filter((s) => s.left > 0);

/** Bar segment widths in percent: finished, played but not finished, and the rest. */
export function barShares(s) {
  if (!s.total) return { finished: 0, played: 0, rest: 100 };
  const finished = (s.finished / s.total) * 100;
  const played = ((s.played - s.finished) / s.total) * 100;
  return { finished, played, rest: 100 - finished - played };
}

/** "3 finished · 4 played · 5 games" for the series header and screen readers. */
export function progressText(s) {
  return `${s.finished} finished · ${s.played} played · ${s.total} ${s.total === 1 ? 'game' : 'games'}`;
}

/**
 * Where a game stands in its series: { position, total, name }, or null outside a multi-game series.
 * `position` follows release order and is null while any game of the series has no release year (it would be a guess).
 */
export function seriesPosition(game, games) {
  if (!game.series || !game.series.trim()) return null;
  const s = buildSeries(games).find((x) => x.key === keyOf(game.series));
  if (!s || s.total < 2) return null;
  const i = s.games.findIndex((g) => g.id === game.id);
  if (i < 0) return null;
  return { position: s.noYear ? null : i + 1, total: s.total, name: s.name };
}

// ---- games of the series' IGDB collections that the library does not have ("not registered") ----

/** The key the page and the server group a series by: its name, trimmed and lower-cased. */
export const seriesKey = keyOf;

const EMPTY_EXTRA = Object.freeze({ missing: [], hidden: [], collections: [] });

/**
 * Index the answer of GET api/series/missing by series key: { missing, hidden, collections } per series.
 * A game that has become a library game since the answer was made is dropped, so adding one needs no new refresh.
 * IGDB splits one game into several entries (the original, an expanded edition, a remaster), so a game counts as owned
 * with its family, like on the server: the root of its version chain (`parent`, or the game itself) is a root the server
 * listed in `owned_roots`, or is (or is the parent of) a game added since.
 */
export function missingIndex(resp, games) {
  const owned = new Set(games.map((g) => g.igdb_id).filter((id) => id != null));
  const roots = new Set(resp?.owned_roots ?? []);
  const isOwned = (m) => owned.has(m.igdb_id) || roots.has(m.parent > 0 ? m.parent : m.igdb_id) || (m.parent > 0 && owned.has(m.parent));
  const out = new Map();
  for (const s of resp?.series ?? []) {
    out.set(s.key, {
      missing: s.missing.filter((m) => !isOwned(m)),
      hidden: s.hidden.filter((m) => !isOwned(m)),
      collections: s.collections,
    });
  }
  return out;
}

/** What the index holds for one series (never undefined). */
export const extraOf = (index, s) => index.get(s.key) ?? EMPTY_EXTRA;

/** Series that have at least one game the library does not have. */
export const withMissing = (series, index) => series.filter((s) => extraOf(index, s).missing.length > 0);

/**
 * Move a game of the answer of GET api/series/missing between its `missing` and `hidden` lists (to = 'hidden' or
 * 'missing'), matching igdb_id and collection_id, so that the index rebuilt from the answer (on every games-changed)
 * keeps what the owner hid or showed. Shared by the Series and Upcoming pages.
 */
export function moveHidden(resp, m, to) {
  const from = to === 'hidden' ? 'missing' : 'hidden';
  for (const e of resp?.series ?? []) {
    const i = e[from].findIndex((x) => x.igdb_id === m.igdb_id && x.collection_id === m.collection_id);
    if (i < 0) continue;
    const [x] = e[from].splice(i, 1);
    e[to].push(x);
    e[to].sort((a, b) => (a.year ?? Infinity) - (b.year ?? Infinity) || a.title.localeCompare(b.title));
  }
}

/**
 * The items of an opened series in release order: the library's games and the games not registered:
 * { kind: 'owned', game } or { kind: 'missing', game: {igdb_id, title, year, cover, collection_id} }.
 * Same order as compareInSeries (year ascending, unknown last, then title); an owned game comes before a game of
 * the same year and title that is missing.
 */
export function shelfItems(s, extra) {
  const items = s.games.map((game) => ({ kind: 'owned', game, year: game.release_year, title: game.title }));
  for (const m of extra.missing) items.push({ kind: 'missing', game: m, year: m.year, title: m.title });
  return items.sort((a, b) => {
    const ya = a.year ?? Infinity, yb = b.year ?? Infinity;
    if (ya !== yb) return ya < yb ? -1 : 1;
    return collator.compare(a.title, b.title) || (a.kind === b.kind ? 0 : a.kind === 'owned' ? -1 : 1);
  });
}

/**
 * A big series lists dozens of games the library lacks: keep the library's games, and only the first
 * `limit` games that are missing (release order is kept). Returns { items, hidden } (how many were left out).
 */
export function collapseMissing(items, limit) {
  let seen = 0, hidden = 0;
  const out = items.filter((it) => {
    if (it.kind !== 'missing') return true;
    if (seen++ < limit) return true;
    hidden++;
    return false;
  });
  return { items: out, hidden };
}

/**
 * Up to `n` games whose pictures make the series' fan on its tile, in release order: the games with a portrait poster
 * (`hasPoster`); when none has one, the games with any image (`hasImage`, default `hasPoster`); else the first games.
 * Among the posters, the games `prefer` accepts (linked to IGDB: their poster is IGDB's own cover, not a fuzzy
 * SteamGridDB match) are taken first; the pick is shown in release order.
 */
export function fanGames(s, hasPoster, n = 4, hasImage = hasPoster, prefer = () => false) {
  const posters = s.games.filter(hasPoster);
  if (posters.length) {
    const first = [...posters.filter(prefer), ...posters.filter((g) => !prefer(g))].slice(0, n);
    return posters.filter((g) => first.includes(g));
  }
  const withImg = s.games.filter(hasImage);
  return (withImg.length ? withImg : s.games).slice(0, n);
}

/** "2 finished · 3 played · 4 games · 1 left to play · 3 not in your library" */
export function seriesSummary(s, extra) {
  let t = progressText(s);
  if (s.left) t += ` · ${s.left} left to play`;
  if (extra.missing.length) t += ` · ${extra.missing.length} not in your library`;
  return t;
}

/** The oldest time the cached collections of these series were read (an ISO string), or null when none was. */
export function oldestFetch(index) {
  let old = null;
  for (const e of index.values()) for (const c of e.collections) if (!old || c.fetched_at < old) old = c.fetched_at;
  return old;
}
