// Data layer: one in-memory store loaded at startup; views read from it.
import { norm } from './util.js';

export const store = {
  games: [],
  byId: new Map(),
  statuses: [], // [{key, label, color, count}] in display order
  statusByCode: new Map(),
  statusOrder: new Map(),
  loaded: false,
};

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

export async function load() {
  const [games, facets] = await Promise.all([getJSON('/api/games'), getJSON('/api/facets')]);
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
