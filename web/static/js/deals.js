// Deals: the prices (from IsThereAnyDeal, cached by the server) of the games you don't own and haven't played yet.
// The server decides which games those are (info.watched, without the ones the owner hid, info.hidden); the client only filters.
// Pure (no DOM): unit-tested. Amounts are cents of the price's own currency; they are only formatted when shown.

import { LOCALE, noSept, formatDate } from './format.js'; // dates and numbers are written the same way on every page
import { norm } from './util.js';
export const NEAR_LOW = 0.10; // "near the historical low": within 10% of it
/** The statuses Deals can cover (a game also has to be unowned and unplayed: the server's rule), in the order of the filter chips. */
export const DEAL_STATUSES = [
  { code: 'backlog', label: 'Backlog' },
  { code: 'up_next', label: 'Up next' },
];
export const SORTS = [
  { key: 'cut', label: 'Biggest discount' },
  { key: 'low', label: 'Closest to the low' },
  { key: 'price', label: 'Lowest price' },
];

/** Every way the list can be ordered: the phone's select shows them all, the desktop table's headers sort by the column ones. */
export const SORT_KEYS = [
  ...SORTS,
  { key: 'name', label: 'Name' },
  { key: 'lowest', label: 'Lowest ever' },
  { key: 'shop', label: 'Shop' },
  { key: 'ends', label: 'Ends soonest' },
  { key: 'added', label: 'Recently added', wish: true },
];
/** The columns of the desktop table, left to right (image and actions have no header sort). `wish`: only the wishlist has it. */
export const COLUMNS = [
  { key: 'name', label: 'Name' },
  { key: 'cut', label: 'Discount' },
  { key: 'price', label: 'Price' },
  { key: 'lowest', label: 'Lowest ever' },
  { key: 'shop', label: 'Shop' },
  { key: 'ends', label: 'Ends' },
  { key: 'added', label: 'Added', wish: true },
];

/** "€7.49". Falls back to "7.49 XYZ" for a currency code the browser does not know. */
export function formatMoney(cents, currency, locale = LOCALE) {
  if (cents == null) return '';
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(cents / 100);
  } catch {
    return `${(cents / 100).toFixed(2)} ${currency}`;
  }
}

/** "16:56" for a time today, "27 Oct" for an earlier day; '' when invalid. Used next to the refresh button ("checked 16:56"). */
export function checkedLabel(iso, now = new Date(), locale = LOCALE) {
  const t = iso ? new Date(iso) : null;
  if (!t || Number.isNaN(t.getTime())) return '';
  if (t.toDateString() === now.toDateString()) return t.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' });
  return noSept(t.toLocaleDateString(locale, { day: 'numeric', month: 'short' })) + ' ' + t.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' });
}

/**
 * Whether a low price means something: the game is on sale now, or its lowest ever was a real discount. A game that never
 * left its full price has price = regular = lowest ever, and calling that a "historical low" would be noise.
 */
const realLow = (p) => (p.cut ?? 0) > 0 || (p.regular != null && p.low_price < p.regular);

/** 'low' when the price is at (or under) the historical low, 'near' within NEAR_LOW above it, else null (also for a game never discounted). */
export function badgeOf(p) {
  if (!p || p.price == null || p.low_price == null || !realLow(p)) return null;
  if (p.price <= p.low_price) return 'low';
  if (p.price <= p.low_price * (1 + NEAR_LOW)) return 'near';
  return null;
}
export const BADGE_LABEL = { low: 'Historical low', near: 'Near low (within 10%)' };

/** What a price alert says (the server's kinds): at the historical low, or at the lowest price of the last two years. */
export const ALERT_LABEL = { historical: 'Historical low', two_year: 'Lowest in 2 years' };
/** The standing alert of a game (the server's info.alerts), or null. */
export const alertOf = (info, gameId) => (info?.alerts || []).find((a) => a.game_id === gameId) || null;
/** How many alerts the owner has not seen yet (the badge on the menu): library games and wishlist games. */
export const unseenCount = (info) => (info?.alerts || []).filter((a) => !a.seen).length
  + (info?.wishlist?.items || []).filter((i) => i.alert && !i.alert.seen).length;
