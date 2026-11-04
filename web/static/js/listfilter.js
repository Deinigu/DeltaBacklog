// What the list pages (Series, Similar games, Upcoming, Hidden, Rankings) share in their search and filters. Pure (no DOM):
// unit-tested. Each page has its own module for its filters (serieslist.js, similarfilter.js, upcomingfilter.js,
// hiddenfilter.js); they all read and write the page state as a URL query the same way.
import { norm } from './util.js';

export const MAX_QUERY = 100; // the longest search text an address may carry
export const MAX_VALUES = 40; // the most values of one filter an address may carry

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
export const byName = (a, b) => collator.compare(a, b);

/** The words of a search text, without accents and in lower case. */
export const words = (q) => norm(q).split(/\s+/).filter(Boolean);

/** Whether every word of the search is somewhere in the texts (an empty search matches everything). */
export function matchesText(q, ...texts) {
  const w = words(q);
  if (!w.length) return true;
  const hay = norm(texts.filter((t) => t != null && t !== '').join(' '));
  return w.every((x) => hay.includes(x));
}

/** The filter with one key replaced by `value` (a Set key gets a Set of that one value): "what would this chip give?". */
export function override(filter, key, value) {
  return { ...filter, [key]: filter[key] instanceof Set ? new Set([value]) : value };
}

/** Add the value to the Set, or remove it when it is there. */
export function toggleIn(set, value) {
  if (set.has(value)) set.delete(value); else set.add(value);
}

/**
 * The distinct values of `pick(item)` (a value or an array of them) with how many items have each: [{value, count}], the
 * most items first, then by name. Empty values are left out.
 */
export function tally(items, pick) {
  const m = new Map();
  for (const it of items) {
    const v = pick(it);
    for (const x of new Set(Array.isArray(v) ? v : [v])) if (x != null && x !== '') m.set(x, (m.get(x) || 0) + 1);
  }
  return [...m].map(([value, count]) => ({ value, count })).sort((a, b) => b.count - a.count || byName(String(a.value), String(b.value)));
}

/** The options of a filter group: the values that exist, plus the selected ones that do not (a saved address never loses a condition). */
export function withSelected(values, selected) {
  const out = [...values];
  for (const s of selected) if (!out.includes(s)) out.push(s);
  return out;
}

// ---- the address query ----

/** A URLSearchParams from `?a=b`, `a=b` or nothing. */
export const params = (search) => new URLSearchParams(String(search || '').replace(/^\?/, ''));

/** The search text of an address, trimmed and cut to MAX_QUERY. */
export const qOf = (qs) => (qs.get('q') || '').trim().slice(0, MAX_QUERY);

/** The values of a repeated parameter that `allowed` accepts (every value when it is omitted), at most MAX_VALUES. */
export function setOf(qs, name, allowed = null) {
  const out = new Set();
  for (const v of qs.getAll(name)) {
    if (!v || v.length > 80 || out.size >= MAX_VALUES) continue;
    if (!allowed || allowed.includes(v)) out.add(v);
  }
  return out;
}

/** The numbers of a comma separated parameter (positive integers only). */
export function idsOf(qs, name) {
  const out = new Set();
  for (const v of (qs.get(name) || '').split(',')) {
    const n = Number(v);
    if (Number.isInteger(n) && n > 0 && out.size < MAX_VALUES) out.add(n);
  }
  return out;
}

/** A whole number inside [min, max], or 0. */
export function intIn(v, min, max) {
  const n = Number(v);
  return v !== null && v !== '' && Number.isInteger(n) && n >= min && n <= max ? n : 0;
}

/** Sets the repeated parameter for every value, in the order of `order` when given (so the address is the same whatever the click order). */
export function appendSet(qs, name, set, order = null) {
  const values = order ? order.filter((v) => set.has(v)).concat([...set].filter((v) => !order.includes(v)).sort(byName)) : [...set].sort(byName);
  for (const v of values) qs.append(name, v);
}

/** Items whose name (or description) has every word of the search; the whole list for an empty search. */
export const filterByName = (list, q, textsOf) => (words(q).length ? list.filter((it) => matchesText(q, ...textsOf(it))) : list);
