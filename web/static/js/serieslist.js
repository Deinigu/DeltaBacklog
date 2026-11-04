// The Series page's search, filters and sort. Pure (no DOM): unit-tested. The series themselves come from series.js
// (buildSeries); this module only decides which of them are shown and in what order.
import { extraOf } from './series.js';
import { matchesText, override, params, qOf, setOf, appendSet, byName, tally } from './listfilter.js';

/** How far through a series the owner is. `completed` is isCompleted; `progress` has a played game; `notstarted` has none. */
export const PROGRESS = [
  { key: 'completed', label: 'Completed' },
  { key: 'progress', label: 'In progress' },
  { key: 'notstarted', label: 'Not started' },
];
export const SORTS = [
  { key: 'name', label: 'Name' },
  { key: 'finished', label: '% finished' },
  { key: 'games', label: 'Number of games' },
  { key: 'played', label: 'Last played' },
];
export const DEFAULT_SORT = 'name';

export function emptySeriesFilter() {
  return { q: '', progress: new Set(), left: false, missing: false, platforms: new Set() };
}

export const seriesFilterCount = (f) => f.progress.size + (f.left ? 1 : 0) + (f.missing ? 1 : 0) + f.platforms.size;

/**
 * A series is *completed* when every game of it that the owner has is finished. Games the library does not have (the
 * "not in library" ones read from IGDB, hidden or not, whole families included: see missingIndex) are not in `s.games` at
 * all, so they never count. A library game with the ownership "Nope" is one the owner does not have: it is ignored while it is
 * not finished (a finished one counts, the owner did play it): buildSeries (series.js) counts them out of `owned`, which the
 * progress bar, the card text and the % finished sort use too, so the mark and the numbers always agree.
 */
export const isCompleted = (s) => s.owned > 0 && s.finished === s.owned;

/** 'completed', 'progress' or 'notstarted'. */
export const progressOf = (s) => (isCompleted(s) ? 'completed' : s.played > 0 ? 'progress' : 'notstarted');

/** The year the series was last played in (the latest year played of any of its games), 0 when it never was. */
export function lastPlayed(s) {
  let y = 0;
  for (const g of s.games) for (const v of g.years ?? []) if (v > y) y = v;
  return y;
}

/** The platforms of the games of a series. */
export const platformsOf = (s) => [...new Set(s.games.flatMap((g) => g.platforms ?? []))];

/** Whether a series passes the filter. `index` is missingIndex(): the games it lacks. */
export function seriesPasses(f, s, index) {
  if (f.q && !matchesText(f.q, s.name, ...s.games.map((g) => g.title))) return false;
  if (f.progress.size && !f.progress.has(progressOf(s))) return false;
  if (f.left && !(s.left > 0)) return false;
  if (f.missing && !(extraOf(index, s).missing.length > 0)) return false;
  if (f.platforms.size && !platformsOf(s).some((p) => f.platforms.has(p))) return false;
  return true;
}

const share = (s) => (s.owned ? s.finished / s.owned : 0);
const COMPARE = {
  name: (a, b) => byName(a.name, b.name),
  finished: (a, b) => share(b) - share(a) || b.finished - a.finished || byName(a.name, b.name),
  games: (a, b) => b.total - a.total || byName(a.name, b.name),
  played: (a, b) => lastPlayed(b) - lastPlayed(a) || byName(a.name, b.name), // never played last
};

/** The series that pass the filter, in the order of `sort` (a SORTS key). */
export function filterSeries(all, f, sort, index) {
  return all.filter((s) => seriesPasses(f, s, index)).sort(COMPARE[sort] || COMPARE.name);
}

/** How many series a chip would show: the filter with that group set to `value` only. */
export const countSeries = (all, f, index, key, value) => {
  const g = override(f, key, value);
  return all.filter((s) => seriesPasses(g, s, index)).length;
};

/** The platforms of the series, the most series first: [{value, count}]. */
export const seriesPlatforms = (all) => tally(all, platformsOf);

// ---- the address: #/series?q=zelda&progress=completed&left=1&missing=1&platform=PC&sort=finished ----
const PROGRESS_KEYS = PROGRESS.map((p) => p.key);

/** The page state of an address query: {filter, sort}. Anything unknown is dropped. */
export function seriesStateFromQuery(search) {
  const qs = params(search);
  const f = emptySeriesFilter();
  f.q = qOf(qs);
  f.progress = setOf(qs, 'progress', PROGRESS_KEYS);
  f.left = qs.get('left') === '1';
  f.missing = qs.get('missing') === '1';
  f.platforms = setOf(qs, 'platform');
  const sort = SORTS.some((s) => s.key === qs.get('sort')) ? qs.get('sort') : DEFAULT_SORT;
  return { filter: f, sort };
}

/** The address query of a page state (only what differs from the default); '' for the default page. */
export function seriesStateToQuery({ filter: f, sort = DEFAULT_SORT }) {
  const qs = new URLSearchParams();
  if (f.q) qs.set('q', f.q);
  appendSet(qs, 'progress', f.progress, PROGRESS_KEYS);
  if (f.left) qs.set('left', '1');
  if (f.missing) qs.set('missing', '1');
  appendSet(qs, 'platform', f.platforms);
  if (sort !== DEFAULT_SORT) qs.set('sort', sort);
  return qs.toString();
}
