import test from 'node:test';
import assert from 'node:assert/strict';
import { tally, backlogSize, finishedPerYear, daysBetween, timeInStatus, isPlayed, topGenres, finishedVsDropped, formatDays, niceScale, importBatch,
  estimate, finishedYear, finishedTime, cleanHoursPerWeek, monthsToClear, formatDuration, timeLeft, outcomesBy, shortestWins, composition, yearByYear, seriesProgress } from '../static/js/stats.js';

const g = (o) => ({ id: 1, title: 'T', status: 'backlog', platforms: [], genres: [], years: [], periods: [], added_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...o });

test('tally sorts by count then name and honours the limit', () => {
  assert.deepEqual(tally(['b', 'a', 'b', 'c', 'a', 'b']), [{ label: 'b', count: 3 }, { label: 'a', count: 2 }, { label: 'c', count: 1 }]);
  assert.equal(tally(['a', 'b', 'c'], 2).length, 2);
  assert.deepEqual(tally([]), []);
});

test('backlog size says how many platforms the limit hid', () => {
  const gs = ['A', 'B', 'C'].map((p, i) => g({ id: i + 1, platforms: [p] }));
  const r = backlogSize(gs, 2);
  assert.equal(r.byPlatform.length, 2);
  assert.equal(r.morePlatforms, 1);
  assert.equal(backlogSize(gs, 0).morePlatforms, 0);
});

test('backlog size counts backlog and up next, by platform', () => {
  const r = backlogSize([
    g({ status: 'backlog', platforms: ['PC', 'Switch'] }), g({ status: 'up_next', platforms: ['PC'] }),
    g({ status: 'backlog' }), g({ status: 'finished', platforms: ['PC'] }), g({ status: 'hold', platforms: ['PC'] }),
  ]);
  assert.equal(r.total, 3);
  assert.deepEqual(r.byStatus, [{ key: 'backlog', count: 2 }, { key: 'up_next', count: 1 }]);
  assert.deepEqual(r.byPlatform, [{ label: 'PC', count: 2 }, { label: 'No platform', count: 1 }, { label: 'Switch', count: 1 }]);
});

test('finished per year uses the latest year played, fills gaps, counts unknowns', () => {
  const r = finishedPerYear([
    g({ status: 'finished', years: [2019, 2021] }), g({ status: 'finished', years: [2021] }),
    g({ status: 'finished', years: [2023] }), g({ status: 'finished' }), g({ status: 'dropped', years: [2022] }),
  ]);
  assert.deepEqual(r.bars, [{ year: 2021, count: 2 }, { year: 2022, count: 0 }, { year: 2023, count: 1 }]);
  assert.equal(r.unknown, 1);
  assert.equal(r.total, 4);
});

test('finished games with only a period are not "unknown"', () => {
  const r = finishedPerYear([
    g({ status: 'finished', years: [2020] }), g({ status: 'finished', periods: ['childhood'] }),
    g({ status: 'finished', periods: ['always'] }), g({ status: 'finished' }),
  ]);
  assert.equal(r.periods, 2);
  assert.equal(r.unknown, 1);
  assert.equal(r.total, 4);
  assert.deepEqual(r.bars, [{ year: 2020, count: 1 }]);
});

test('finished per year with nothing finished', () => {
  assert.deepEqual(finishedPerYear([g({})]), { bars: [], periods: 0, unknown: 0, total: 0 });
});

test('daysBetween floors, clamps and survives bad dates', () => {
  assert.equal(daysBetween('2026-01-01T00:00:00Z', '2026-01-11T12:00:00Z'), 10);
  assert.equal(daysBetween('2026-02-01T00:00:00Z', '2026-01-01T00:00:00Z'), 0);
  assert.equal(daysBetween('nope', '2026-01-01T00:00:00Z'), null);
});

test('time in status: median, longest and oldest per waiting status', () => {
  const now = '2026-01-31T00:00:00Z';
  const r = timeInStatus([
    g({ id: 1, title: 'A', status: 'backlog', updated_at: '2026-01-01T00:00:00Z' }),
    g({ id: 2, title: 'B', status: 'backlog', updated_at: '2026-01-21T00:00:00Z' }),
    g({ id: 3, title: 'C', status: 'backlog', updated_at: '2026-01-29T00:00:00Z' }),
    g({ id: 4, title: 'D', status: 'finished' }),
  ], now, 2);
  const b = r.rows.find((x) => x.key === 'backlog');
  assert.equal(b.count, 3);
  assert.equal(b.median, 10);
  assert.equal(b.longest, 30);
  assert.deepEqual(b.oldest.map((x) => x.title), ['A', 'B']);
  assert.deepEqual(r.rows.map((x) => x.key), ['backlog', 'up_next', 'hold']);
  assert.equal(r.rows.find((x) => x.key === 'hold').count, 0);
  assert.equal(r.backlogAgeMedian, 30);
  assert.ok(r.hasHistory);
});

