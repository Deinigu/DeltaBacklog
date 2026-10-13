import test from 'node:test';
import assert from 'node:assert/strict';
import { groupOf, groupYear, breakdown, yearCounts, neighbours, parseYearPath, yearRankingName, findYearRanking } from '../static/js/yearreview.js';

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

test('the ranking of a year is found by its name', () => {
  assert.equal(yearRankingName(2025), 'Tier list 2025');
  const list = [{ id: 1, name: 'Best of 2025' }, { id: 2, name: 'Tier list 2025' }, { id: 3, name: 'Tier list 2024' }];
  assert.equal(findYearRanking(list, 2025).id, 2);
  assert.equal(findYearRanking(list, 2023), undefined);
});
