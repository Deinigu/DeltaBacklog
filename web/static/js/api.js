// Data layer: one in-memory store loaded at startup; views read from it.
import { norm } from './util.js';

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

export async function load() {
  const [games, facets, views] = await Promise.all([getJSON('/api/games'), getJSON('/api/facets'), getJSON('/api/views')]);
  store.views = views;
  store.games = games;
  for (const g of games) g._search = norm([g.title, g.series, g.notes, g.developer].filter(Boolean).join(' '));
  store.byId = new Map(games.map((g) => [g.id, g]));
  store.statuses = facets.status;
  store.statusByCode = new Map(facets.status.map((s) => [s.key, s]));
  store.statusOrder = new Map(facets.status.map((s, i) => [s.key, i]));
  store.loaded = true;
}

export function coverURL(g) {
  return g.cover ? `/covers/${g.cover}?v=${encodeURIComponent(g.updated_at)}` : null;
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
