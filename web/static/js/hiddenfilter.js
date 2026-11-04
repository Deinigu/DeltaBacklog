// The Hidden page's search and source filter. Pure (no DOM): unit-tested. The groups come from hidden.js (groupHidden).
import { HIDDEN_GROUPS } from './hidden.js';
import { matchesText, params, qOf } from './listfilter.js';

const KINDS = HIDDEN_GROUPS.map((g) => g.kind);

export function emptyHiddenFilter() {
  return { q: '', sources: new Set() };
}

export const hiddenFilterCount = (f) => f.sources.size;

/** Whether a row passes: its title or note (the series, the games it was suggested because of) has every word, and its source is chosen. */
export function hiddenPasses(f, it) {
  if (f.sources.size && !f.sources.has(it.kind)) return false;
  return !f.q || matchesText(f.q, it.title, it.note);
}

/** The groups with only the rows that pass the filter; groups left empty are dropped. */
export function filterHidden(groups, f) {
  return groups.map((g) => ({ ...g, items: g.items.filter((it) => hiddenPasses(f, it)) })).filter((g) => g.items.length);
}

/** How many rows a source chip would show (the search applies, the other sources do not). */
export function countSource(groups, f, kind) {
  return groups.reduce((n, g) => n + g.items.filter((it) => hiddenPasses({ ...f, sources: new Set([kind]) }, it)).length, 0);
}

// ---- the address: #/hidden?q=zelda&source=deals,series ----

/** The page state of an address query: the filter. */
export function hiddenStateFromQuery(search) {
  const qs = params(search);
  const f = emptyHiddenFilter();
  f.q = qOf(qs);
  f.sources = new Set((qs.get('source') || '').split(',').filter((k) => KINDS.includes(k)));
  return { filter: f };
}

/** The address query of a page state; '' for the default page. */
export function hiddenStateToQuery({ filter: f }) {
  const qs = new URLSearchParams();
  if (f.q) qs.set('q', f.q);
  if (f.sources.size) qs.set('source', KINDS.filter((k) => f.sources.has(k)).join(','));
  return qs.toString();
}
