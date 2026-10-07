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
  return g.cover ? `/covers/${g.cover}?v=${encodeURIComponent(g.updated_at)}` : null;
}

// ---- game edits ----
const gamesChanged = () => window.dispatchEvent(new Event('games-changed'));

const NULLABLE_TEXT = ['series', 'developer', 'ownership'];
const NULLABLE_NUM = ['release_year', 'series_order'];

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
  gamesChanged();

  const slot = inFlight.get(id) || { chain: Promise.resolve(), count: 0 };
  inFlight.set(id, slot);
  slot.count++;
  const run = async () => {
    try {
      const fresh = await send('PATCH', `/api/games/${id}`, patch);
      if (slot.count === 1) { Object.assign(g, fresh); indexGame(g); } // this is the newest edit: authoritative
    } catch (e) {
      if (slot.count === 1) Object.assign(g, prev);
      throw e;
    } finally {
      if (--slot.count === 0) inFlight.delete(id);
      gamesChanged();
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
  gamesChanged();
  try {
    const next = await send('POST', '/api/games/reorder', ids);
    for (const [id, pos] of Object.entries(next)) store.byId.get(Number(id)).queue_pos = pos;
  } catch (e) {
    for (const [id, pos] of before) store.byId.get(id).queue_pos = pos;
    throw e;
  } finally {
    gamesChanged();
  }
}

// ---- saved views ----
async function send(method, url, body) {
  const res = await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  if (!res.ok) throw new Error(`${method} ${url}: ${res.status}`);
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