test('played: years, periods or a played status', () => {
  assert.ok(isPlayed(g({ years: [2020] })));
  assert.ok(isPlayed(g({ periods: ['childhood'] })));
  assert.ok(isPlayed(g({ status: 'dropped' })));
  assert.ok(!isPlayed(g({ status: 'backlog' })));
  assert.ok(!isPlayed(g({ status: 'not_interested' })));
});

test('top genres only count played games', () => {
  const r = topGenres([
    g({ status: 'finished', genres: ['RPG', 'Indie'] }), g({ status: 'dropped', genres: ['RPG'] }),
    g({ status: 'backlog', genres: ['Puzzle', 'RPG'] }),
  ], 5);
  assert.deepEqual(r, [{ label: 'RPG', count: 2 }, { label: 'Indie', count: 1 }]);
});

test('finished vs dropped', () => {
  assert.deepEqual(finishedVsDropped([g({ status: 'finished' }), g({ status: 'finished' }), g({ status: 'finished' }), g({ status: 'dropped' })]), { finished: 3, dropped: 1, share: 0.75 });
  assert.equal(finishedVsDropped([]).share, 0);
});

test('formatDays', () => {
  assert.equal(formatDays(0), 'today');
  assert.equal(formatDays(1), '1 day');
  assert.equal(formatDays(45), '45 days');
  assert.equal(formatDays(150), '5 months');
  assert.equal(formatDays(900), '2.5 years');
  assert.equal(formatDays(730), '2 years');
});

test('niceScale gives round ticks covering the maximum', () => {
  assert.deepEqual(niceScale(37), { max: 40, ticks: [0, 10, 20, 30, 40] });
  assert.deepEqual(niceScale(7), { max: 8, ticks: [0, 2, 4, 6, 8] });
  assert.deepEqual(niceScale(0), { max: 1, ticks: [0, 1] });
  assert.ok(niceScale(123).max >= 123);
  assert.deepEqual(niceScale(1), { max: 1, ticks: [0, 1] });
  assert.deepEqual(niceScale(2), { max: 2, ticks: [0, 1, 2] });
  assert.deepEqual(niceScale(3), { max: 3, ticks: [0, 1, 2, 3] });
});

test('a fresh import has no history to compare', () => {
  const r = timeInStatus([g({ id: 1 }), g({ id: 2 })], '2026-01-02T00:00:00Z');
  assert.equal(r.hasHistory, false);
});

const many = (n, o) => Array.from({ length: n }, (_, i) => g({ id: 100 + i, ...o(i) }));

test('one shared old timestamp is not history, whatever the age', () => {
  const old = '2026-01-01T00:00:00Z';
  const r = timeInStatus(many(3, () => ({ updated_at: old, added_at: old })), '2026-10-01T00:00:00Z');
  assert.equal(r.hasHistory, false);
  assert.equal(r.untouched, 0);
});

test('the import batch is left out and counted as untouched', () => {
  const stamp = (s) => `2026-01-01T00:00:${String(s).padStart(2, '0')}Z`;
  const games = [
    ...many(30, (i) => ({ id: 100 + i, updated_at: stamp(i % 3), added_at: stamp(i % 3) })), // the import, over a few seconds
    g({ id: 1, title: 'Touched', updated_at: '2026-03-01T00:00:00Z', added_at: '2026-01-01T00:00:00Z' }),
    g({ id: 2, title: 'Touched later', updated_at: '2026-03-20T00:00:00Z', added_at: '2026-01-01T00:00:00Z' }),
  ];
  const batch = importBatch(games);
  assert.equal(batch.ids.size, 30);
  assert.equal(batch.date, '2026-01-01');
  const r = timeInStatus(games, '2026-04-01T00:00:00Z');
  assert.equal(r.untouched, 30);
  assert.equal(r.importDate, '2026-01-01');
  assert.equal(r.rows.find((x) => x.key === 'backlog').count, 2);
  assert.deepEqual(r.rows.find((x) => x.key === 'backlog').oldest.map((x) => x.title), ['Touched', 'Touched later']);
  assert.ok(r.hasHistory); // 19 days between the two touched games
});