/** Whether the owner turned the alerts of this game off. */
export const isMuted = (info, gameId) => (info?.muted || []).includes(gameId);

/** price / low: 1 is at the low, 1.1 is 10% above it; Infinity without both numbers or when the low is no real discount. */
const lowRatio = (p) => (p.price != null && p.low_price > 0 && realLow(p) ? p.price / p.low_price : Infinity);
const onSale = (p) => p.price != null && (p.cut ?? 0) > 0;

// Each sort puts the games it has nothing to say about last, then by title.
const COMPARE = {
  // Games on sale by cut; then those that are not on sale but sit at or near their low; then the rest by price.
  cut: (a, b) => (onSale(b) - onSale(a)) || (b.cut ?? 0) - (a.cut ?? 0) || lowRatio(a) - lowRatio(b),
  low: (a, b) => lowRatio(a) - lowRatio(b) || (b.cut ?? 0) - (a.cut ?? 0),
  price: (a, b) => (a.price ?? Infinity) - (b.price ?? Infinity),
};

const missing = (v) => v == null || v === '' || (typeof v === 'number' && !Number.isFinite(v));
/** A comparator on a value of the row; rows without a value always go last, whichever way the list is turned. */
const byValue = (get, cmp, dir) => (a, b) => {
  const x = get(a), y = get(b);
  if (missing(x) || missing(y)) return missing(x) - missing(y);
  return dir * cmp(x, y);
};
const num = (x, y) => x - y;
const text = (x, y) => String(x).localeCompare(String(y), 'en');
const when = (x, y) => (x < y ? -1 : x > y ? 1 : 0);

/**
 * The comparator of rows (each with `p`, the price) for a sort key; titleOf reads a row's title. dir 1 is the key's natural order
 * (biggest discount first, cheapest first, A to Z, ends soonest, added most recently), -1 the reverse. Ties fall back to the title.
 */
export function rowSorter(key, dir, titleOf) {
  const d = dir === -1 ? -1 : 1;
  const get = {
    name: [titleOf, text],
    price: [(r) => r.p.price, num],
    lowest: [(r) => r.p.low_price, num],
    shop: [(r) => r.p.shop, text],
    ends: [(r) => (r.p.price != null && r.p.expiry && formatDate(r.p.expiry) ? r.p.expiry : null), when],
    added: [(r) => r.item?.added_at, (x, y) => when(y, x)],
  }[key];
  const legacy = COMPARE[key] || COMPARE.cut;
  const main = get ? byValue(get[0], get[1], d) : (a, b) => d * legacy(a.p, b.p);
  return (a, b) => main(a, b) || text(titleOf(a), titleOf(b));
}

/** The small tags of a row, as [{text, cls}]: the price badge, the standing alert (not twice when both say "historical low"), New. */
export function tagsOf(badge, alert, isNew) {
  const tags = [];
  if (badge) tags.push({ text: BADGE_LABEL[badge], cls: badge === 'low' ? 'low' : 'near' });
  if (alert && !(badge === 'low' && alert.kind === 'historical')) tags.push({ text: ALERT_LABEL[alert.kind] || 'Price alert', cls: 'alert' });
  if (alert && isNew) tags.push({ text: 'New', cls: 'new' });
  return tags;
}

/** "Lowest €7.49 · GOG" for the muted line of a row; 'No price history' without one. */
export function lowLine(p) {
  if (p.low_price == null) return 'No price history';
  return `Lowest ${formatMoney(p.low_price, p.currency)}${p.low_shop ? ` · ${p.low_shop}` : ''}`;
}

