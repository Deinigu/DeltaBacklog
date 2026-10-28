// Time to beat (minutes; from IGDB, HowLongToBeat or typed by the owner): which estimate to use, how to show it, and the library filter.
// Pure (no DOM): unit-tested. Games with no time have no estimate and are never counted as short.

/** The old "Short game" flag: a usual time to beat under this many minutes (10 hours). Saved views migrate it to a range. */
export const SHORT_MINUTES = 600;

const has = (v) => typeof v === 'number' && v > 0;

/** The one number to show and add up: the usual playthrough, else the quick one, else the complete one. null when unknown. */
export function ttbMinutes(g) {
  for (const v of [g.ttb_normally, g.ttb_hastily, g.ttb_completely]) if (has(v)) return v;
  return null;
}

export const isShort = (g) => {
  const m = ttbMinutes(g);
  return m !== null && m < SHORT_MINUTES;
};

/** Which estimate a filter or sort looks at: '' = the one shown everywhere (ttbMinutes), else one of the TTB_KINDS keys. */
export const ttbValue = (g, kind = '') => (kind ? (has(g[kind]) ? g[kind] : null) : ttbMinutes(g));

// ---- the filter: {min, max (minutes; min inclusive, max exclusive, null = open), unknown ('' | 'in' | 'ex'), kind} ----
export const emptyTtbFilter = () => ({ min: null, max: null, unknown: '', kind: '' });
/** False for a range that is empty (max not above min): such a range is never stored. */
export const rangeOk = (min, max) => min === null || max === null || max > min;
export const ttbRangeActive = (f) => f.min !== null || f.max !== null;
export const ttbFilterActive = (f) => ttbRangeActive(f) || f.unknown !== '';

/** Quick ranges, tiling the line: each starts where the one before ends. */
export const TTB_PRESETS = [
  { label: 'Under 5 h', min: null, max: 300 },
  { label: '5-15 h', min: 300, max: 900 },
  { label: '15-40 h', min: 900, max: 2400 },
  { label: 'Over 40 h', min: 2400, max: null },
];

/**
 * A game passes when its time is in the range. Games without a time pass only when "no time known" is included, and
 * never when it is excluded ("in" is an OR with the range, like the values of any other facet).
 */
export function matchesTtb(f, g) {
  if (!ttbFilterActive(f)) return true;
  const m = ttbValue(g, f.kind);
  if (m === null) return f.unknown === 'in' || (f.unknown === '' && !ttbRangeActive(f));
  if (f.unknown === 'in' && !ttbRangeActive(f)) return false;
  return (f.min === null || m >= f.min) && (f.max === null || m < f.max);
}

/** The name of the estimate a filter uses, for pills. */
export const ttbKindLabel = (kind) => TTB_KINDS.find((k) => k.key === kind)?.label ?? 'Time to beat';

/** Pill text for the range, e.g. "5-15 h", "under 10 h", "over 40 h"; "" when open on both sides. */
export function ttbRangeText(f) {
  if (f.min !== null && f.max !== null) return `${formatTime(f.min).replace(' h', '')}-${formatTime(f.max)}`;
  if (f.min !== null) return `over ${formatTime(f.min)}`;
  if (f.max !== null) return `under ${formatTime(f.max)}`;
  return '';
}

/** The three times of a game, in the order they are shown, with the one set of labels used everywhere. */
export const TTB_KINDS = [
  { key: 'ttb_hastily', label: 'Main story' },
  { key: 'ttb_normally', label: 'Main + extras' },
  { key: 'ttb_completely', label: 'Completionist' },
];

/**
 * 45 -> "45 min", 90 -> "1½ h", 450 -> "7½ h", 720 -> "12 h", 2500 -> "41 h". Rounds down (to the half hour below ten
 * hours, to the hour above), so a game under 10 h (isShort) never reads "10 h".
 */
export function formatTime(minutes) {
  if (!has(minutes)) return '';
  if (minutes < 60) return `${Math.round(minutes)} min`;
  const h = minutes / 60;
  if (h >= 10) return `${Math.floor(h)} h`;
  const half = Math.floor(h * 2) / 2;
  return `${Math.floor(half)}${half % 1 ? '½' : ''} h`;
}

/** What the drawer shows: all three times, each {key, label, minutes (or null), text ("" when unknown), share (0..1 of the longest, for the bar)}. */
export function ttbTiles(g) {
  const longest = Math.max(0, ...TTB_KINDS.map((k) => (has(g[k.key]) ? g[k.key] : 0)));
  return TTB_KINDS.map(({ key, label }) => {
    const minutes = has(g[key]) ? g[key] : null;
    return { key, label, minutes, text: formatTime(minutes), share: minutes && longest ? minutes / longest : 0 };
  });
}

export const hasTimes = (g) => TTB_KINDS.some((k) => has(g[k.key]));

/** Where the times come from, for the small note under them. Times without a recorded source are IGDB's (older data). */
export function ttbSourceNote(g) {
  if (!hasTimes(g)) return '';
  return { hltb: 'from HowLongToBeat', manual: 'set by you' }[g.ttb_source] || 'from IGDB';
}

/** Largest time accepted, in minutes (the server's limit). */
export const MAX_MINUTES = 100000;

/**
 * Read what someone typed as a time: hours by default ("7.5", "7,5", "12"), "7h30", "7 h 30 min", "1:30", or minutes
 * ("45m", "90 min"). Returns whole minutes, null for an empty field (clear), or undefined when it cannot be read.
 */
export function parseTime(input) {
  const t = String(input ?? '').trim().toLowerCase().replace(',', '.');
  if (t === '') return null;
  let m;
  let minutes;
  if ((m = t.match(/^(\d+(?:\.\d+)?)$/))) minutes = parseFloat(m[1]) * 60;
  else if ((m = t.match(/^(\d+(?:\.\d+)?)\s*(?:h|hr|hrs|hour|hours)$/))) minutes = parseFloat(m[1]) * 60;
  else if ((m = t.match(/^(\d+)\s*(?:h|hr|hrs|hour|hours|:)\s*(\d+)\s*(?:m|min|mins|minutes)?$/))) minutes = parseInt(m[1], 10) * 60 + parseInt(m[2], 10);
  else if ((m = t.match(/^(\d+(?:\.\d+)?)\s*(?:m|min|mins|minutes)$/))) minutes = parseFloat(m[1]);
  else return undefined;
  minutes = Math.round(minutes);
  return minutes >= 1 && minutes <= MAX_MINUTES ? minutes : undefined;
}

/** The text for an input: whole hours or hours with one decimal ("7.5"), minutes below an hour ("45m"); "" for no time. */
export function timeInputValue(minutes) {
  if (!has(minutes)) return '';
  if (minutes < 60) return `${Math.round(minutes)}m`;
  return String(Math.round((minutes / 60) * 100) / 100);
}

/** The patch that stores one HowLongToBeat candidate ({hastily, normally, completely} minutes, 0 = none). */
export const hltbPatch = (c) => ({
  ttb_hastily: c.hastily || null, ttb_normally: c.normally || null, ttb_completely: c.completely || null, ttb_source: 'hltb',
});

/**
 * Total time of a set of games, for "the backlog is N hours". `known` games have a time, `unknown` have none, so the
 * caller can say how much of the backlog the number covers.
 */
export function totalTime(games) {
  let minutes = 0, known = 0;
  for (const g of games) {
    const m = ttbMinutes(g);
    if (m !== null) { minutes += m; known++; }
  }
  return { minutes, hours: Math.floor(minutes / 60), known, unknown: games.length - known };
}
