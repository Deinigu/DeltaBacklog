// Filter state shared by the Library and saved views. Pure logic: no DOM here.
//
// state = { q, in: {key: Set}, ex: {key: Set}, all: Set<key>, flags: Set, sorts: [], group: '' }
// A game passes a facet group when it has ANY (or ALL, if the group is in `all`) of the included
// values and NONE of the excluded ones.
import { parseSort, formatSort, GROUP_FIELDS } from './sort.js';
import { norm } from './util.js';
import { isShort } from './ttb.js';

export const GROUPS = [
  { key: 'status', label: 'Status', get: (g) => [g.status], multi: false },
  { key: 'platform', label: 'Platform', get: (g) => g.platforms, multi: true },
  { key: 'genre', label: 'Genre', get: (g) => g.genres, multi: true },
  { key: 'theme', label: 'Theme', get: (g) => g.themes, multi: true },
  { key: 'year', label: 'Year played', get: (g) => [...g.years.map(String), ...g.periods], multi: true },
  { key: 'ownership', label: 'Ownership', get: (g) => (g.ownership ? [g.ownership] : []), multi: false },
  { key: 'mode', label: 'Game mode', get: (g) => g.modes || [], multi: true },
  { key: 'perspective', label: 'Perspective', get: (g) => g.perspectives || [], multi: true },
  { key: 'player', label: 'Played with', get: (g) => g.players, multi: true },
];
export const FLAGS = [
  { key: 'favorite', label: '★ Favorite' },
  { key: 'vr', label: 'VR' },
  { key: 'completed', label: '100% completed' },
  { key: 'short', label: 'Short game (under 10 h)', test: isShort }, // time to beat from IGDB; games without one never match
];

const FLAG_TESTS = Object.fromEntries(FLAGS.filter((f) => f.test).map((f) => [f.key, f.test]));

export function emptyState() {
  return {
    q: '',
    in: Object.fromEntries(GROUPS.map((g) => [g.key, new Set()])),
    ex: Object.fromEntries(GROUPS.map((g) => [g.key, new Set()])),
    all: new Set(),
    flags: new Set(),
    sorts: [],
    group: '',
    layout: 'cards', // 'cards' | 'table'
  };
}

/** True when the game passes every active filter. `skip` ignores one group (for facet counts). */
export function matches(state, g, skip) {
  if (state.q && !state.q.split(/\s+/).every((t) => g._search.includes(t))) return false;
  for (const f of state.flags) if (!(FLAG_TESTS[f] ? FLAG_TESTS[f](g) : g[f])) return false;
  for (const grp of GROUPS) {
    if (grp.key === skip) continue;
    const inc = state.in[grp.key], exc = state.ex[grp.key];
    if (!inc.size && !exc.size) continue;
    const vals = grp.get(g);
    if (exc.size && vals.some((v) => exc.has(v))) return false;
    if (inc.size) {
      const ok = state.all.has(grp.key) ? [...inc].every((v) => vals.includes(v)) : vals.some((v) => inc.has(v));
      if (!ok) return false;
    }
  }
  return true;
}

/** Number of active filter items (for badges). */
export function activeCount(state) {
  let n = state.flags.size;
  for (const grp of GROUPS) n += state.in[grp.key].size + state.ex[grp.key].size;
  return n;
}

export function isPristine(state) {
  return !state.q && activeCount(state) === 0 && !state.sorts.length && !state.group && state.layout === 'cards';
}

/** Cycle a chip: off -> included -> excluded -> off. */
export function cycle(state, key, value) {
  const inc = state.in[key], exc = state.ex[key];
  if (inc.has(value)) { inc.delete(value); exc.add(value); }
  else if (exc.has(value)) exc.delete(value);
  else inc.add(value);
  if (inc.size < 2) state.all.delete(key);
}

export const chipState = (state, key, value) => (state.in[key].has(value) ? 'in' : state.ex[key].has(value) ? 'ex' : 'off');