// ---------------- the filters ----------------
// The filter state of the page (shared by the Library and Wishlist views; a few filters only make sense for one):
//   q        title search (normalised words, all must be in the title)
//   statuses Set of status codes (library only; empty = all)
//   sale     only games on sale now
//   minCut   minimum discount in percent: 0 (any), 25, 50 or 75
//   maxPrice most the price may be, in whole units of the shop's currency: 0 (any), 5, 10, 20 or 40
//   lows     Set of 'historical' | 'two_year' | 'near': the low types to keep
//   alerts   only games with a standing price alert
//   stores   Set of shop names
//   ends     only deals that end within 3 days
//   added    wishlist only: added in the last 30 days
export const MIN_CUTS = [25, 50, 75];
export const MAX_PRICES = [5, 10, 20, 40];
export const LOW_TYPES = [
  { key: 'historical', label: 'Historical low' },
  { key: 'two_year', label: 'Lowest in 2 years' },
  { key: 'near', label: 'Near low' },
];
export const ENDS_DAYS = 3;
export const ADDED_DAYS = 30;
const DAY = 24 * 3600 * 1000;
const MAX_STORE_FILTER = 30;

export function emptyDealsFilter() {
  return { q: '', statuses: new Set(), sale: false, minCut: 0, maxPrice: 0, lows: new Set(), alerts: false, stores: new Set(), ends: false, added: false };
}

/** The low types a row belongs to: at the historical low (badge or alert), the lowest in 2 years (alert), or near the low (badge). */
export function lowKindsOf(row) {
  const kinds = new Set();
  if (row.badge === 'low' || row.alert?.kind === 'historical') kinds.add('historical');
  if (row.alert?.kind === 'two_year') kinds.add('two_year');
  if (row.badge === 'near') kinds.add('near');
  return kinds;
}

/** Whether the deal ends in the next `days` days (not already over); false without a price or an end date. */
export function endsWithin(p, now, days = ENDS_DAYS) {
  if (!p || p.price == null || !p.expiry) return false;
  const t = Date.parse(p.expiry);
  return Number.isFinite(t) && t >= now.getTime() && t <= now.getTime() + days * DAY;
}

const titleOfRow = (row) => row.game?.title ?? row.item?.title ?? '';
/** Whether a title has every word of the search. */
export const titleMatches = (q, title) => !q || norm(q).split(/\s+/).filter(Boolean).every((w) => norm(title).includes(w));

/**
 * Whether a row (with `p`, `badge`, `alert` and a `game` or an `item`) passes the filters. `skip` ignores one filter (for the counts
 * of its own chips): 'sale' | 'minCut' | 'maxPrice' | 'lows' | 'alerts' | 'stores' | 'ends' | 'added'.
 */
export function rowPasses(f, row, now = new Date(), skip = '') {
  const p = row.p;
  if (!titleMatches(f.q, titleOfRow(row))) return false;
  if (f.statuses.size && row.game && !f.statuses.has(row.game.status)) return false;
  const on = (k) => skip !== k;
  if (on('sale') && f.sale && !(p.price != null && p.cut > 0)) return false;
  if (on('minCut') && f.minCut && !(p.price != null && (p.cut ?? 0) >= f.minCut)) return false;
  if (on('maxPrice') && f.maxPrice && !(p.price != null && p.price <= f.maxPrice * 100)) return false;
  if (on('lows') && f.lows.size && ![...lowKindsOf(row)].some((k) => f.lows.has(k))) return false;
  if (on('alerts') && f.alerts && !row.alert) return false;
  if (on('stores') && f.stores.size && !(p.price != null && f.stores.has(p.shop))) return false;
  if (on('ends') && f.ends && !endsWithin(p, now)) return false;
  if (on('added') && f.added) {
    const t = Date.parse(row.item?.added_at);
    if (!(Number.isFinite(t) && t <= now.getTime() + DAY && t >= now.getTime() - ADDED_DAYS * DAY)) return false;
  }
  return true;
}

/** Whether a filter only a price can pass is on (games without a price are then left out of the other sections too). */
export function priceFilterActive(f, wishlist = false) {
  return f.sale || f.minCut > 0 || f.maxPrice > 0 || f.lows.size > 0 || f.alerts || f.stores.size > 0 || f.ends || (wishlist && f.added);
}