test('games changed within a few days after an import are not history yet', () => {
  const games = [
    ...many(30, () => ({ updated_at: '2026-01-01T00:00:00Z', added_at: '2026-01-01T00:00:00Z' })),
    g({ id: 1, updated_at: '2026-01-02T00:00:00Z' }), g({ id: 2, updated_at: '2026-01-04T00:00:00Z' }),
  ];
  assert.equal(timeInStatus(games, '2026-06-01T00:00:00Z').hasHistory, false);
});

test('a small library or a varied one has no import batch', () => {
  assert.equal(importBatch(many(5, () => ({ updated_at: '2026-01-01T00:00:00Z' }))), null);
  assert.equal(importBatch(many(40, (i) => ({ updated_at: new Date(Date.UTC(2026, 0, 1 + i)).toISOString() }))), null);
});

// ---- estimates and the wider figures ----
const t = (o) => g({ ttb_normally: null, ttb_hastily: null, ttb_completely: null, ...o });

test('estimate adds the usual time, falls back to quick then complete, and skips games with no time', () => {
  const r = estimate([t({ ttb_normally: 600 }), t({ ttb_hastily: 300 }), t({ ttb_completely: 900 }), t({}), t({ ttb_normally: 0 })]);
  assert.deepEqual(r, { minutes: 1800, hours: 30, known: 3, total: 5 });
  assert.deepEqual(estimate([]), { minutes: 0, hours: 0, known: 0, total: 0 });
});

test('finished time: a game finished in several years counts once, in its latest; periods and unknowns are apart', () => {
  const r = finishedTime([
    t({ status: 'finished', years: [2019, 2021], ttb_normally: 600, genres: ['RPG'], platforms: ['PC'] }),
    t({ status: 'finished', years: [2021], ttb_normally: 1200, genres: ['RPG', 'Puzzle'], platforms: ['PC'] }),
    t({ status: 'finished', years: [2023], genres: ['RPG'] }),                       // no time: counted, adds no hours
    t({ status: 'finished', periods: ['childhood'], ttb_normally: 600 }),
    t({ status: 'finished', ttb_normally: 600 }),
    t({ status: 'dropped', years: [2022], ttb_normally: 6000 }),
  ]);
  assert.deepEqual(r.all, { minutes: 3000, hours: 50, known: 4, total: 5 });
  assert.deepEqual(r.perYear.map((b) => [b.year, b.count, b.hours, b.known]), [[2021, 2, 30, 2], [2022, 0, 0, 0], [2023, 1, 0, 0]]);
  assert.equal(r.periods, 1);
  assert.equal(r.unknown, 1);
  assert.deepEqual(r.genres.map((x) => [x.label, x.hours, x.known, x.count]), [['RPG', 30, 2, 3], ['No genre', 20, 2, 2], ['Puzzle', 20, 1, 1]]);
  assert.deepEqual(r.platforms.map((x) => [x.label, x.hours]), [['PC', 30], ['No platform', 20]]);
  assert.equal(finishedTime([r.all && t({ status: 'finished', years: [1], ttb_normally: 60, genres: ['A'] }), t({ status: 'finished', years: [1], ttb_normally: 120, genres: ['B'] })], 1).genres.length, 1); // the limit
});

test('finished time with nothing finished is empty, not an error', () => {
  const r = finishedTime([g({ status: 'backlog' })]);
  assert.deepEqual(r.perYear, []);
  assert.equal(r.all.hours, 0);
  assert.equal(finishedYear(g({ years: [2001, 2005] })), 2005);
  assert.equal(finishedYear(g({})), null);
});

test('weekly hours maths: months, rounding of the input, durations', () => {
  assert.equal(cleanHoursPerWeek('abc'), 10);
  assert.equal(cleanHoursPerWeek(0), 10);
  assert.equal(cleanHoursPerWeek(7.6), 8);
  assert.equal(cleanHoursPerWeek(9999), 168);
  assert.ok(Math.abs(monthsToClear(435, 10) - 10) < 0.05);          // 43.5 weeks
  assert.equal(formatDuration(0), '');
  assert.equal(formatDuration(0.5), 'about 2 weeks');
  assert.equal(formatDuration(0.1), 'about 1 week');
  assert.equal(formatDuration(1), 'about 1 month');
  assert.equal(formatDuration(5.4), 'about 5 months');
  assert.equal(formatDuration(30), 'about 2.5 years');
});

test('time left is split between up next and backlog and ignores other statuses', () => {
  const r = timeLeft([t({ status: 'up_next', ttb_normally: 600 }), t({ status: 'backlog', ttb_normally: 1200 }), t({ status: 'backlog' }), t({ status: 'hold', ttb_normally: 6000 })]);
  assert.deepEqual([r.upNext.hours, r.upNext.known, r.upNext.total], [10, 1, 1]);
  assert.deepEqual([r.backlog.hours, r.backlog.known, r.backlog.total], [20, 1, 2]);
  assert.deepEqual([r.both.hours, r.both.known, r.both.total], [30, 2, 3]);
});

