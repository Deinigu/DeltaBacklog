import test from 'node:test';
import assert from 'node:assert/strict';
import { similarIndex } from '../static/js/similar.js';
import { emptySimilarFilter, similarFilterCount, poolOf, filterSimilar, countSimilar, genresOf, platformsOf, sourcesOf, similarStateFromQuery, similarStateToQuery } from '../static/js/similarfilter.js';

let n = 0;
const done = (title, igdb, years = []) => ({ id: ++n, title, igdb_id: igdb, status: 'finished', favorite: false, years, periods: [] });
const sugg = (id, title, sources, o = {}) => ({ igdb_id: id, title, year: 2010, cover: null, v: '', parent: 0, date: null, format: null, sources, hidden: false, genres: [], platforms: [], ...o });

const games = [done('Hades', 1, [2022]), done('Celeste', 2, [2020]), done('Doom', 3, [2019])];
const resp = {
  owned_roots: [],
  suggestions: [
    sugg(10, 'Dead Cells', [1, 2], { year: 2018, genres: ['Platform', 'Role-playing (RPG)'], platforms: ['PC', 'Switch'] }),
    sugg(11, 'Hollow Knight', [2], { year: 2017, genres: ['Platform'], platforms: ['PC'] }),
    sugg(12, 'Ultrakill', [3], { year: 2020, genres: ['Shooter'], platforms: ['PC'] }),
    sugg(13, 'Old Sprite', [3], { year: null }),
    sugg(14, 'Hidden Gem', [1], { year: 2015, genres: ['Platform'], platforms: ['Switch'], hidden: true }),
  ],
};
const index = similarIndex(resp, games);
const F = (o) => ({ ...emptySimilarFilter(), ...o });
const titles = (f, sort = 'sources') => filterSimilar(index, f, sort).map((s) => s.title);

test('the default order is the page ranking; hidden suggestions are left out', () => {
  assert.deepEqual(titles(F({})), ['Dead Cells', 'Hollow Knight', 'Ultrakill', 'Old Sprite']);
});

test('search by title', () => {
  assert.deepEqual(titles(F({ q: 'dead' })), ['Dead Cells']);
  assert.deepEqual(titles(F({ q: 'KNIGHT hollow' })), ['Hollow Knight']);
});

test('genre and platform: any of the chosen', () => {
  assert.deepEqual(titles(F({ genres: new Set(['Shooter']) })), ['Ultrakill']);
  assert.deepEqual(titles(F({ genres: new Set(['Shooter', 'Role-playing (RPG)']) })), ['Dead Cells', 'Ultrakill']);
  assert.deepEqual(titles(F({ platforms: new Set(['Switch']) })), ['Dead Cells']);
  assert.deepEqual(titles(F({ genres: new Set(['Platform']), platforms: new Set(['PC']) })), ['Dead Cells', 'Hollow Knight']);
});

test('a suggestion cached before genres were read has none and never passes a genre filter', () => {
  assert.deepEqual(titles(F({ genres: new Set(['Platform']) })), ['Dead Cells', 'Hollow Knight']);
});

test('year range: either end alone or both; a game without a year is outside any range', () => {
  assert.deepEqual(titles(F({ from: 2018 })), ['Dead Cells', 'Ultrakill']);
  assert.deepEqual(titles(F({ to: 2017 })), ['Hollow Knight']);
  assert.deepEqual(titles(F({ from: 2017, to: 2018 })), ['Dead Cells', 'Hollow Knight']);
  assert.deepEqual(titles(F({})).includes('Old Sprite'), true);
  assert.deepEqual(titles(F({ from: 1990 })).includes('Old Sprite'), false);
});

test('because of: suggestions made by any of the chosen source games', () => {
  assert.deepEqual(titles(F({ because: new Set([2]) })), ['Dead Cells', 'Hollow Knight']);
  assert.deepEqual(titles(F({ because: new Set([3]) })), ['Ultrakill', 'Old Sprite']);
  assert.deepEqual(titles(F({ because: new Set([2, 3]) })), ['Dead Cells', 'Hollow Knight', 'Ultrakill', 'Old Sprite']);
});

test('the games you hid stay out unless the filter asks for them, then they come last', () => {
  assert.equal(poolOf(index, F({})).length, 4);
  assert.equal(poolOf(index, F({ withHidden: true })).length, 5);
  assert.deepEqual(titles(F({ withHidden: true, genres: new Set(['Platform']) })), ['Dead Cells', 'Hollow Knight', 'Hidden Gem']);
  assert.deepEqual(titles(F({ withHidden: true, platforms: new Set(['Switch']) }), 'title'), ['Dead Cells', 'Hidden Gem']);
});

test('sorts: year (newest first, unknown last) and title', () => {
  assert.deepEqual(titles(F({}), 'year'), ['Ultrakill', 'Dead Cells', 'Hollow Knight', 'Old Sprite']);
  assert.deepEqual(titles(F({}), 'title'), ['Dead Cells', 'Hollow Knight', 'Old Sprite', 'Ultrakill']);
  assert.deepEqual(titles(F({}), 'nonsense'), titles(F({})));
});

test('chip counts: the filter with that group set to the chip alone', () => {
  const f = F({ genres: new Set(['Shooter']), platforms: new Set(['Switch']) });
  assert.equal(countSimilar(index, f, 'genres', 'Platform'), 1); // platform Switch + genre Platform
  assert.equal(countSimilar(index, f, 'platforms', 'PC'), 1);
  assert.equal(countSimilar(index, F({}), 'because', 2), 2);
  assert.equal(countSimilar(index, F({}), 'withHidden', true), 5);
});

test('options come with their counts; sources have a title', () => {
  const pool = poolOf(index, F({}));
  assert.deepEqual(genresOf(pool), [{ value: 'Platform', count: 2 }, { value: 'Role-playing (RPG)', count: 1 }, { value: 'Shooter', count: 1 }]);
  assert.deepEqual(platformsOf(pool), [{ value: 'PC', count: 3 }, { value: 'Switch', count: 1 }]);
  assert.deepEqual(sourcesOf(pool).map((s) => [s.value, s.title, s.count]), [[2, 'Celeste', 2], [3, 'Doom', 2], [1, 'Hades', 1]]);
});

test('the filter count', () => {
  assert.equal(similarFilterCount(F({})), 0);
  assert.equal(similarFilterCount(F({ q: 'x', genres: new Set(['a', 'b']), from: 2000, to: 2010, because: new Set([1]), withHidden: true })), 5);
});

test('the address round-trips and drops what is unknown', () => {
  const f = F({ q: 'cells', genres: new Set(['RPG', 'Platform']), platforms: new Set(['PC']), from: 2010, to: 2020, because: new Set([7, 3]), withHidden: true });
  const qs = similarStateToQuery({ filter: f, sort: 'year' });
  assert.equal(qs, 'q=cells&genre=Platform&genre=RPG&platform=PC&from=2010&to=2020&because=3%2C7&hidden=1&sort=year');
  const back = similarStateFromQuery(qs);
  assert.deepEqual(back.filter, f);
  assert.equal(back.sort, 'year');
  assert.equal(similarStateToQuery({ filter: F({}), sort: 'sources' }), '');
  const bad = similarStateFromQuery('from=1800&to=abc&because=x,0,5&sort=zzz');
  assert.equal(bad.filter.from, 0);
  assert.equal(bad.filter.to, 0);
  assert.deepEqual([...bad.filter.because], [5]);
  assert.equal(bad.sort, 'sources');
});
