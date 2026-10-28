import test from 'node:test';
import assert from 'node:assert/strict';
import { isFinished, isPlayed, isLeft, buildSeries, withLeft, barShares, progressText, seriesPosition, moveHidden } from '../static/js/series.js';

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

// ---- not registered games ----
import { collapseMissing, seriesKey, missingIndex, extraOf, withMissing as seriesWithMissing, shelfItems, fanGames, seriesSummary, oldestFetch } from '../static/js/series.js';

const miss = (igdb_id, title, year, o = {}) => ({ igdb_id, title, year, cover: null, collection_id: 7, ...o });
const answer = (...series) => ({ configured: true, series });

test('the series key is the trimmed lower-case name, like the server', () => {
  assert.equal(seriesKey('  The Legend OF Zelda '), 'the legend of zelda');
});

test('the index drops games that are in the library by IGDB id, and series without a cache are empty', () => {
  const games = [g('A', 'Zed', { igdb_id: 1 }), g('B', 'Zed', { igdb_id: 2 }), g('C', 'Zed')];
  const idx = missingIndex(answer({ key: 'zed', collections: [{ id: 7, name: 'Zed', fetched_at: '2026-02-01T00:00:00Z' }], missing: [miss(2, 'B', 2001), miss(3, 'D', 2005)], hidden: [miss(1, 'A', 1999), miss(9, 'H', 2010)] }), games);
  assert.deepEqual(idx.get('zed').missing.map((m) => m.igdb_id), [3]);
  assert.deepEqual(idx.get('zed').hidden.map((m) => m.igdb_id), [9]);
  const [s] = buildSeries(games);
  assert.equal(extraOf(idx, s).missing.length, 1);
  assert.deepEqual(extraOf(new Map(), s).missing, []);
  assert.deepEqual(missingIndex(null, games).size, 0);
});

test('series with games to add', () => {
  const games = [g('A', 'Zed'), g('B', 'Other')];
  const idx = missingIndex(answer({ key: 'zed', collections: [], missing: [miss(3, 'D', 2005)], hidden: [] }, { key: 'other', collections: [], missing: [], hidden: [miss(4, 'E', 2005)] }), games);
  assert.deepEqual(seriesWithMissing(buildSeries(games), idx).map((s) => s.name), ['Zed']); // a hidden game does not count
});

test('owned and missing games share one release order; unknown years last, owned first on a tie', () => {
  const [s] = buildSeries([g('Two', 'X', { release_year: 2005 }), g('One', 'X', { release_year: 1998 }), g('Tail', 'X')]);
  const items = shelfItems(s, { missing: [miss(1, 'Zero', 1995), miss(2, 'Two', 2005), miss(3, 'Lost', null), miss(4, 'Mid', 2001)], hidden: [], collections: [] });
  assert.deepEqual(items.map((i) => `${i.kind[0]}:${i.title}`), ['m:Zero', 'o:One', 'm:Mid', 'o:Two', 'm:Two', 'm:Lost', 'o:Tail']);
});

test('the fan takes the first games that have a picture', () => {
  const [s] = buildSeries([1, 2, 3, 4, 5, 6].map((y) => g('G' + y, 'X', { release_year: 2000 + y, cover: y === 2 ? null : 'c.jpg' })));
  assert.deepEqual(fanGames(s, (x) => !!x.cover).map((x) => x.title), ['G1', 'G3', 'G4', 'G5']);
  assert.equal(fanGames(s, () => false).length, 4); // no picture at all: placeholders
  assert.equal(fanGames(s, () => true, 2).length, 2);
});

test('the summary names the games left and the games not in the library', () => {
  const [s] = buildSeries([g('A', 'X', { status: 'finished' }), g('B', 'X')]);
  assert.equal(seriesSummary(s, { missing: [], hidden: [], collections: [] }), '1 finished · 1 played · 2 games · 1 left to play');
  assert.equal(seriesSummary(s, { missing: [miss(1, 'C', 2000), miss(2, 'D', 2001)], hidden: [], collections: [] }), '1 finished · 1 played · 2 games · 1 left to play · 2 not in your library');
});

test('the oldest fetch time of the cached collections', () => {
  const idx = missingIndex(answer({ key: 'a', collections: [{ id: 1, name: 'A', fetched_at: '2026-03-01T00:00:00Z' }, { id: 2, name: 'B', fetched_at: '2026-01-01T00:00:00Z' }], missing: [], hidden: [] }), []);
  assert.equal(oldestFetch(idx), '2026-01-01T00:00:00Z');
  assert.equal(oldestFetch(new Map()), null);
});

