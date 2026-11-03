// Deals: the prices (from IsThereAnyDeal, cached by the server) of the games you don't own and haven't played yet.
// The server decides which games those are (info.watched, without the ones the owner hid, info.hidden); the client only filters.
// Pure (no DOM): unit-tested. Amounts are cents of the price's own currency; they are only formatted when shown.

export const LOCALE = 'en-GB'; // the UI text is English, so money and dates are too, whatever the browser's language
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

/** "27 Nov 2025" for an ITAD timestamp; '' when absent or invalid. */
export function formatDate(iso, locale = LOCALE) {
  const t = iso ? new Date(iso) : null;
  if (!t || Number.isNaN(t.getTime())) return '';
  return t.toLocaleDateString(locale, { day: 'numeric', month: 'short', year: 'numeric' });
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

/**
 * What the Deals page lists.
 *  games    the library (store.games); info  the server's answer {matches, prices, watched}
 *  statuses a Set of status codes to show (empty or null = all); sort a SORTS key; alertsOnly keeps the games with a standing price alert
 * Returns {rows, unmatched, waiting}: rows are games with a cached price (a row has `p`, the price, and `badge`), unmatched
 * are games ITAD has no match for, waiting are matched games whose prices have not been read yet.
 */
export function buildDeals(games, info, { statuses = null, sort = 'cut', dir = 1, alertsOnly = false } = {}) {
  const prices = new Map((info?.prices || []).map((p) => [p.game_id, p]));
  const matches = new Map((info?.matches || []).map((m) => [m.game_id, m]));
  const alerts = new Map((info?.alerts || []).map((a) => [a.game_id, a]));
  const scope = scopeOf(info);
  const want = new Set(statuses && statuses.size ? statuses : DEAL_STATUSES.map((s) => s.code));
  const rows = [], unmatched = [], waiting = [];
  for (const game of games) {
    if (!scope.has(game.id) || !want.has(game.status) || !DEAL_STATUSES.some((s) => s.code === game.status)) continue;
    const p = prices.get(game.id), m = matches.get(game.id), alert = alerts.get(game.id) || null;
    if (alertsOnly && !(p && alert)) continue;
    if (p) rows.push({ game, p, match: m || null, badge: badgeOf(p), alert });
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
 * the ones that are in the library). Same sorts and badges as the library rows. Returns {rows, unmatched, waiting}: rows have
 * `item`, `p` (the price), `badge` and `alert`; unmatched are games ITAD does not know, waiting are games whose prices have not
 * been read yet. alertsOnly keeps the games with a standing price alert.
 */
export function buildWishlist(info, { sort = 'cut', dir = 1, alertsOnly = false } = {}) {
  const rows = [], unmatched = [], waiting = [];
  for (const item of info?.wishlist?.items || []) {
    const p = item.price, alert = item.alert || null;
    if (alertsOnly && !(p && alert)) continue;
    if (p) rows.push({ item, p, badge: badgeOf(p), alert });
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
