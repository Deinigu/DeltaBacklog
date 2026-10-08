import test from 'node:test';
import assert from 'node:assert/strict';
import { isFinished, isPlayed, isLeft, buildSeries, withLeft, barShares, progressText, seriesPosition } from '../static/js/series.js';

let n = 0;
const g = (title, series, o = {}) => ({ id: ++n, title, series, status: 'backlog', release_year: null, years: [], periods: [], ...o });

test('finished means the Finished status', () => {
  assert.ok(isFinished(g('a', 'S', { status: 'finished' })));
  assert.ok(!isFinished(g('a', 'S', { status: 'dropped', years: [2020] })));
});

test('played: finished, started statuses, or any year or period', () => {
  for (const status of ['finished', 'playing', 'playing_evergreen', 'evergreen', 'hold', 'dropped']) assert.ok(isPlayed(g('a', 'S', { status })), status);
  assert.ok(isPlayed(g('a', 'S', { years: [2019] })));
  assert.ok(isPlayed(g('a', 'S', { periods: ['childhood'] })));
  for (const status of ['backlog', 'up_next', 'not_interested']) assert.ok(!isPlayed(g('a', 'S', { status })), status);
});

test('left to play: unplayed and not Not Interested', () => {
  assert.ok(isLeft(g('a', 'S', { status: 'up_next' })));
  assert.ok(!isLeft(g('a', 'S', { status: 'not_interested' })));
  assert.ok(!isLeft(g('a', 'S', { status: 'backlog', years: [2020] })));
});

test('games are ordered by release year, unknown last, then title naturally', () => {
  const s = buildSeries([g('Part 10', 'X', { release_year: 2010 }), g('Part 2', 'X', { release_year: 2010 }), g('Zed', 'X'), g('Old', 'X', { release_year: 1999 })])[0];
  assert.deepEqual(s.games.map((x) => x.title), ['Old', 'Part 2', 'Part 10', 'Zed']);
});

test('counts, case-insensitive grouping, and games without a series are ignored', () => {
  const list = buildSeries([
    g('A', 'Zelda', { status: 'finished' }), g('B', 'zelda ', { status: 'playing' }), g('C', 'Zelda'), g('D', 'Zelda', { status: 'not_interested' }),
    g('E', 'Alpha'), g('F', null), g('G', ''),
  ]);
  assert.deepEqual(list.map((s) => s.name), ['Alpha', 'Zelda']);
  const z = list[1];
  assert.deepEqual([z.total, z.finished, z.played, z.left], [4, 1, 2, 1]);
});

test('withLeft keeps only series with something left to play', () => {
  const list = buildSeries([g('A', 'Done', { status: 'finished' }), g('B', 'Open', { status: 'finished' }), g('C', 'Open')]);
  assert.deepEqual(withLeft(list).map((s) => s.name), ['Open']);
});

test('bar shares add up to 100 and text is readable', () => {
  const s = buildSeries([g('A', 'S', { status: 'finished' }), g('B', 'S', { status: 'dropped' }), g('C', 'S'), g('D', 'S')])[0];
  assert.deepEqual(barShares(s), { finished: 25, played: 25, rest: 50 });
  assert.equal(progressText(s), '1 finished · 2 played · 4 games');
});

test('series with a game without a release year have no position, only a count', () => {
  const a = g('One', 'S', { release_year: 2000 }), b = g('Two', 'S'), c = g('Three', 'S', { release_year: 2010 });
  const all = [a, b, c];
  assert.equal(buildSeries(all)[0].noYear, 1);
  assert.deepEqual(seriesPosition(a, all), { position: null, total: 3, name: 'S' });
  assert.deepEqual(seriesPosition(b, all), { position: null, total: 3, name: 'S' });
});

test('ties on year and title fall back to the id; bar rest; empty withLeft', () => {
  const x = g('Same', 'S', { release_year: 2000 }), y = g('Same', 'S', { release_year: 2000 });
  assert.deepEqual(buildSeries([y, x])[0].games.map((q) => q.id), [x.id, y.id]);
  const s = buildSeries([g('A', 'S', { status: 'finished' }), g('B', 'S')])[0];
  assert.equal(barShares(s).rest, 50);
  assert.deepEqual(withLeft(buildSeries([g('A', 'S', { status: 'finished' })])), []);
});

test('seriesPosition follows release order and hides single-game series', () => {
  const a = g('One', 'S', { release_year: 2000 }), b = g('Two', 'S', { release_year: 2005 }), c = g('Three', 'S', { release_year: 2010 }), solo = g('Solo', 'T');
  const all = [c, a, b, solo];
  assert.deepEqual(seriesPosition(c, all), { position: 3, total: 3, name: 'S' });
  assert.equal(seriesPosition(solo, all), null);
  assert.equal(seriesPosition(g('x', null), all), null);
});
