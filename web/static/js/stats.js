// Backlog statistics computed from the games already in memory. Pure (no DOM): unit-tested.
import { ttbMinutes } from './ttb.js';
import { ttbState } from './ttbscope.js';
import { buildSeries } from './series.js';

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
  const platforms = tally(waiting.flatMap((g) => (g.platforms.length ? g.platforms : ['No platform'])));
  return {
    total: waiting.length,
    byStatus: WAITING.map((key) => ({ key, count: waiting.filter((g) => g.status === key).length })),
    byPlatform: limit ? platforms.slice(0, limit) : platforms,
    morePlatforms: limit ? Math.max(0, platforms.length - limit) : 0,
  };
}

/**
 * Finished games per calendar year, from the year played. A finished game is counted in the latest year it was
 * played. A finished game with no year but a period (childhood / always) goes to `periods`, one with neither to
 * `unknown`; neither is in the bars. Years with nothing are filled in so the bars have no gaps.
 */
export function finishedPerYear(games) {
  const counts = new Map();
  let periods = 0, unknown = 0;
  for (const g of games) {
    if (g.status !== 'finished') continue;
    if (!g.years.length) { if (g.periods.length) periods++; else unknown++; continue; }
    const y = Math.max(...g.years);
    counts.set(y, (counts.get(y) || 0) + 1);
  }
  const bars = [];
  if (counts.size) {
    const ys = [...counts.keys()];
    for (let y = Math.min(...ys); y <= Math.max(...ys); y++) bars.push({ year: y, count: counts.get(y) || 0 });
  }
  return { bars, periods, unknown, total: [...counts.values()].reduce((a, b) => a + b, 0) + periods + unknown };
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

export const IMPORT_MIN_GAMES = 20;
const IMPORT_WINDOW = 5 * 60000;

/**
 * The import batch: a bulk import stamps most of the library within a few seconds, so those games carry no
 * information about how long they have waited. Found as the games updated within 5 minutes of the most common
 * timestamp, when they are at least 20 games and half the library. Returns { ids, date } or null.
 */
export function importBatch(games) {
  const counts = new Map();
  for (const g of games) {
    const t = Date.parse(g.updated_at);
    if (!Number.isNaN(t)) counts.set(t, (counts.get(t) || 0) + 1);
  }
  if (!counts.size) return null;
  const [mode] = [...counts].sort((a, b) => b[1] - a[1] || a[0] - b[0])[0];
  const inBatch = games.filter((g) => Math.abs(Date.parse(g.updated_at) - mode) <= IMPORT_WINDOW);
  if (inBatch.length < IMPORT_MIN_GAMES || inBatch.length * 2 < games.length) return null;
  return { ids: new Set(inBatch.map((g) => g.id)), date: new Date(mode).toISOString().slice(0, 10) };
}

/**
 * Time in status. `updated_at` moves on every edit, so it is "at least this long since the game was last touched",
 * an approximation of how long it has been in its status. Per waiting status: the count, median and longest wait,
 * plus the longest-waiting games. Also how long the backlog games have been in the library (`added_at`).
 * Games still carrying the import's timestamp are left out and counted in `untouched` (see importBatch). `hasHistory`
 * is true when the remaining waiting games were last changed over at least MIN_HISTORY_DAYS days (the spread of
 * their `updated_at`), so one shared old timestamp never counts as history.
 */
export function timeInStatus(games, now = new Date().toISOString(), oldest = 5) {
  const batch = importBatch(games);
  const imported = (g) => !!batch?.ids.has(g.id);
  const waiting = games.filter((g) => WAITING_STATUSES.includes(g.status));
  const rows = WAITING_STATUSES.map((key) => {
    const items = games
      .filter((g) => g.status === key && !imported(g))
      .map((g) => ({ id: g.id, title: g.title, days: daysBetween(g.updated_at, now) }))
      .filter((x) => x.days !== null)
      .sort((a, b) => b.days - a.days || a.title.localeCompare(b.title));
    const sorted = items.map((x) => x.days).sort((a, b) => a - b);
    return { key, count: items.length, median: median(sorted), longest: sorted.length ? sorted[sorted.length - 1] : 0, oldest: items.slice(0, oldest) };
  });
  const ages = games.filter((g) => WAITING.includes(g.status) && !imported(g)).map((g) => daysBetween(g.added_at, now)).filter((d) => d !== null).sort((a, b) => a - b);
  const stamps = waiting.filter((g) => !imported(g)).map((g) => Date.parse(g.updated_at)).filter((t) => !Number.isNaN(t));
  const spread = stamps.length ? (Math.max(...stamps) - Math.min(...stamps)) / DAY : 0;
  return {
    rows, backlogAgeMedian: median(ages), hasHistory: spread >= MIN_HISTORY_DAYS,
    untouched: waiting.filter(imported).length, importDate: batch ? batch.date : null, waiting: waiting.length,
  };
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
  return `${+(d / 365.25).toFixed(1)} years`;
}

/** A round upper bound and evenly spaced whole-number ticks (counts) for a chart axis: niceScale(37) -> {max: 40, ticks: [0, 10, 20, 30, 40]}. */
export function niceScale(maxValue, wanted = 4) {
  if (maxValue <= 0) return { max: 1, ticks: [0, 1] };
  const raw = maxValue / wanted;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const step = Math.max(1, [1, 2, 5, 10].map((k) => k * pow).find((s) => s >= raw));
  const max = Math.ceil(maxValue / step) * step;
  const ticks = [];
  for (let v = 0; v <= max + 1e-9; v += step) ticks.push(Math.round(v));
  return { max, ticks };
}
// ---------------------------------------------------------------------------------------------------------------
// Time estimates and the wider figures of the Stats page. Every hour figure is an estimate: it adds up the stored
// time to beat (ttbMinutes: "Main + extras", else the quickest, else the complete time) of the games that have one,
// and always reports how many games that covered, so the page can say "about 1,240 h across 142 of 189 games".
// ---------------------------------------------------------------------------------------------------------------

export const MIN_GROUP = 5;             // a genre or platform needs this many finished + dropped games to get a finish rate

const hoursOf = (minutes) => Math.round(minutes / 60);

/**
 * Estimated time of a set of games: { minutes, hours (rounded), known (games with a time), total, missing, none }.
 * Only games whose status needs a time (the rule in ttbscope.js, the same one Needs attention lists) are counted, so
 * `total` is those games. Of the ones without a time, `none` are marked "no time on HowLongToBeat" (no time exists)
 * and `missing` are the rest: the ones Needs attention lists to be fixed. Games without a time add nothing.
 */
export function estimate(games) {
  let minutes = 0, known = 0, missing = 0, none = 0, total = 0;
  for (const g of games) {
    const state = ttbState(g);
    if (state === null) continue;
    total++;
    if (state === 'known') { minutes += ttbMinutes(g); known++; } else if (state === 'none') none++; else missing++;
  }
  return { minutes, hours: hoursOf(minutes), known, total, missing, none };
}

/** The year a finished game belongs to: the latest year played, or null (only periods, or nothing recorded). */
export const finishedYear = (g) => (g.years.length ? Math.max(...g.years) : null);

/** Fill the gaps between the smallest and largest key of a Map(int -> row) with make(key). */
function fillGaps(map, make) {
  if (!map.size) return [];
  const ks = [...map.keys()];
  const out = [];
  for (let k = Math.min(...ks); k <= Math.max(...ks); k++) out.push(map.get(k) ?? make(k));
  return out;
}

/**
 * Time spent on finished games. `all` is the estimate for every finished game. `perYear` has one bar per calendar year
 * (gaps filled) with games finished and the estimated hours; a game finished in several years counts once, in its
 * latest year. Finished games with only a period (childhood / always) or nothing recorded have no year: they are in
 * `all` but not in the bars, and are counted in `periods` / `unknown`. `genres` and `platforms` are the groups with the
 * most estimated hours (a game with several genres or platforms counts for each).
 */
export function finishedTime(games, limit = 6) {
  const fin = games.filter((g) => g.status === 'finished');
  const years = new Map();
  let periods = 0, unknown = 0;
  for (const g of fin) {
    const y = finishedYear(g);
    if (y === null) { if (g.periods.length) periods++; else unknown++; continue; }
    if (!years.has(y)) years.set(y, []);
    years.get(y).push(g);
  }
  const perYear = fillGaps(new Map([...years].map(([y, gs]) => [y, { year: y, count: gs.length, ...estimate(gs) }])),
    (year) => ({ year, count: 0, minutes: 0, hours: 0, known: 0, total: 0 }));
  const group = (pick, none) => {
    const m = new Map();
    for (const g of fin) for (const k of (pick(g).length ? pick(g) : [none])) { if (!m.has(k)) m.set(k, []); m.get(k).push(g); }
    return [...m].map(([label, gs]) => ({ label, count: gs.length, ...estimate(gs) }))
      .filter((r) => r.known > 0)
      .sort((a, b) => b.minutes - a.minutes || a.label.localeCompare(b.label))
      .slice(0, limit);
  };
  return { all: estimate(fin), perYear, periods, unknown, genres: group((g) => g.genres, 'No genre'), platforms: group((g) => g.platforms, 'No platform') };
}

/** Time left: the estimate for the games waiting (Up next and Backlog together). */
export const timeLeft = (games) => estimate(games.filter((g) => WAITING.includes(g.status)));

/**
 * Finished against dropped per genre or per platform (`key` = 'genres' | 'platforms'), only for groups with at least
 * `min` finished + dropped games. `rate` is the finished share (0..1). Most games first.
 */
export function outcomesBy(games, key, min = MIN_GROUP) {
  const m = new Map();
  for (const g of games) {
    if (g.status !== 'finished' && g.status !== 'dropped') continue;
    for (const k of new Set(g[key])) {
      const r = m.get(k) ?? { label: k, finished: 0, dropped: 0 };
      r[g.status]++;
      m.set(k, r);
    }
  }
  return [...m.values()].map((r) => ({ ...r, total: r.finished + r.dropped, rate: r.finished / (r.finished + r.dropped) }))
    .filter((r) => r.total >= min)
    .sort((a, b) => b.total - a.total || a.label.localeCompare(b.label));
}

/** The shortest games still to play: Backlog and Up next with a time to beat, shortest first. Returns the games themselves. */
export function shortestWins(games, n = 10) {
  return games.filter((g) => WAITING.includes(g.status) && ttbMinutes(g) !== null)
    .sort((a, b) => ttbMinutes(a) - ttbMinutes(b) || a.title.localeCompare(b.title)).slice(0, n);
}

/** Library composition: ownership, platforms (every game; one on two platforms counts for both) and release decade. */
export function composition(games) {
  const decades = new Map();
  let noYear = 0;
  for (const g of games) {
    if (!g.release_year) { noYear++; continue; }
    const d = Math.floor(g.release_year / 10);
    decades.set(d, (decades.get(d) || 0) + 1);
  }
  return {
    total: games.length,
    ownership: tally(games.map((g) => g.ownership || 'Not set')),
    platforms: tally(games.flatMap((g) => (g.platforms.length ? g.platforms : ['No platform']))),
    decades: fillGaps(new Map([...decades].map(([d, count]) => [d, { label: `${d * 10}s`, count }])), (d) => ({ label: `${d * 10}s`, count: 0 })),
    noYear,
  };
}

/**
 * Year by year. `rows` has, for every calendar year from the first to the last, `played` (games with that year in
 * their years played: a game played in three years counts in each) and `finished` (finished games, counted once in
 * their latest year). `mostActive` is the year with the most games played (the later year on a tie). Games played in
 * a period are counted in `childhood` / `always` (a game with both is in both), never given a year.
 */
export function yearByYear(games) {
  const played = new Map(), fin = new Map();
  for (const g of games) {
    for (const y of new Set(g.years)) played.set(y, (played.get(y) || 0) + 1);
    if (g.status === 'finished') { const y = finishedYear(g); if (y !== null) fin.set(y, (fin.get(y) || 0) + 1); }
  }
  const all = new Set([...played.keys(), ...fin.keys()]);
  const rows = [];
  if (all.size) for (let y = Math.min(...all); y <= Math.max(...all); y++) rows.push({ year: y, played: played.get(y) || 0, finished: fin.get(y) || 0 });
  let mostActive = null;
  for (const r of rows) if (r.played > 0 && (!mostActive || r.played >= mostActive.played)) mostActive = r;
  return {
    rows, mostActive: mostActive && { year: mostActive.year, played: mostActive.played },
    childhood: games.filter((g) => g.periods.includes('childhood')).length,
    always: games.filter((g) => g.periods.includes('always')).length,
  };
}

/**
 * Series of the library with two games or more (a series of one has nothing to complete), from the games in the
 * library only: completed (every game finished), in progress (some played, not all finished), not started.
 */
export function seriesProgress(games) {
  const list = buildSeries(games).filter((s) => s.total >= 2);
  const completed = list.filter((s) => s.finished === s.total);
  const inProgress = list.filter((s) => s.finished < s.total && s.played > 0);
  const closest = inProgress.map((s) => ({ name: s.name, finished: s.finished, total: s.total }))
    .sort((a, b) => (b.finished / b.total) - (a.finished / a.total) || b.total - a.total || a.name.localeCompare(b.name)).slice(0, 3);
  return { total: list.length, completed: completed.length, inProgress: inProgress.length, notStarted: list.length - completed.length - inProgress.length, closest };
}

/**
 * Which section the page is "in" while scrolling: `rects` are [{id, top, bottom}] (viewport coordinates), `line` is the
 * y just under the sticky chips. It is the section whose top has passed the line last and that is still on screen;
 * before the first section it is the first one, and past the end of the last one it stays the last.
 */
export function currentSection(rects, line, atEnd = false) {
  if (!rects.length) return null;
  if (atEnd) return rects[rects.length - 1].id; // the page cannot scroll further: the last section is the one being read
  let best = null;
  for (const r of rects) if (r.top <= line && r.bottom > line && (!best || r.top > best.top)) best = r;
  if (best) return best.id;
  const above = rects.filter((r) => r.bottom <= line);
  if (above.length) return above.reduce((a, r) => (r.bottom > a.bottom ? r : a)).id; // in a gap: the one just passed
  return rects.reduce((a, r) => (r.top < a.top ? r : a)).id;
}
