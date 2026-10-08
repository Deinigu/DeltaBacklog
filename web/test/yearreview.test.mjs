import test from 'node:test';
import assert from 'node:assert/strict';
import { groupOf, groupYear, breakdown, yearCounts, neighbours, parseYearPath, tierState, tierOf, unranked, place, toEntries } from '../static/js/yearreview.js';

const g = (id, status, years, extra = {}) => ({ id, title: `G${id}`, status, years, platforms: [], genres: [], ...extra });
const games = [
  g(1, 'finished', [2024, 2025], { platforms: ['PC'], genres: ['RPG', 'Indie'] }),
  g(2, 'dropped', [2025], { platforms: ['PC', 'PS5'], genres: ['RPG'] }),
  g(3, 'playing_evergreen', [2025]),
  g(4, 'backlog', [2025]),
  g(5, 'hold', [2019]),
  g(6, 'finished', []), // childhood only: no calendar year
];

test('status maps to a group', () => {
  assert.equal(groupOf('finished'), 'finished');
  assert.equal(groupOf('dropped'), 'dropped');
  assert.equal(groupOf('playing'), 'playing');
  assert.equal(groupOf('playing_evergreen'), 'playing');
  for (const s of ['backlog', 'hold', 'evergreen', 'up_next', 'not_interested']) assert.equal(groupOf(s), 'played');
});

test('a year groups only the games with a play in it', () => {
  const r = groupYear(games, 2025);
  assert.deepEqual(r.map((x) => [x.key, x.games.map((y) => y.id)]), [['finished', [1]], ['dropped', [2]], ['playing', [3]], ['played', [4]]]);
  assert.ok(groupYear(games, 2010).every((x) => x.games.length === 0));
});

test('breakdown counts each name once per game, biggest first', () => {
  const y = groupYear(games, 2025).flatMap((x) => x.games);
  assert.deepEqual(breakdown(y, (x) => x.platforms), [{ name: 'PC', count: 2 }, { name: 'PS5', count: 1 }]);
  assert.deepEqual(breakdown(y, (x) => [...x.genres, ...x.genres]), [{ name: 'RPG', count: 2 }, { name: 'Indie', count: 1 }]);
});

test('year strip and neighbours', () => {
  const c = yearCounts(games);
  assert.deepEqual(c, [{ year: 2025, count: 4 }, { year: 2024, count: 1 }, { year: 2019, count: 1 }]);
  assert.deepEqual(neighbours(c, 2022, 2026), { prev: 2021, next: 2023 }); // a gap year is reachable
  assert.deepEqual(neighbours(c, 2019, 2026), { prev: null, next: 2020 });
  assert.deepEqual(neighbours(c, 2026, 2026), { prev: 2025, next: null });
  assert.deepEqual(neighbours([], 2026, 2026), { prev: null, next: null });
});

test('year route parsing', () => {
  assert.equal(parseYearPath('years/2024'), 2024);
  assert.equal(parseYearPath('years'), null);
  assert.equal(parseYearPath('years/24'), undefined);
  assert.equal(parseYearPath('years/1800'), undefined);
  assert.equal(parseYearPath('library'), undefined);
});

const tiers = [{ id: 10, name: 'S' }, { id: 11, name: 'A' }];
const pool = [1, 2, 3];

test('state ignores entries outside the pool, unknown tiers and repeats', () => {
  const s = tierState(tiers, [{ game_id: 1, tier_id: 11 }, { game_id: 9, tier_id: 10 }, { game_id: 2, tier_id: 99 }, { game_id: 1, tier_id: 10 }], pool);
  assert.deepEqual(s.rows, { 10: [], 11: [1] });
  assert.deepEqual(unranked(s, pool), [2, 3]);
  assert.equal(tierOf(s, 1), 11);
  assert.equal(tierOf(s, 2), null);
});

test('place moves between tiers, reorders, and unranks without mutating', () => {
  const s0 = tierState(tiers, [{ game_id: 1, tier_id: 10 }, { game_id: 2, tier_id: 10 }], pool);
  const s1 = place(s0, 3, 10, 1);
  assert.deepEqual(s1.rows[10], [1, 3, 2]);
  assert.deepEqual(s0.rows[10], [1, 2]);
  const s2 = place(s1, 1, 11);
  assert.deepEqual(s2.rows, { 10: [3, 2], 11: [1] });
  const s3 = place(s2, 2, 10, 0); // reorder inside a tier
  assert.deepEqual(s3.rows[10], [2, 3]);
  assert.deepEqual(place(s3, 2, 10, 99).rows[10], [3, 2]);
  assert.deepEqual(unranked(place(s3, 3, null), pool), [3]);
});

test('toEntries lists tiers in order, games in row order', () => {
  const s = place(place(place(tierState(tiers, [], pool), 1, 11), 2, 10), 3, 11);
  assert.deepEqual(toEntries(s), [{ game_id: 2, tier_id: 10 }, { game_id: 1, tier_id: 11 }, { game_id: 3, tier_id: 11 }]);
});

test('a game that leaves the pool and returns gets its tier back', async () => {
  const { remember, restored } = await import('../static/js/yearreview.js');
  const tiers = [{ id: 10 }, { id: 11 }];
  const known = new Map();
  let s = tierState(tiers, [{ game_id: 1, tier_id: 10 }, { game_id: 2, tier_id: 11 }], [1, 2, 3]);
  remember(known, s, [1, 2, 3]);
  s = tierState(tiers, toEntries(s), [2, 3]); // game 1 lost the year: remembered, not forgotten
  remember(known, s, [2, 3]);
  assert.equal(known.get(1), 10);
  s = tierState(tiers, [...toEntries(s), ...restored(known, [1])], [1, 2, 3]); // and got it back
  assert.equal(tierOf(s, 1), 10);
  assert.equal(tierOf(s, 2), 11);
  remember(known, place(s, 2, null), [1, 2, 3]);
  assert.ok(!known.has(2)); // moved to Not ranked: nothing to restore
});
