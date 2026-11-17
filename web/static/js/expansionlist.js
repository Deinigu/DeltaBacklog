// The Expansions page's search, filters and sort. Pure (no DOM): unit-tested. The rows are every expansion of the library
// ({x, game}, see allExpansions in expansions.js); this module decides which are shown and in what order.
import { allExpansions, isOwned, releaseKey } from './expansions.js';
import { matchesText, override, params, qOf, setOf, appendSet, byName, tally } from './listfilter.js';

/** The status chip for an expansion the owner has not given a status. */
export const NOT_TRACKED = 'none';

/** Ownership groups: owned (any value but Nope), not owned (Nope) and not set (no ownership yet). */
export const OWNERSHIP = [
  { key: 'owned', label: 'Owned' },
  { key: 'not_owned', label: 'Not owned' },
  { key: 'unset', label: 'Not set' },
];
const OWNERSHIP_KEYS = OWNERSHIP.map((o) => o.key);

export const SORTS = [
  { key: 'game', label: 'Game' },
  { key: 'release', label: 'Release date' },
  { key: 'title', label: 'Title' },
  { key: 'changed', label: 'Recently changed' },
];
export const DEFAULT_SORT = 'game';
const SORT_KEYS = SORTS.map((s) => s.key);

export function emptyExpansionFilter() {
  return { q: '', status: new Set(), ownership: new Set(), platforms: new Set(), baseFinished: false, hidden: false };
}

export const expansionFilterCount = (f) => f.status.size + f.ownership.size + f.platforms.size + (f.baseFinished ? 1 : 0) + (f.hidden ? 1 : 0);

export const statusKey = (x) => x.status || NOT_TRACKED;
export const ownershipKey = (x) => (!x.ownership ? 'unset' : isOwned(x) ? 'owned' : 'not_owned');

/** Whether a row passes the filter. Hidden expansions are shown only when the filter asks for them (and then only those). */
export function expansionPasses(f, { x, game }) {
  if (f.hidden !== !!x.hidden) return false;
  if (f.q && !matchesText(f.q, x.title, game.title)) return false;
  if (f.status.size && !f.status.has(statusKey(x))) return false;
  if (f.ownership.size && !f.ownership.has(ownershipKey(x))) return false;
  if (f.platforms.size && !(game.platforms || []).some((p) => f.platforms.has(p))) return false;
  if (f.baseFinished && game.status !== 'finished') return false;
  return true;
}

/** The rows of every expansion the page could show (hidden ones too: the filter decides). */
export const poolOf = (games) => allExpansions(games, { hidden: true });

const COMPARE = {
  game: (a, b) => byName(a.game.title, b.game.title) || releaseKey(a.x) - releaseKey(b.x) || byName(a.x.title, b.x.title),
  release: (a, b) => cmpNum(releaseKey(b.x), releaseKey(a.x)) || byName(a.x.title, b.x.title), // newest first, TBA first
  title: (a, b) => byName(a.x.title, b.x.title) || byName(a.game.title, b.game.title),
  changed: (a, b) => (b.x.updated_at || '').localeCompare(a.x.updated_at || '') || byName(a.x.title, b.x.title),
};
// Infinity - Infinity is NaN: compare by hand
const cmpNum = (a, b) => (a === b ? 0 : a < b ? -1 : 1);

export function filterExpansions(pool, f, sort = DEFAULT_SORT) {
  return pool.filter((row) => expansionPasses(f, row)).sort(COMPARE[sort] || COMPARE[DEFAULT_SORT]);
}

/** How many rows a chip would show: the filter with that group set to `value` only. */
export function countExpansions(pool, f, key, value) {
  const g = override(f, key, value);
  return pool.filter((row) => expansionPasses(g, row)).length;
}

/** The platforms of the base games of the pool, most expansions first. */
export const platformsOf = (pool) => tally(pool, ({ game }) => game.platforms || []);

// ---- the address: #/expansions?q=witcher&status=finished&status=none&own=owned&platform=PC&base=finished&sort=release ----

export function expansionStateFromQuery(search) {
  const qs = params(search);
  const f = emptyExpansionFilter();
  f.q = qOf(qs);
  f.status = setOf(qs, 'status');
  f.ownership = setOf(qs, 'own', OWNERSHIP_KEYS);
  f.platforms = setOf(qs, 'platform');
  f.baseFinished = qs.get('base') === 'finished';
  f.hidden = qs.get('hidden') === '1';
  const sort = qs.get('sort');
  return { filter: f, sort: SORT_KEYS.includes(sort) ? sort : DEFAULT_SORT };
}

export function expansionStateToQuery({ filter: f, sort }) {
  const qs = new URLSearchParams();
  if (f.q) qs.set('q', f.q);
  appendSet(qs, 'status', f.status);
  appendSet(qs, 'own', f.ownership, OWNERSHIP_KEYS);
  appendSet(qs, 'platform', f.platforms);
  if (f.baseFinished) qs.set('base', 'finished');
  if (f.hidden) qs.set('hidden', '1');
  if (sort && sort !== DEFAULT_SORT) qs.set('sort', sort);
  return qs.toString();
}

/**
 * The finish time of a run (GET api/expansions/sync) whose results the browser has not loaded yet, or null. `seen` is the finish
 * time last loaded: a run that ended while the page was closed (or on another device) still gets its expansions into the store.
 */
export const runToLoad = (st, seen) => (st && !st.running && st.finished_at && st.finished_at !== seen ? st.finished_at : null);

/** "N of M" text helper input: the pool size without the hidden ones (what the page counts as "all"). */
export const poolCount = (pool, f) => pool.filter((r) => !!r.x.hidden === f.hidden).length;