/** How many filters are on (the badge on the Filters button; the search is not one of them). */
export function dealsFilterCount(f, wishlist = false) {
  return (wishlist ? 0 : f.statuses.size) + (f.sale ? 1 : 0) + (f.minCut ? 1 : 0) + (f.maxPrice ? 1 : 0) + f.lows.size + (f.alerts ? 1 : 0) + f.stores.size + (f.ends ? 1 : 0) + (wishlist && f.added ? 1 : 0);
}

/** The shops of the rows, most games first (then by name), with how many rows each has. */
export function storesOf(rows) {
  const tally = new Map();
  for (const r of rows) if (r.p?.price != null && r.p.shop) tally.set(r.p.shop, (tally.get(r.p.shop) || 0) + 1);
  return [...tally].map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count || a.name.localeCompare(b.name, 'en'));
}

// ---- the address: #/deals?view=wishlist&q=zelda&status=backlog&sale=1&min=50&max=10&low=historical,near&alerts=1&store=Steam&ends=1&added=1&sort=price&dir=-1 ----
const LOW_KEYS = new Set(LOW_TYPES.map((l) => l.key));
const STATUS_CODES = new Set(DEAL_STATUSES.map((s) => s.code));
const csv = (v) => (v || '').split(',').map((x) => x.trim()).filter(Boolean);

/** The page state of an address query: {view, filter, sort, dir}. Anything unknown or out of range is dropped. */
export function dealsStateFromQuery(search) {
  const qs = new URLSearchParams(search || '');
  const f = emptyDealsFilter();
  f.q = norm(qs.get('q') || '').trim().slice(0, 100);
  for (const s of csv(qs.get('status'))) if (STATUS_CODES.has(s)) f.statuses.add(s);
  f.sale = qs.get('sale') === '1';
  const min = Number(qs.get('min'));
  if (MIN_CUTS.includes(min)) f.minCut = min;
  const max = Number(qs.get('max'));
  if (MAX_PRICES.includes(max)) f.maxPrice = max;
  for (const l of csv(qs.get('low'))) if (LOW_KEYS.has(l)) f.lows.add(l);
  f.alerts = qs.get('alerts') === '1';
  // A shop name may hold a comma, so every shop is its own store= parameter.
  for (const s of qs.getAll('store')) if (s && s.length <= 60 && f.stores.size < MAX_STORE_FILTER) f.stores.add(s);
  f.ends = qs.get('ends') === '1';
  f.added = qs.get('added') === '1';
  const sort = SORT_KEYS.some((k) => k.key === qs.get('sort')) ? qs.get('sort') : 'cut';
  return { view: qs.get('view') === 'wishlist' ? 'wishlist' : 'library', filter: f, sort, dir: qs.get('dir') === '-1' ? -1 : 1 };
}

/** The address query of a page state (only what differs from the default); '' for the default page. */
export function dealsStateToQuery({ view = 'library', filter: f, sort = 'cut', dir = 1 }) {
  const qs = new URLSearchParams();
  if (view === 'wishlist') qs.set('view', 'wishlist');
  if (f.q) qs.set('q', f.q);
  if (f.statuses.size) qs.set('status', DEAL_STATUSES.map((s) => s.code).filter((c) => f.statuses.has(c)).join(','));
  if (f.sale) qs.set('sale', '1');
  if (f.minCut) qs.set('min', f.minCut);
  if (f.maxPrice) qs.set('max', f.maxPrice);
  if (f.lows.size) qs.set('low', LOW_TYPES.map((l) => l.key).filter((k) => f.lows.has(k)).join(','));
  if (f.alerts) qs.set('alerts', '1');
  for (const s of [...f.stores].sort()) qs.append('store', s);
  if (f.ends) qs.set('ends', '1');
  if (f.added) qs.set('added', '1');
  if (sort !== 'cut') qs.set('sort', sort);
  if (dir === -1) qs.set('dir', '-1');
  return qs.toString();
}

