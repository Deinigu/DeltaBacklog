// The Upcoming page's search and filters. Pure (no DOM): unit-tested. The groups come from upcoming.js (buildUpcoming);
// this module keeps the items that pass the filter, in the same groups and order.
import { today } from './upcoming.js';
import { matchesText, override, params, qOf, setOf, appendSet, tally } from './listfilter.js';

/** When a game comes out. A date that covers several months (a quarter, a year) counts for every month it covers. */
export const WHEN = [
  { key: 'month', label: 'This month' },
  { key: 'next3', label: 'Next 3 months' },
  { key: 'year', label: 'This year' },
  { key: 'later', label: 'Later' },
  { key: 'tba', label: 'TBA' },
];
const WHEN_KEYS = WHEN.map((w) => w.key);

export function emptyUpcomingFilter() {
  return { q: '', when: new Set(), series: new Set(), platforms: new Set() };
}

export const upcomingFilterCount = (f) => f.when.size + f.series.size + f.platforms.size;

/** The WHEN keys an item belongs to, from today's calendar date (see today() in upcoming.js). A game already out has none. */
export function whenKeys(item, now = new Date()) {
  const p = item.period;
  if (!p) return new Set(['tba']);
  const t = today(now);
  const out = new Set();
  const first = p.year * 12 + p.month - 1, last = p.year * 12 + p.endMonth - 1;
  const released = p.precision === 'day' ? p.endDay <= t.day : p.endDay < t.day;
  if (released) return out;
  const overlaps = (a, b) => first <= b && last >= a;
  if (overlaps(t.ym, t.ym)) out.add('month');
  if (overlaps(t.ym + 1, t.ym + 3)) out.add('next3');
  if (overlaps(now.getFullYear() * 12, now.getFullYear() * 12 + 11)) out.add('year');
  if (last > t.ym + 3) out.add('later');
  return out;
}

/** Whether an item passes the filter. */
export function upcomingPasses(f, item, now = new Date()) {
  if (f.q && !matchesText(f.q, item.title, item.seriesName)) return false;
  if (f.when.size && ![...whenKeys(item, now)].some((k) => f.when.has(k))) return false;
  if (f.series.size && !f.series.has(item.seriesName)) return false;
  if (f.platforms.size && !item.platforms.some((p) => f.platforms.has(p))) return false;
  return true;
}

/** Every item of a buildUpcoming result: the recently released and the groups. */
export const allItems = (res) => [...res.recent, ...res.groups.flatMap((g) => g.items)];

/**
 * The buildUpcoming result with only the items that pass the filter: groups left empty are dropped, `total` counts the
 * upcoming games shown (not the recently released). A filter on "when" leaves the recently released out (they are out).
 */
export function filterUpcoming(res, f, now = new Date()) {
  const pass = (it) => upcomingPasses(f, it, now);
  const groups = res.groups.map((g) => ({ ...g, items: g.items.filter(pass) })).filter((g) => g.items.length);
  return { groups, recent: res.recent.filter(pass), total: groups.reduce((n, g) => n + g.items.length, 0) };
}

/** How many items a chip would show: the filter with that group set to `value` only. */
export function countUpcoming(res, f, key, value, now = new Date()) {
  const g = override(f, key, value);
  return allItems(res).filter((it) => upcomingPasses(g, it, now)).length;
}

export const seriesOf = (res) => tally(allItems(res), (it) => it.seriesName);
export const platformsOf = (res) => tally(allItems(res), (it) => it.platforms);

// ---- the address: #/upcoming?q=zelda&when=month,next3&series=Zelda&platform=PC ----

/** The page state of an address query: the filter. Anything unknown is dropped. */
export function upcomingStateFromQuery(search) {
  const qs = params(search);
  const f = emptyUpcomingFilter();
  f.q = qOf(qs);
  f.when = new Set((qs.get('when') || '').split(',').filter((k) => WHEN_KEYS.includes(k)));
  f.series = setOf(qs, 'series');
  f.platforms = setOf(qs, 'platform');
  return { filter: f };
}

/** The address query of a page state; '' for the default page. */
export function upcomingStateToQuery({ filter: f }) {
  const qs = new URLSearchParams();
  if (f.q) qs.set('q', f.q);
  if (f.when.size) qs.set('when', WHEN_KEYS.filter((k) => f.when.has(k)).join(','));
  appendSet(qs, 'series', f.series);
  appendSet(qs, 'platform', f.platforms);
  return qs.toString();
}