// ---- saved-view JSON: {"q":"","in":{"genre":["RPG"]},"ex":{},"all":["genre"],"flags":["vr"]} ----
const arr = (sets) => Object.fromEntries(Object.entries(sets).filter(([, s]) => s.size).map(([k, s]) => [k, [...s]]));

export function filterToJSON(state) {
  const out = {};
  if (state.q) out.q = state.q;
  const i = arr(state.in), e = arr(state.ex);
  if (Object.keys(i).length) out.in = i;
  if (Object.keys(e).length) out.ex = e;
  if (state.all.size) out.all = [...state.all];
  if (state.flags.size) out.flags = [...state.flags];
  return out;
}

export function stateFromView(view) {
  const s = emptyState();
  const f = view.filter || {};
  s.q = typeof f.q === 'string' ? norm(f.q) : '';
  for (const grp of GROUPS) {
    for (const v of f.in?.[grp.key] || []) s.in[grp.key].add(v);
    for (const v of f.ex?.[grp.key] || []) s.ex[grp.key].add(v);
  }
  for (const k of f.all || []) if (GROUPS.some((g) => g.key === k && g.multi)) s.all.add(k);
  for (const k of f.flags || []) if (FLAGS.some((x) => x.key === k)) s.flags.add(k);
  s.sorts = (view.sort || []).filter((l) => l && typeof l.field === 'string').map((l) => ({ field: l.field, dir: l.dir === 'desc' ? 'desc' : 'asc' }));
  s.group = GROUP_FIELDS[view.group_by] ? view.group_by : '';
  s.layout = view.layout === 'table' ? 'table' : 'cards';
  return s;
}

/** Canonical string used to tell whether a view has unsaved changes. */
export function signature(state) {
  return JSON.stringify([filterToJSON(state), state.sorts, state.group, state.layout]);
}

// ---- URL query (library scratch view): status=x  ex_status=x  all=genre,theme  favorite=1  sort=  group= ----
export function stateFromQuery(search) {
  const qs = new URLSearchParams(search || '');
  const s = emptyState();
  s.q = norm(qs.get('q') || '');
  for (const grp of GROUPS) {
    for (const v of qs.getAll(grp.key)) s.in[grp.key].add(v);
    for (const v of qs.getAll('ex_' + grp.key)) s.ex[grp.key].add(v);
  }
  for (const k of (qs.get('all') || '').split(',')) if (GROUPS.some((g) => g.key === k && g.multi)) s.all.add(k);
  for (const f of FLAGS) if (qs.get(f.key) === '1') s.flags.add(f.key);
  s.sorts = parseSort(qs.get('sort'));
  s.group = GROUP_FIELDS[qs.get('group')] ? qs.get('group') : '';
  s.layout = qs.get('layout') === 'table' ? 'table' : 'cards';
  return s;
}

export function stateToQuery(state) {
  const qs = new URLSearchParams();
  if (state.q) qs.set('q', state.q);
  for (const grp of GROUPS) {
    for (const v of state.in[grp.key]) qs.append(grp.key, v);
    for (const v of state.ex[grp.key]) qs.append('ex_' + grp.key, v);
  }
  if (state.all.size) qs.set('all', [...state.all].join(','));
  for (const f of state.flags) qs.set(f, '1');
  if (state.sorts.length) qs.set('sort', formatSort(state.sorts));
  if (state.group) qs.set('group', state.group);
  if (state.layout === 'table') qs.set('layout', 'table');
  return qs.toString();
}

/** A string that changes when any filter chip value appears or disappears (a new genre, platform, year...). */
export function facetSignature(games) {
  return GROUPS.map((grp) => {
    const seen = new Set();
    for (const g of games) for (const v of grp.get(g)) seen.add(v);
    return [...seen].sort().join('\u0001');
  }).join('\u0002');
}