/**
 * What the Deals page lists.
 *  games    the library (store.games); info  the server's answer {matches, prices, watched}
 *  filter   a deals filter (see emptyDealsFilter); statuses (a Set of status codes) and alertsOnly are the older spelling of two of them
 *  sort a SORT_KEYS key; now is the time "ends within" is counted from
 * Returns {rows, unmatched, waiting}: rows are games with a cached price (a row has `p`, the price, and `badge`), unmatched
 * are games ITAD has no match for, waiting are matched games whose prices have not been read yet. A filter only a price can
 * pass leaves the last two out.
 */
export function buildDeals(games, info, { filter = null, statuses = null, sort = 'cut', dir = 1, alertsOnly = false, now = new Date() } = {}) {
  const f = { ...emptyDealsFilter(), ...filter };
  if (statuses && statuses.size) f.statuses = new Set(statuses);
  if (alertsOnly) f.alerts = true;
  f.added = false; // only a wishlist game has an added date
  const prices = new Map((info?.prices || []).map((p) => [p.game_id, p]));
  const matches = new Map((info?.matches || []).map((m) => [m.game_id, m]));
  const alerts = new Map((info?.alerts || []).map((a) => [a.game_id, a]));
  const scope = scopeOf(info);
  const want = new Set(f.statuses.size ? f.statuses : DEAL_STATUSES.map((s) => s.code));
  const priceOnly = priceFilterActive(f);
  const rows = [], unmatched = [], waiting = [];
  for (const game of games) {
    if (!scope.has(game.id) || !want.has(game.status) || !DEAL_STATUSES.some((s) => s.code === game.status)) continue;
    const p = prices.get(game.id), m = matches.get(game.id), alert = alerts.get(game.id) || null;
    if (p) {
      const row = { game, p, match: m || null, badge: badgeOf(p), alert };
      if (rowPasses(f, row, now)) rows.push(row);
    } else if (priceOnly || !titleMatches(f.q, game.title)) continue;
    else if (m && !m.itad_id) unmatched.push({ game, match: m });
    else if (m) waiting.push({ game, match: m });
  }
  const title = (a, b) => a.game.title.localeCompare(b.game.title, 'en');
  rows.sort(rowSorter(sort, dir, (r) => r.game.title));
  unmatched.sort(title);
  waiting.sort(title);
  return { rows, unmatched, waiting };
}

/**
 * What the Wishlist section lists: the Steam wishlist games (info.wishlist.items; the server already left out hidden ones and
 * the ones that are in the library). Same filters, sorts and badges as the library rows. Returns {rows, unmatched, waiting}: rows
 * have `item`, `p` (the price), `badge` and `alert`; unmatched are games ITAD does not know, waiting are games whose prices
 * have not been read yet. `alertsOnly` is the older spelling of the alerts filter.
 */
export function buildWishlist(info, { filter = null, sort = 'cut', dir = 1, alertsOnly = false, now = new Date() } = {}) {
  const f = { ...emptyDealsFilter(), ...filter, statuses: new Set() }; // a wishlist game has no status
  if (alertsOnly) f.alerts = true;
  const priceOnly = priceFilterActive(f, true);
  const rows = [], unmatched = [], waiting = [];
  for (const item of info?.wishlist?.items || []) {
    const p = item.price, alert = item.alert || null;
    if (p) {
      const row = { item, p, badge: badgeOf(p), alert };
      if (rowPasses(f, row, now)) rows.push(row);
    } else if (priceOnly || !titleMatches(f.q, item.title)) continue;
    else if (item.matched && !item.itad_id) unmatched.push({ item });
    else waiting.push({ item });
  }
  const title = (a, b) => a.item.title.localeCompare(b.item.title, 'en');
  rows.sort(rowSorter(sort, dir, (r) => r.item.title));
  unmatched.sort(title);
  waiting.sort(title);
  return { rows, unmatched, waiting };
}

