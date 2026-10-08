// Data layer: one in-memory store loaded at startup; views read from it.
import { norm } from './util.js';
import { slotOrder } from './order.js';

export const store = {
  games: [],
  byId: new Map(),
  statuses: [], // [{key, label, color, count}] in display order
  statusByCode: new Map(),
  statusOrder: new Map(),
  views: [],
  linkPending: 0, // games waiting in the IGDB review queue; the nav shows its link while > 0
  loaded: false,
};

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

/** Recompute the lowercase, accent-free search text of a game after it changes. */
export function indexGame(g) {
  g._search = norm([g.title, g.series, g.notes, g.developer].filter(Boolean).join(' '));
}

export async function load() {
  const [games, facets, views] = await Promise.all([getJSON('/api/games'), getJSON('/api/facets'), getJSON('/api/views')]);
  store.views = views;
  store.games = games;
  for (const g of games) indexGame(g);
  store.byId = new Map(games.map((g) => [g.id, g]));
  store.statuses = facets.status;
  store.statusByCode = new Map(facets.status.map((s) => [s.key, s]));
  store.statusOrder = new Map(facets.status.map((s, i) => [s.key, i]));
  store.loaded = true;
}

export function coverURL(g) {
  // cover_v changes only with the image itself, so editing a game never re-downloads its cover
  return g.cover ? `/covers/${g.cover}?v=${encodeURIComponent(g.cover_v || '')}` : null;
}

// ---- game edits ----
// detail.id: the one game that changed (views rebuild only its card); detail.reorder: only positions moved.
const gamesChanged = (id = null, extra = {}) => window.dispatchEvent(new CustomEvent('games-changed', { detail: { id, ...extra } }));

/** Replace a game with the server's copy: the safe way out of a failed edit when others were chained behind it. */
async function resync(id) {
  try {
    const fresh = (await getJSON('/api/games')).find((x) => x.id === id);
    const g = store.byId.get(id);
    if (fresh && g) { Object.assign(g, fresh); indexGame(g); }
  } catch { /* offline: the next load fixes it */ }
}

const NULLABLE_TEXT = ['series', 'developer', 'ownership'];
const NULLABLE_NUM = ['release_year'];

/** The values the server will store for a patch: cleared text and zero numbers become null. */
export function localPatch(patch) {
  const o = { ...patch };
  for (const k of NULLABLE_TEXT) if (o[k] === '') o[k] = null;
  for (const k of NULLABLE_NUM) if (o[k] === 0) o[k] = null;
  return o;
}

// Edits to one game are sent one at a time, in order, so a slow response can never overwrite a newer edit.
const inFlight = new Map(); // game id -> { chain, count }

/**
 * Quick edit of any game field. The local game changes immediately (optimistic); the server's answer
 * is applied only when no newer edit of the same game is still pending, and a failure restores only
 * the fields this edit touched.
 */
export function patchGame(id, patch) {
  const g = store.byId.get(id);
  const prev = Object.fromEntries(Object.keys(patch).map((k) => [k, g[k]]));
  Object.assign(g, localPatch(patch));
  gamesChanged(id);

  const slot = inFlight.get(id) || { chain: Promise.resolve(), count: 0 };
  inFlight.set(id, slot);
  slot.count++;
  const run = async () => {
    try {
      const fresh = await send('PATCH', `/api/games/${id}`, patch);
      if (slot.count === 1) { Object.assign(g, fresh); indexGame(g); } // this is the newest edit: authoritative
    } catch (e) {
      slot.failed = true;
      if (slot.count === 1) Object.assign(g, prev);
      throw e;
    } finally {
      if (--slot.count === 0) {
        inFlight.delete(id);
        // Edits chained behind a failed one captured values that were never saved: ask the server what is true.
        if (slot.failed) await resync(id);
      }
      gamesChanged(id);
    }
  };
  const result = slot.chain.then(run, run);
  slot.chain = result.catch(() => {});
  return result;
}

/** Persist a manual order for the given ids (the games a view shows, in the new order). */
export async function reorderGames(ids) {
  const before = new Map(ids.map((id) => [id, store.byId.get(id).queue_pos]));
  for (const [id, pos] of slotOrder(ids, before)) store.byId.get(id).queue_pos = pos; // optimistic
  gamesChanged(null, { reorder: true });
  try {
    const next = await send('POST', '/api/games/reorder', ids);
    for (const [id, pos] of Object.entries(next)) store.byId.get(Number(id)).queue_pos = pos;
  } catch (e) {
    for (const [id, pos] of before) store.byId.get(id).queue_pos = pos;
    throw e;
  } finally {
    gamesChanged(null, { reorder: true });
  }
}

