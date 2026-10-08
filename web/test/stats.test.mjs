import test from 'node:test';
import assert from 'node:assert/strict';
import { tally, backlogSize, finishedPerYear, daysBetween, timeInStatus, isPlayed, topGenres, finishedVsDropped, formatDays, niceScale } from '../static/js/stats.js';

const g = (o) => ({ id: 1, title: 'T', status: 'backlog', platforms: [], genres: [], years: [], periods: [], added_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...o });

test('tally sorts by count then name and honours the limit', () => {
  assert.deepEqual(tally(['b', 'a', 'b', 'c', 'a', 'b']), [{ label: 'b', count: 3 }, { label: 'a', count: 2 }, { label: 'c', count: 1 }]);
  assert.equal(tally(['a', 'b', 'c'], 2).length, 2);
  assert.deepEqual(tally([]), []);
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

test('finished per year with nothing finished', () => {
  assert.deepEqual(finishedPerYear([g({})]), { bars: [], unknown: 0, total: 0 });
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
});

test('niceScale gives round ticks covering the maximum', () => {
  assert.deepEqual(niceScale(37), { max: 40, ticks: [0, 10, 20, 30, 40] });
  assert.deepEqual(niceScale(7), { max: 8, ticks: [0, 2, 4, 6, 8] });
  assert.deepEqual(niceScale(0), { max: 1, ticks: [0, 1] });
  assert.ok(niceScale(123).max >= 123);
});

test('a fresh import has no history to compare', () => {
  const r = timeInStatus([g({ id: 1 }), g({ id: 2 })], '2026-01-02T00:00:00Z');
  assert.equal(r.hasHistory, false);
});
