// The Hidden page: everything the owner hid, in one list (GET api/hidden, read from the database). Pure (no DOM): unit-tested.
// Each group is one place where a Hide button exists; unhiding goes through that place's own endpoint (see hiddenview.js).
import { formatDay } from './upcoming.js';

/** The groups in page order. `kind` is also the key of the answer. */
export const HIDDEN_GROUPS = [
  { kind: 'deals', label: 'Deals' },
  { kind: 'similar', label: 'Similar games' },
  { kind: 'series', label: 'Series' },
];

/** "Because you played A and B" for a hidden suggestion (the Similar page's wording, from titles). */
export function becauseTitles(titles, n = 3) {
  const names = (titles ?? []).slice(0, n);
  if (!names.length) return '';
  const more = titles.length - names.length;
  const list = names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
  return `Because you played ${list}${more > 0 ? ` and ${more} more` : ''}`;
}

/** "Hidden on 3 Feb 2026", or '' when the date is missing or unreadable. */
export function hiddenOn(iso, locale = 'en-GB') {
  const t = Date.parse(iso || '');
  return Number.isNaN(t) ? '' : `Hidden on ${formatDay(t, locale)}`;
}

/** One row: { key, kind, id, title, year, cover, v, poster, poster_v (Deals only), note, when, ref } (ref: what the unhide call needs). */
function rows(kind, list) {
  return (list ?? []).map((g) => {
    if (kind === 'deals') return { key: `deals:${g.id}`, kind, id: g.id, title: g.title, year: null, cover: g.cover ?? null, v: g.cover_v ?? '', poster: g.poster ?? null, poster_v: g.poster_v ?? '', note: 'Not shown on Deals', when: hiddenOn(g.hidden_at), ref: { id: g.id } };
    if (kind === 'similar') return { key: `similar:${g.igdb_id}`, kind, id: g.igdb_id, title: g.title, year: g.year ?? null, cover: g.cover ?? null, v: g.v ?? '', note: becauseTitles(g.because), when: hiddenOn(g.hidden_at), ref: { igdb_id: g.igdb_id } };
    return { key: `series:${g.collection_id}:${g.igdb_id}`, kind, id: g.igdb_id, title: g.title, year: g.year ?? null, cover: g.cover ?? null, v: g.v ?? '', note: g.series ? `Series: ${g.series}` : 'Series', when: hiddenOn(g.hidden_at), ref: { collection_id: g.collection_id, igdb_id: g.igdb_id } };
  });
}

/** The answer as [{ kind, label, items }], in HIDDEN_GROUPS order; groups with nothing in them are left out. */
export function groupHidden(resp) {
  return HIDDEN_GROUPS.map((g) => ({ ...g, items: rows(g.kind, resp?.[g.kind]) })).filter((g) => g.items.length);
}

/** How many games are hidden in all. */
export const hiddenCount = (groups) => groups.reduce((n, g) => n + g.items.length, 0);

/** The groups without one row (after Show again, until the page reads the list again). */
export function withoutItem(groups, key) {
  return groups.map((g) => ({ ...g, items: g.items.filter((it) => it.key !== key) })).filter((g) => g.items.length);
}
