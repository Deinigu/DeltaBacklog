// Backlog statistics computed from the games already in memory. Pure (no DOM): unit-tested.

export const WAITING = ['backlog', 'up_next'];                  // "the backlog" for the size and platform counts
export const WAITING_STATUSES = ['backlog', 'up_next', 'hold']; // statuses where "time waiting" is meaningful
const PLAYED_STATUSES = new Set(['playing', 'playing_evergreen', 'evergreen', 'finished', 'dropped']);
const DAY = 86400000;
export const MIN_HISTORY_DAYS = 7;

/** Count values, biggest first, ties alphabetical. Returns [{label, count}], at most `limit` (0 = all). */
export function tally(values, limit = 0) {
  const m = new Map();
  for (const v of values) m.set(v, (m.get(v) || 0) + 1);
  const rows = [...m].map(([label, count]) => ({ label, count })).sort((a, b) => b.count - a.count || String(a.label).localeCompare(String(b.label)));
  return limit ? rows.slice(0, limit) : rows;
}

/** Backlog size: how many games wait, and where (a game on two platforms counts for both). */
export function backlogSize(games, limit = 8) {
  const waiting = games.filter((g) => WAITING.includes(g.status));
  return {
    total: waiting.length,
    byStatus: WAITING.map((key) => ({ key, count: waiting.filter((g) => g.status === key).length })),
    byPlatform: tally(waiting.flatMap((g) => (g.platforms.length ? g.platforms : ['No platform'])), limit),
  };
}

/**
 * Finished games per calendar year, from the year played. A finished game is counted in the latest year it was
 * played; those with no year recorded go to `unknown`. Years with nothing are filled in so the bars have no gaps.
 */
export function finishedPerYear(games) {
  const counts = new Map();
  let unknown = 0;
  for (const g of games) {
    if (g.status !== 'finished') continue;
    if (!g.years.length) { unknown++; continue; }
    const y = Math.max(...g.years);
    counts.set(y, (counts.get(y) || 0) + 1);
  }
  const bars = [];
  if (counts.size) {
    const ys = [...counts.keys()];
    for (let y = Math.min(...ys); y <= Math.max(...ys); y++) bars.push({ year: y, count: counts.get(y) || 0 });
  }
  return { bars, unknown, total: [...counts.values()].reduce((a, b) => a + b, 0) + unknown };
}

/** Whole days between two ISO timestamps (never negative); null for a bad date. */
export function daysBetween(from, to) {
  const a = Date.parse(from), b = Date.parse(to);
  if (Number.isNaN(a) || Number.isNaN(b)) return null;
  return Math.max(0, Math.floor((b - a) / DAY));
}

function median(sorted) {
  if (!sorted.length) return 0;
  const m = sorted.length >> 1;
  return sorted.length % 2 ? sorted[m] : Math.round((sorted[m - 1] + sorted[m]) / 2);
}

/**
 * Time in status. `updated_at` moves on every edit, so it is "at least this long since the game was last touched",
 * an approximation of how long it has been in its status. Per waiting status: the count, median and longest wait,
 * plus the longest-waiting games. Also how long the backlog games have been in the library (`added_at`).
 */
export function timeInStatus(games, now = new Date().toISOString(), oldest = 5) {
  const rows = WAITING_STATUSES.map((key) => {
    const items = games
      .filter((g) => g.status === key)
      .map((g) => ({ id: g.id, title: g.title, days: daysBetween(g.updated_at, now) }))
      .filter((x) => x.days !== null)
      .sort((a, b) => b.days - a.days || a.title.localeCompare(b.title));
    const sorted = items.map((x) => x.days).sort((a, b) => a - b);
    return { key, count: items.length, median: median(sorted), longest: sorted.length ? sorted[sorted.length - 1] : 0, oldest: items.slice(0, oldest) };
  });
  const ages = games.filter((g) => WAITING.includes(g.status)).map((g) => daysBetween(g.added_at, now)).filter((d) => d !== null).sort((a, b) => a - b);
  // Right after an import every game carries the same date: then the numbers say nothing yet.
  const hasHistory = Math.max(0, ...rows.map((r) => r.longest), ...ages) >= MIN_HISTORY_DAYS;
  return { rows, backlogAgeMedian: median(ages), hasHistory };
}

/** A game counts as played if it has years/periods recorded or its status says so. */
export const isPlayed = (g) => g.years.length > 0 || g.periods.length > 0 || PLAYED_STATUSES.has(g.status);

/** Most played genres (a game with several genres counts for each). */
export function topGenres(games, limit = 8) {
  return tally(games.filter(isPlayed).flatMap((g) => g.genres), limit);
}

/** Finished against dropped. `share` is the finished fraction of the two (0 when neither exists). */
export function finishedVsDropped(games) {
  const finished = games.filter((g) => g.status === 'finished').length;
  const dropped = games.filter((g) => g.status === 'dropped').length;
  return { finished, dropped, share: finished + dropped ? finished / (finished + dropped) : 0 };
}

/** "3 days", "5 months", "2.4 years": a coarse human duration. */
export function formatDays(d) {
  if (d < 1) return 'today';
  if (d < 60) return `${d} day${d === 1 ? '' : 's'}`;
  if (d < 730) return `${Math.round(d / 30.44)} months`;
  return `${(d / 365.25).toFixed(1)} years`;
}

/** A round upper bound and evenly spaced ticks for a chart axis: niceScale(37) -> {max: 40, ticks: [0, 10, 20, 30, 40]}. */
export function niceScale(maxValue, wanted = 4) {
  if (maxValue <= 0) return { max: 1, ticks: [0, 1] };
  const raw = maxValue / wanted;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((k) => k * pow).find((s) => s >= raw);
  const max = Math.ceil(maxValue / step) * step;
  const ticks = [];
  for (let v = 0; v <= max + 1e-9; v += step) ticks.push(Math.round(v));
  return { max, ticks };
}
