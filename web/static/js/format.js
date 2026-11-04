// One way to write dates and numbers, for every page. The UI text is English, so these are too, whatever language the
// browser is set to (a Spanish phone must not show "1.240 h" on Stats and "€7.49" on Deals). Pure (no DOM): unit-tested.

export const LOCALE = 'en-GB';

/** Newer ICU writes en-GB September as "Sept"; every other short month has three letters, so use "Sep" too. */
export const noSept = (s) => s.replace(/\bSept\b/, 'Sep');

const DAY = { day: 'numeric', month: 'short', year: 'numeric' };

/** "27 Nov 2025" for a date, a timestamp (ms) or an ISO string, in the viewer's time zone; '' when absent or unreadable. */
export function formatDate(value, locale = LOCALE) {
  const t = value ? new Date(value) : null;
  if (!t || Number.isNaN(t.getTime())) return '';
  return noSept(t.toLocaleDateString(locale, DAY));
}

/**
 * "14 Nov 2023" for a calendar day written as YYYY-MM-DD at the start of a string (with or without a time after it).
 * The day is read as written, with no time zone shift: a photo taken at 23:30 stays on its day. '' when unreadable.
 */
export function formatCalendarDate(iso, locale = LOCALE) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso || '');
  if (!m) return '';
  const [y, mo, d] = [+m[1], +m[2], +m[3]];
  const t = new Date(Date.UTC(y, mo - 1, d));
  if (mo < 1 || mo > 12 || t.getUTCMonth() !== mo - 1 || t.getUTCDate() !== d) return '';
  return noSept(t.toLocaleDateString(locale, { ...DAY, timeZone: 'UTC' }));
}

/** "1,240" (grouping commas, whatever the browser's locale). */
export function formatNumber(n, locale = LOCALE) {
  return Number(n).toLocaleString(locale);
}