/** The Steam store page of a wishlist game. */
export const steamStoreURL = (appid) => `https://store.steampowered.com/app/${encodeURIComponent(appid)}`;

/** The ids of the games Deals shows (the server's rule, hidden ones left out); empty until the server has answered. */
export const scopeOf = (info) => new Set(info?.watched || []);

/** The hidden games (the owner hid them and they still qualify), by title. */
export function hiddenGames(games, info) {
  const hid = new Set(info?.hidden || []);
  return games.filter((g) => hid.has(g.id)).sort((a, b) => a.title.localeCompare(b.title, 'en'));
}

/** The cached price of one game (for the drawer), or null. */
export const priceOf = (info, gameId) => (info?.prices || []).find((p) => p.game_id === gameId) || null;
export const matchOf = (info, gameId) => (info?.matches || []).find((m) => m.game_id === gameId) || null;

/** The oldest time any shown price was read (what "last checked" can promise), or null. */
export function oldestCheck(info) {
  const times = [...(info?.prices || []), ...(info?.wishlist?.items || []).map((i) => i.price).filter(Boolean)].map((p) => p.fetched_at).filter(Boolean).sort();
  return times[0] || null;
}

/** Whether a game can be on Deals: by status alone (a quick test) or, with the server's answer, by the whole rule and not hidden. */
export const isWatched = (game, info) => DEAL_STATUSES.some((s) => s.code === game.status) && (info == null || scopeOf(info).has(game.id));

/** The shop link of a price, only if it is a real web address (it opens in a new tab). */
export function safeLink(url) {
  try {
    const u = new URL(url);
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : null;
  } catch {
    return null;
  }
}

/** One line for the drawer / a row: "€7.49 at Steam (-50%, was €14.99)". */
export function priceLine(p) {
  if (!p || p.price == null) return p?.low_price != null ? 'No shop lists it right now' : 'No price found';
  const cut = p.cut > 0 ? ` (-${p.cut}%${p.regular > p.price ? `, was ${formatMoney(p.regular, p.currency)}` : ''})` : '';
  return `${formatMoney(p.price, p.currency)}${p.shop ? ` at ${p.shop}` : ''}${cut}`;
}

// ---- preferred stores ----
export const MAX_STORES = 30;
export const DEFAULT_THRESHOLD = 15;

/** "€9.99 at Epic": a clearly cheaper offer at another shop than the preferred one (the server's alt_*); '' without one. */
export function altLine(p) {
  if (!p || p.alt_shop == null || p.alt_price == null || (p.price != null && p.alt_price >= p.price)) return '';
  return `${formatMoney(p.alt_price, p.currency)} at ${p.alt_shop}`;
}

/** The ranking with the shop at index i moved up (delta -1) or down (+1); the same list at an edge. Never changes its input. */
export function moveStore(list, i, delta) {
  const j = i + delta;
  if (i < 0 || i >= list.length || j < 0 || j >= list.length) return list;
  const out = [...list];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** The ranking with a shop added at the end; the same list for a blank name, a shop already in it (case aside) or a full list. */
export function addStore(list, name) {
  const n = String(name ?? '').trim();
  if (!n || n.length > 60 || list.length >= MAX_STORES || list.some((s) => s.toLowerCase() === n.toLowerCase())) return list;
  return [...list, n];
}

/** The ranking without the shop at index i. */
export const removeStore = (list, i) => list.filter((_, k) => k !== i);

/** A threshold from an input: a whole number from 0 to 100, or null when it is not one. */
export function parseThreshold(v) {
  const s = String(v ?? '').trim();
  if (!/^\d{1,3}$/.test(s)) return null;
  const n = Number(s);
  return n <= 100 ? n : null;
}

/** The shop names ITAD lists that are not in the ranking yet, for the add box. */
export const unusedShops = (shops, list) => {
  const used = new Set(list.map((s) => s.toLowerCase()));
  return (shops || []).filter((s) => !used.has(s.toLowerCase()));
};
