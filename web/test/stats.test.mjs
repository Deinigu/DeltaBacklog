import test from 'node:test';
import assert from 'node:assert/strict';
import { tally, backlogSize, finishedPerYear, daysBetween, timeInStatus, isPlayed, topGenres, finishedVsDropped, formatDays, niceScale, importBatch } from '../static/js/stats.js';

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

test('the stats are about the library: Wishlist games are left out', async () => {
  const { libraryGames } = await import('../static/js/stats.js');
  const games = [{ id: 1, status: 'backlog' }, { id: 2, status: 'wishlist' }, { id: 3, status: 'finished' }];
  assert.deepEqual(libraryGames(games).map((x) => x.id), [1, 3]);
});