// ---- saved views ----
async function send(method, url, body) {
  const res = await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  if (!res.ok) {
    const err = new Error(`${method} ${url}: ${res.status}`);
    err.status = res.status;
    err.data = await res.json().catch(() => null);
    throw err;
  }
  return res.status === 204 ? null : res.json();
}
const changed = () => window.dispatchEvent(new Event('views-changed'));

export async function createView(v) {
  const created = await send('POST', '/api/views', v);
  store.views.push(created);
  changed();
  return created;
}
export async function updateView(id, patch) {
  const v = await send('PATCH', `/api/views/${id}`, patch);
  store.views = store.views.map((x) => (x.id === id ? v : x));
  changed();
  return v;
}
export async function deleteView(id) {
  await send('DELETE', `/api/views/${id}`);
  store.views = store.views.filter((x) => x.id !== id);
  changed();
}
export async function reorderViews(ids) {
  store.views = await send('PUT', '/api/views/order', ids);
  changed();
}

// ---- adding, deleting, covers, lookups ----
async function request(method, url, body) {
  const res = await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = res.status === 204 ? null : await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data?.error || `${method} ${url}: ${res.status}`);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}
// The list of games itself changed (added or deleted): views must rebuild, not just refresh.
const reloaded = () => window.dispatchEvent(new Event('games-reloaded'));

/** Search IGDB by title or store link. Resolves {configured, query, results}. */
export const lookup = (q) => request('GET', `/api/lookup?q=${encodeURIComponent(q)}`);
/**
 * Cover options. source: 'steamgriddb' (search by title) or 'igdb' (the pictures IGDB holds; igdbId links to the
 * exact game when known). Resolves {configured, source, covers}.
 */
export function coverSearch(q, { source = 'steamgriddb', igdbId = 0 } = {}) {
  const p = new URLSearchParams({ q });
  if (source !== 'steamgriddb') p.set('source', source);
  if (igdbId) p.set('igdb_id', String(igdbId));
  return request('GET', `/api/covers/search?${p}`);
}

/** Create a game. Resolves {game, cover_error}; rejects with err.status 409 and err.data.id when it already exists. */
export async function createGame(body) {
  const data = await request('POST', '/api/games', body);
  const g = data.game;
  indexGame(g);
  store.games.push(g);
  store.byId.set(g.id, g);
  reloaded();
  return data;
}

export async function deleteGame(id) {
  await request('DELETE', `/api/games/${id}`);
  store.games = store.games.filter((g) => g.id !== id);
  store.byId.delete(id);
  reloaded();
}

/** Replace a game's cover with an image from the metadata CDNs. */
export async function setCover(id, url) {
  const fresh = await request('POST', `/api/games/${id}/cover`, { url });
  const g = store.byId.get(id);
  Object.assign(g, fresh);
  indexGame(g);
  gamesChanged(id);
  return g;
}

// ---- Link with IGDB (review queue of the IGDB sync) ----

/** Games the IGDB sync could not link on its own. Resolves [{game_id, title, year, platforms, status, candidates:[{igdb_id, name, year, score, platforms, slug}]}]. */
export async function linkQueue() {
  return (await request('GET', '/api/link/queue')).items;
}

/** Link a game to an IGDB game (a proposal, or one picked with the lookup). Rejects with err.status 409 when it is taken. */
export async function linkAccept(gameId, igdbId) {
  await request('POST', '/api/link/accept', { game_id: gameId, igdb_id: igdbId });
  const g = store.byId.get(gameId);
  if (g) g.igdb_id = igdbId;
}

/** Record that a game is not on IGDB, so the sync never searches for it again. */
export const linkNone = (gameId) => request('POST', '/api/link/none', { game_id: gameId });

/** Refresh store.linkPending (the navigation shows "Link IGDB" while the review queue is not empty). */
export async function loadLinkPending() {
  try { store.linkPending = (await linkQueue()).length; } catch { store.linkPending = 0; }
}

// ---- tier lists ----
export const getTierlist = (year) => getJSON(`/api/tierlist/${year}`);
/** Replace a year's tier list; `entries` is [{game_id, tier_id}] (see yearreview.toEntries). */
export const saveTierlist = (year, entries) => send('PUT', `/api/tierlist/${year}`, { entries });
