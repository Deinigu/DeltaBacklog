// Deals: the prices (from IsThereAnyDeal, cached by the server) of the games on the Wishlist, in the Backlog and Up next.
// Pure (no DOM): unit-tested. Amounts are cents of the price's own currency; they are only formatted when shown.

export const LOCALE = 'en-GB'; // the UI text is English, so money and dates are too, whatever the browser's language
export const NEAR_LOW = 0.10; // "near the historical low": within 10% of it
/** The statuses that get prices, in the order of the filter chips. */
export const DEAL_STATUSES = [
  { code: 'wishlist', label: 'Wishlist' },
  { code: 'backlog', label: 'Backlog' },
  { code: 'up_next', label: 'Up next' },
];
export const SORTS = [
  { key: 'cut', label: 'Biggest discount' },
  { key: 'low', label: 'Closest to the low' },
  { key: 'price', label: 'Lowest price' },
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

/**
 * What the Deals page lists.
 *  games    the library (store.games); info  the server's answer {matches, prices}
 *  statuses a Set of status codes to show (empty or null = all three); sort a SORTS key
 * Returns {rows, unmatched, waiting}: rows are games with a cached price (a row has `p`, the price, and `badge`), unmatched
 * are games ITAD has no match for, waiting are matched games whose prices have not been read yet.
 */
export function buildDeals(games, info, { statuses = null, sort = 'cut' } = {}) {
  const prices = new Map((info?.prices || []).map((p) => [p.game_id, p]));
  const matches = new Map((info?.matches || []).map((m) => [m.game_id, m]));
  const want = new Set(statuses && statuses.size ? statuses : DEAL_STATUSES.map((s) => s.code));
  const rows = [], unmatched = [], waiting = [];
  for (const game of games) {
    if (!want.has(game.status) || !DEAL_STATUSES.some((s) => s.code === game.status)) continue;
    const p = prices.get(game.id), m = matches.get(game.id);
    if (p) rows.push({ game, p, match: m || null, badge: badgeOf(p) });
    else if (m && !m.itad_id) unmatched.push({ game, match: m });
    else if (m) waiting.push({ game, match: m });
  }
  const cmp = COMPARE[sort] || COMPARE.cut;
  const title = (a, b) => a.game.title.localeCompare(b.game.title, 'en');
  rows.sort((a, b) => cmp(a.p, b.p) || title(a, b));
  unmatched.sort(title);
  waiting.sort(title);
  return { rows, unmatched, waiting };
}

/** The cached price of one game (for the drawer), or null. */
export const priceOf = (info, gameId) => (info?.prices || []).find((p) => p.game_id === gameId) || null;
export const matchOf = (info, gameId) => (info?.matches || []).find((m) => m.game_id === gameId) || null;

/** The oldest time any shown price was read (what "last checked" can promise), or null. */
export function oldestCheck(info) {
  const times = (info?.prices || []).map((p) => p.fetched_at).filter(Boolean).sort();
  return times[0] || null;
}

/** A game the deals cover: Wishlist, Backlog or Up next. */
export const isWatched = (game) => DEAL_STATUSES.some((s) => s.code === game.status);

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
