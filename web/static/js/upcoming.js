// Upcoming releases: which games are not out yet (or just came out) and how they group by month. Pure (no DOM):
// unit-tested. Dates are IGDB's: unix seconds in UTC plus a format saying how exact they are
// (0 day, 1 month, 2 year, 3-6 quarter). A "2027" release carries a day inside 2027, so it is shown as "2027", never
// as a day. Everything is read in UTC so a release on the 1st stays in its month in every time zone; only "today"
// is the viewer's own calendar date.
import { buildSeries } from './series.js';

const DAY_MS = 86400000;
export const LOCALE = 'en-GB'; // the UI text is English, so dates are too, whatever the browser's language
export const RECENT_DAYS = 60;
export const HORIZON_MONTHS = 18; // a release starting later than this goes under "Later / TBA"

const utcDay = (y, m, d) => Math.floor(Date.UTC(y, m - 1, d) / DAY_MS); // m is 1-12
const lastDay = (y, m) => new Date(Date.UTC(y, m, 0)).getUTCDate();

/**
 * The calendar period a release covers, or null without a date:
 *  { precision: 'day'|'month'|'quarter'|'year', year, month (first month), endMonth, quarter, startDay, endDay }
 * Days are whole days since 1970-01-01 (UTC). A format IGDB adds later is read as an exact day.
 */
export function releasePeriod(date, format) {
  if (date == null || !(date > 0)) return null;
  const t = new Date(date * 1000);
  const y = t.getUTCFullYear(), m = t.getUTCMonth() + 1;
  if (format === 1) return { precision: 'month', year: y, month: m, endMonth: m, quarter: null, startDay: utcDay(y, m, 1), endDay: utcDay(y, m, lastDay(y, m)) };
  if (format === 2) return { precision: 'year', year: y, month: 1, endMonth: 12, quarter: null, startDay: utcDay(y, 1, 1), endDay: utcDay(y, 12, 31) };
  if (format >= 3 && format <= 6) {
    const q = format - 2, first = (q - 1) * 3 + 1, last = q * 3;
    return { precision: 'quarter', year: y, month: first, endMonth: last, quarter: q, startDay: utcDay(y, first, 1), endDay: utcDay(y, last, lastDay(y, last)) };
  }
  const day = Math.floor(date / 86400);
  return { precision: 'day', year: y, month: m, endMonth: m, quarter: null, startDay: day, endDay: day };
}

const monthName = (y, m, locale) => new Intl.DateTimeFormat(locale, { timeZone: 'UTC', month: 'long', year: 'numeric' }).format(new Date(Date.UTC(y, m - 1, 15)));