test('a game counts as owned with its IGDB family: entry, parent, remaster, shared parent', () => {
  // the owned DS entry 1426 has the JP original 221280 as parent; the 2013 remaster 221285 has 1426 as parent
  const games = [g('Phoenix Wright', 'Ace', { igdb_id: 1426 })];
  const resp = {
    owned_roots: [221280],
    series: [{ key: 'ace', collections: [], hidden: [], missing: [
      miss(221280, 'Gyakuten Saiban', 2001), // the parent of an owned game
      miss(221285, 'Phoenix Wright', 2013, { parent: 1426 }), // a version of an owned game
      miss(230001, 'Sibling', 2013, { parent: 221280 }), // shares the parent of an owned game
      miss(222000, 'Dual Destinies', 2013), // really missing
    ] }],
  };
  assert.deepEqual(missingIndex(resp, games).get('ace').missing.map((m) => m.title), ['Dual Destinies']);
  // a game added since the answer was made (no owned_roots yet) still hides its remaster and itself
  const added = [g('Justice for All', 'Ace', { igdb_id: 1427 })];
  const resp2 = { series: [{ key: 'ace', collections: [], hidden: [], missing: [miss(1427, 'JfA', 2002), miss(221286, 'JfA HD', 2013, { parent: 1427 }), miss(5, 'Other', 2010)] }] };
  assert.deepEqual(missingIndex(resp2, added).get('ace').missing.map((m) => m.igdb_id), [5]);
});

test('the poster URL is relative and versioned by the cover image', async () => {
  const { missingPosterURL } = await import('../static/js/api.js');
  assert.equal(missingPosterURL('i7.jpg', 'co1x'), 'covers/i7.jpg?v=co1x');
  assert.equal(missingPosterURL(null, 'x'), null);
});

test('a Wishlist game is not a game of the series: no total, played or left; shown on the shelf as wished for', () => {
  const games = [g('Part 1', 'W', { status: 'finished', release_year: 2010 }), g('Part 2', 'W', { status: 'wishlist', release_year: 2026 }), g('Only wished', 'Z', { status: 'wishlist' })];
  const all = buildSeries(games);
  assert.deepEqual(all.map((s) => s.key), ['w']); // a series of wishlist games only is not in the library
  const s = all[0];
  assert.deepEqual([s.total, s.finished, s.played, s.left, s.noYear], [1, 1, 1, 0, 0]);
  assert.deepEqual(s.wishlist.map((x) => x.title), ['Part 2']);
  assert.equal(withLeft(all).length, 0); // nothing left to play: the wished game is not playable yet
  assert.deepEqual(shelfItems(s, { missing: [] }).map((i) => [i.kind, i.title]), [['owned', 'Part 1'], ['wishlist', 'Part 2']]);
  assert.match(seriesSummary(s, { missing: [] }), /1 games? · 1 on your wishlist/);
  assert.equal(seriesPosition(games[1], games), null);
});

test('moveHidden keeps a hidden game hidden when the index is rebuilt from the answer', () => {
  const m = { igdb_id: 5, collection_id: 7, title: 'Zed II', year: 2005, parent: 0 };
  const resp = { owned_roots: [], series: [{ key: 'zeds', collections: [], missing: [m], hidden: [] }] };
  const games = [g('Zed', 'Zeds')];
  moveHidden(resp, m, 'hidden');
  let idx = missingIndex(resp, games);
  assert.deepEqual([idx.get('zeds').missing.length, idx.get('zeds').hidden.length], [0, 1]);
  moveHidden(resp, m, 'missing');
  idx = missingIndex(resp, games);
  assert.deepEqual([idx.get('zeds').missing.length, idx.get('zeds').hidden.length], [1, 0]);
});

test('a big series keeps its own games and the first missing ones, in order', () => {
  const items = [{ kind: 'owned' }, { kind: 'missing', n: 1 }, { kind: 'wishlist' }, { kind: 'missing', n: 2 }, { kind: 'missing', n: 3 }, { kind: 'owned' }];
  const c = collapseMissing(items, 2);
  assert.deepEqual(c.items.map((x) => x.n ?? x.kind), ['owned', 1, 'wishlist', 2, 'owned']);
  assert.equal(c.hidden, 1);
  assert.equal(collapseMissing(items, 3).hidden, 0);
});