test('outcomes need at least five finished + dropped games per group', () => {
  const mk = (status, n, genres, platforms = []) => Array.from({ length: n }, () => g({ status, genres, platforms }));
  const gs = [...mk('finished', 3, ['RPG'], ['PC']), ...mk('dropped', 2, ['RPG'], ['PC']), ...mk('finished', 5, ['Puzzle']), ...mk('backlog', 3, ['Shooter']), ...mk('finished', 4, ['Racing'])];
  const r = outcomesBy(gs, 'genres');
  assert.deepEqual(r.map((x) => x.label), ['Puzzle', 'RPG']);      // Racing has 4 and Shooter none finished or dropped
  assert.equal(r[0].rate, 1);
  assert.equal(r[1].rate, 0.6);
  assert.deepEqual(outcomesBy(gs, 'genres', 6), []);
  assert.deepEqual(outcomesBy(gs, 'platforms').map((x) => x.label), ['PC']);
});

test('shortest wins: waiting games with a time, shortest first, at most n', () => {
  const gs = [t({ id: 1, status: 'backlog', ttb_normally: 900 }), t({ id: 2, status: 'up_next', ttb_hastily: 120 }), t({ id: 3, status: 'backlog' }),
    t({ id: 4, status: 'finished', ttb_normally: 30 }), t({ id: 5, status: 'hold', ttb_normally: 30 }), t({ id: 6, status: 'backlog', ttb_normally: 600 })];
  assert.deepEqual(shortestWins(gs).map((x) => x.id), [2, 6, 1]);
  assert.deepEqual(shortestWins(gs, 2).map((x) => x.id), [2, 6]);
});

test('composition: ownership, platforms, decades with gaps filled and unknown years apart', () => {
  const r = composition([
    g({ ownership: 'Owned', platforms: ['PC', 'Switch'], release_year: 1998 }), g({ ownership: 'Owned', platforms: ['PC'], release_year: 2011 }),
    g({ ownership: null, release_year: 2019 }), g({ ownership: 'Borrowed', release_year: null }),
  ]);
  assert.deepEqual(r.ownership, [{ label: 'Owned', count: 2 }, { label: 'Borrowed', count: 1 }, { label: 'Not set', count: 1 }]);
  assert.deepEqual(r.platforms, [{ label: 'No platform', count: 2 }, { label: 'PC', count: 2 }, { label: 'Switch', count: 1 }]);
  assert.deepEqual(r.decades, [{ label: '1990s', count: 1 }, { label: '2000s', count: 0 }, { label: '2010s', count: 2 }]);
  assert.equal(r.noYear, 1);
});

test('year by year: played counts every year, finished counts once, periods are separate, most active year', () => {
  const r = yearByYear([
    g({ status: 'finished', years: [2019, 2021] }), g({ status: 'playing', years: [2021] }), g({ status: 'finished', years: [2021, 2022] }),
    g({ status: 'finished', periods: ['childhood'] }), g({ status: 'evergreen', years: [2022], periods: ['always', 'childhood'] }), g({}),
  ]);
  assert.deepEqual(r.rows, [{ year: 2019, played: 1, finished: 0 }, { year: 2020, played: 0, finished: 0 }, { year: 2021, played: 3, finished: 1 }, { year: 2022, played: 2, finished: 1 }]);
  assert.deepEqual(r.mostActive, { year: 2021, played: 3 });
  assert.equal(r.childhood, 2);
  assert.equal(r.always, 1);
  assert.deepEqual(yearByYear([g({})]), { rows: [], mostActive: null, childhood: 0, always: 0 });
});

test('series progress: completed, in progress, not started; one-game series ignored', () => {
  const s = (series, status, id) => g({ id, series, status });
  const r = seriesProgress([
    s('A', 'finished', 1), s('A', 'finished', 2),
    s('B', 'finished', 3), s('B', 'backlog', 4), s('B', 'backlog', 5),
    s('C', 'playing', 6), s('C', 'backlog', 7),
    s('D', 'backlog', 8), s('D', 'backlog', 9),
    s('E', 'finished', 10),
  ]);
  assert.deepEqual([r.total, r.completed, r.inProgress, r.notStarted], [4, 1, 2, 1]);
  assert.deepEqual(r.closest.map((x) => x.name), ['B', 'C']);
});
