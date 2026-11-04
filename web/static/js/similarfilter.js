// The Similar games page's search, filters and sort. Pure (no DOM): unit-tested. The suggestions come from similar.js
// (similarIndex: { items, hidden }, each with sourceGames); this module decides which are shown and in what order.
import { matchesText, override, params, qOf, setOf, idsOf, intIn, appendSet, byName, tally } from './listfilter.js';

export const SORTS = [
  { key: 'sources', label: 'Number of sources' },
  { key: 'year', label: 'Year' },
  { key: 'title', label: 'Title' },
];
export const DEFAULT_SORT = 'sources';
export const MIN_YEAR = 1950, MAX_YEAR = 2100;

export function emptySimilarFilter() {
  return { q: '', genres: new Set(), platforms: new Set(), from: 0, to: 0, because: new Set(), withHidden: false };
}

export const similarFilterCount = (f) => f.genres.size + f.platforms.size + (f.from || f.to ? 1 : 0) + f.because.size + (f.withHidden ? 1 : 0);

/** Every suggestion the page may list: the hidden ones too when the filter asks for them. Hidden ones come last, in `hidden`'s order. */
export const poolOf = (index, f) => (f.withHidden ? [...index.items, ...index.hidden] : index.items);

/** Whether a suggestion passes the filter (hidden ones are not in the pool unless asked for, see poolOf). */
export function similarPasses(f, s) {
  if (f.q && !matchesText(f.q, s.title)) return false;
  if (f.genres.size && !(s.genres ?? []).some((g) => f.genres.has(g))) return false;
  if (f.platforms.size && !(s.platforms ?? []).some((p) => f.platforms.has(p))) return false;
  if (f.from || f.to) {
    if (s.year == null) return false; // no year: it cannot be inside a range
    if (f.from && s.year < f.from) return false;
    if (f.to && s.year > f.to) return false;
  }
  if (f.because.size && !s.sourceGames.some((g) => f.because.has(g.igdb_id))) return false;
  return true;
}

// 'sources' keeps the order similarIndex ranked them in (the sort is stable); the others break ties by that order too.
const COMPARE = {
  sources: () => 0,
  year: (a, b) => (b.year ?? -Infinity) - (a.year ?? -Infinity), // newest first, unknown last
  title: (a, b) => byName(a.title, b.title),
};

/** The suggestions that pass the filter, in the order of `sort`. A hidden suggestion (withHidden) goes after the others. */
export function filterSimilar(index, f, sort) {
  const cmp = COMPARE[sort] || COMPARE.sources;
  const pass = (s) => similarPasses(f, s);
  const shown = index.items.filter(pass).sort(cmp);
  return f.withHidden ? shown.concat(index.hidden.filter(pass).sort(cmp)) : shown;
}

/** How many suggestions a chip would show: the filter with that group set to `value` only. */
export function countSimilar(index, f, key, value) {
  const g = override(f, key, value);
  return poolOf(index, g).filter((s) => similarPasses(g, s)).length;
}

/** The genres, platforms and source games of the pool, the most suggestions first. */
export const genresOf = (pool) => tally(pool, (s) => s.genres ?? []);
export const platformsOf = (pool) => tally(pool, (s) => s.platforms ?? []);
/** The source games ("because of"): [{value (IGDB id), title, count}]. */
export function sourcesOf(pool) {
  const t = tally(pool, (s) => s.sourceGames.map((g) => g.igdb_id));
  const title = new Map();
  for (const s of pool) for (const g of s.sourceGames) title.set(g.igdb_id, g.title);
  return t.map((x) => ({ ...x, title: title.get(x.value) ?? String(x.value) })).sort((a, b) => b.count - a.count || byName(a.title, b.title));
}

// ---- the address: #/similar?q=zelda&genre=RPG&platform=PC&from=2015&to=2024&because=12,40&hidden=1&sort=year ----

/** The page state of an address query: {filter, sort}. Anything unknown or out of range is dropped. */
export function similarStateFromQuery(search) {
  const qs = params(search);
  const f = emptySimilarFilter();
  f.q = qOf(qs);
  f.genres = setOf(qs, 'genre');
  f.platforms = setOf(qs, 'platform');
  f.from = intIn(qs.get('from'), MIN_YEAR, MAX_YEAR);
  f.to = intIn(qs.get('to'), MIN_YEAR, MAX_YEAR);
  f.because = idsOf(qs, 'because');
  f.withHidden = qs.get('hidden') === '1';
  const sort = SORTS.some((s) => s.key === qs.get('sort')) ? qs.get('sort') : DEFAULT_SORT;
  return { filter: f, sort };
}

/** The address query of a page state (only what differs from the default); '' for the default page. */
export function similarStateToQuery({ filter: f, sort = DEFAULT_SORT }) {
  const qs = new URLSearchParams();
  if (f.q) qs.set('q', f.q);
  appendSet(qs, 'genre', f.genres);
  appendSet(qs, 'platform', f.platforms);
  if (f.from) qs.set('from', f.from);
  if (f.to) qs.set('to', f.to);
  if (f.because.size) qs.set('because', [...f.because].sort((a, b) => a - b).join(','));
  if (f.withHidden) qs.set('hidden', '1');
  if (sort !== DEFAULT_SORT) qs.set('sort', sort);
  return qs.toString();
}