/** "Mar 4, 2027", "March 2027", "Q3 2027", "2027", or "TBA" without a date. */
export function formatRelease(date, format, locale = LOCALE) {
  const p = releasePeriod(date, format);
  if (!p) return 'TBA';
  switch (p.precision) {
    case 'year': return String(p.year);
    case 'quarter': return `Q${p.quarter} ${p.year}`;
    case 'month': return monthName(p.year, p.month, locale);
    default: return new Intl.DateTimeFormat(locale, { timeZone: 'UTC', day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(date * 1000));
  }
}

/** The viewer's calendar date today: { day (days since 1970-01-01 of that date), ym (year * 12 + month - 1) }. */
export function today(now = new Date()) {
  return { day: utcDay(now.getFullYear(), now.getMonth() + 1, now.getDate()), ym: now.getFullYear() * 12 + now.getMonth() };
}

/** Out already: an exact day up to and including today; a month, quarter or year once it is over. */
export const isReleased = (p, t) => (p.precision === 'day' ? p.endDay <= t.day : p.endDay < t.day);

const startYM = (p) => p.year * 12 + p.month - 1;
const endYM = (p) => p.year * 12 + p.endMonth - 1;

/**
 * One list item from a library game (kind 'owned') or a cached IGDB game the library lacks (kind 'missing').
 * Common fields: id, kind, title, date, format, period, platforms (array), seriesName.
 */
export function ownedItem(g) {
  return { id: `g${g.id}`, kind: 'owned', title: g.title, date: g.release_date ?? null, format: g.release_format ?? null,
    period: releasePeriod(g.release_date, g.release_format), platforms: g.platforms ?? [], seriesName: g.series?.trim() || null, game: g };
}
export function missingItem(m, seriesName) {
  return { id: `i${m.igdb_id}`, kind: 'missing', title: m.title, date: m.date ?? null, format: m.format ?? null,
    period: releasePeriod(m.date, m.format), platforms: m.platforms ? m.platforms.split(',').map((x) => x.trim()).filter(Boolean) : [], seriesName, missing: m };
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const byDate = (a, b) => a.period.endDay - b.period.endDay || a.period.startDay - b.period.startDay || collator.compare(a.title, b.title);

/**
 * The Upcoming page: the Wishlist games of the library plus the games of the user's series (index = missingIndex())
 * that the library lacks, split into
 *  groups: [{ key, label, items }] in time order: months ("This month", "Next month", "March 2027"), a quarter ("Q3 2027")
 *          or a year ("2027") after the months it covers, and last "Later / TBA" (no date, or starting more than
 *          HORIZON_MONTHS away)
 *  recent: items out within the last RECENT_DAYS days, newest first
 * `index` maps a series key to { missing } as missingIndex builds it; hidden games are not in it.
 */
export function buildUpcoming(games, index, now = new Date(), { recentDays = RECENT_DAYS, horizon = HORIZON_MONTHS, locale = LOCALE } = {}) {
  const t = today(now);
  const items = games.filter((g) => g.status === 'wishlist').map(ownedItem);
  const names = new Map(buildSeries(games).map((s) => [s.key, s.name]));
  const seen = new Set();
  for (const [key, extra] of index ?? []) {
    for (const m of extra.missing ?? []) {
      if (seen.has(m.igdb_id)) continue; // in the collections of two series
      seen.add(m.igdb_id);
      items.push(missingItem(m, names.get(key) ?? key));
    }
  }
  const dated = [], later = [], recent = [];
  for (const it of items) {
    if (!it.period) { later.push(it); continue; }
    if (isReleased(it.period, t)) {
      if (t.day - it.period.endDay <= recentDays) recent.push(it);
      continue;
    }
    if (startYM(it.period) - t.ym > horizon) later.push(it);
    else dated.push(it);
  }
  const buckets = new Map();
  for (const it of dated.sort(byDate)) {
    const p = it.period;
    let key, label, order;
    if (p.precision === 'quarter') { key = `q${p.year}-${p.quarter}`; label = `Q${p.quarter} ${p.year}`; order = endYM(p) * 10 + 1; }
    else if (p.precision === 'year') { key = `y${p.year}`; label = String(p.year); order = endYM(p) * 10 + 2; }
    else {
      const ym = startYM(p);
      key = `m${ym}`;
      label = ym === t.ym ? 'This month' : ym === t.ym + 1 ? 'Next month' : monthName(p.year, p.month, locale);
      order = ym * 10;
    }
    if (!buckets.has(key)) buckets.set(key, { key, label, order, items: [] });
    buckets.get(key).items.push(it);
  }
  const groups = [...buckets.values()].sort((a, b) => a.order - b.order).map(({ key, label, items: its }) => ({ key, label, items: its }));
  if (later.length) {
    later.sort((a, b) => (a.period && b.period ? byDate(a, b) : a.period ? -1 : b.period ? 1 : collator.compare(a.title, b.title)));
    groups.push({ key: 'later', label: 'Later / TBA', items: later });
  }
  recent.sort((a, b) => b.period.endDay - a.period.endDay || collator.compare(a.title, b.title));
  return { groups, recent, total: dated.length + later.length };
}
