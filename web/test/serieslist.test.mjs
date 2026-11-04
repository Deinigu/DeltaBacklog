import test from 'node:test';
import assert from 'node:assert/strict';
import { buildSeries, missingIndex } from '../static/js/series.js';
import { isCompleted, progressOf, lastPlayed, seriesPasses, filterSeries, countSeries, emptySeriesFilter, seriesFilterCount, seriesPlatforms, seriesStateFromQuery, seriesStateToQuery } from '../static/js/serieslist.js';

let n = 0;
const g = (title, series, o = {}) => ({ id: ++n, title, series, status: 'backlog', ownership: 'Owned', release_year: 2000, years: [], periods: [], platforms: [], igdb_id: null, ...o });
const fin = (title, series, o = {}) => g(title, series, { status: 'finished', years: [2020], ...o });
const one = (games) => buildSeries(games)[0];
const none = new Map();

test('completed: every game of the series is finished', () => {
  assert.ok(isCompleted(one([fin('A', 'S'), fin('B', 'S')])));
  assert.ok(!isCompleted(one([fin('A', 'S'), g('B', 'S')])));
  assert.ok(isCompleted(one([fin('A', 'S')])));
});

test('completed: a game the owner does not have (ownership Nope) is ignored while unfinished, but a finished one counts', () => {
  assert.ok(isCompleted(one([fin('A', 'S'), g('B', 'S', { ownership: 'Nope' })])));
  assert.ok(isCompleted(one([fin('A', 'S'), g('B', 'S', { ownership: ' nope ' })])));
  assert.ok(!isCompleted(one([fin('A', 'S'), g('B', 'S', { ownership: 'Borrowed' })])));
  assert.ok(!isCompleted(one([g('A', 'S', { ownership: 'Nope' })])), 'no game to have finished');
  assert.ok(isCompleted(one([fin('A', 'S', { ownership: 'Nope' })])));
});

test('completed ignores the games of the series that are not in the library, hidden or not, and applies the family rule', () => {
  const lib = [fin('A', 'S', { igdb_id: 1 }), fin('B', 'S', { igdb_id: 2 })];
  const resp = {
    owned_roots: [100], // a remaster is in the library whose original is entry 99
    series: [{ key: 's', collections: [], missing: [{ igdb_id: 3, title: 'C', parent: 0 }, { igdb_id: 99, title: 'A original', parent: 100 }], hidden: [{ igdb_id: 4, title: 'D', parent: 0 }] }],
  };
  const index = missingIndex(resp, lib);
  const s = one(lib);
  assert.deepEqual(index.get('s').missing.map((m) => m.igdb_id), [3]); // the original is owned through its family: not missing
  assert.ok(isCompleted(s), 'the missing C and the hidden D do not matter');
  assert.equal(progressOf(s), 'completed');
  // "Not in library" is the other filter: it still lists the series
  const f = { ...emptySeriesFilter(), missing: true };
  assert.ok(seriesPasses(f, s, index));
  const owned = missingIndex({ series: [{ key: 's', collections: [], missing: [{ igdb_id: 1, title: 'A', parent: 0 }], hidden: [] }] }, lib);
  assert.ok(!seriesPasses(f, s, owned), 'a game added since is not missing any more');
});

test('progress: completed, in progress (something played) and not started', () => {
  const done = one([fin('A', 'S1')]);
  const part = one([fin('A', 'S2'), g('B', 'S2')]);
  const playing = one([g('A', 'S3', { status: 'playing' }), g('B', 'S3')]);
  const fresh = one([g('A', 'S4'), g('B', 'S4', { status: 'up_next' })]);
  assert.deepEqual([done, part, playing, fresh].map(progressOf), ['completed', 'progress', 'progress', 'notstarted']);
});

test('last played is the latest year played of any game; never played is 0', () => {
  assert.equal(lastPlayed(one([g('A', 'S', { years: [2012, 2019] }), g('B', 'S', { years: [2015] })])), 2019);
  assert.equal(lastPlayed(one([g('A', 'S', { periods: ['childhood'] })])), 0);
});

test('filters combine: search by series name or game title, progress, left, not in library, platform', () => {
  const games = [
    fin('Hades', 'Hades', { platforms: ['PC'] }), fin('Hades II', 'Hades', { platforms: ['PC', 'Switch'] }),
    fin('Doom', 'Doom', { platforms: ['PC'] }), g('Doom Eternal', 'Doom', { platforms: ['PS5'] }),
    g('Zelda', 'Zelda', { platforms: ['Switch'] }),
  ];
  const all = buildSeries(games);
  const names = (f, sort = 'name', index = none) => filterSeries(all, f, sort, index).map((s) => s.name);
  const F = (o) => ({ ...emptySeriesFilter(), ...o });
  assert.deepEqual(names(F({})), ['Doom', 'Hades', 'Zelda']);
  assert.deepEqual(names(F({ q: 'eternal' })), ['Doom'], 'a game title finds its series');
  assert.deepEqual(names(F({ q: 'HADES' })), ['Hades']);
  assert.deepEqual(names(F({ progress: new Set(['completed']) })), ['Hades']);
  assert.deepEqual(names(F({ progress: new Set(['progress', 'notstarted']) })), ['Doom', 'Zelda']);
  assert.deepEqual(names(F({ left: true })), ['Doom', 'Zelda']);
  assert.deepEqual(names(F({ platforms: new Set(['Switch']) })), ['Hades', 'Zelda']);
  assert.deepEqual(names(F({ platforms: new Set(['PS5', 'Switch']), progress: new Set(['notstarted']) })), ['Zelda']);
  const index = missingIndex({ series: [{ key: 'zelda', collections: [], missing: [{ igdb_id: 5, title: 'Z2', parent: 0 }], hidden: [] }] }, games);
  assert.deepEqual(names(F({ missing: true }), 'name', index), ['Zelda']);
  assert.equal(seriesFilterCount(F({ progress: new Set(['completed']), left: true, missing: true, platforms: new Set(['PC', 'PS5']) })), 5);
});

test('sorts: name, % finished, number of games, last played', () => {
  const games = [
    fin('A1', 'Alpha'), g('A2', 'Alpha'), g('A3', 'Alpha'), g('A4', 'Alpha'), // 1 of 4, last played 2020
    fin('B1', 'Beta'), fin('B2', 'Beta', { years: [2024] }), // 2 of 2, last played 2024
    fin('C1', 'Gamma', { years: [2021] }), g('C2', 'Gamma'), g('C3', 'Gamma'), // 1 of 3, last played 2021
    g('D1', 'Delta'), // never played
  ];
  const all = buildSeries(games);
  const order = (sort) => filterSeries(all, emptySeriesFilter(), sort, none).map((s) => s.name);
  assert.deepEqual(order('name'), ['Alpha', 'Beta', 'Delta', 'Gamma']);
  assert.deepEqual(order('finished'), ['Beta', 'Gamma', 'Alpha', 'Delta']);
  assert.deepEqual(order('games'), ['Alpha', 'Gamma', 'Beta', 'Delta']);
  assert.deepEqual(order('played'), ['Beta', 'Gamma', 'Alpha', 'Delta']);
  assert.deepEqual(order('nonsense'), order('name'));
});

test('a chip count is what the filter would show with that chip alone', () => {
  const all = buildSeries([fin('A', 'S1', { platforms: ['PC'] }), g('B', 'S2', { platforms: ['PC'] }), g('C', 'S3', { platforms: ['PS5'] })]);
  const f = { ...emptySeriesFilter(), progress: new Set(['completed']), platforms: new Set(['PS5']) };
  assert.equal(countSeries(all, f, none, 'platforms', 'PC'), 1);
  assert.equal(countSeries(all, f, none, 'progress', 'notstarted'), 1);
  assert.deepEqual(seriesPlatforms(all), [{ value: 'PC', count: 2 }, { value: 'PS5', count: 1 }]);
});

test('the address round-trips and drops what is unknown', () => {
  const f = { ...emptySeriesFilter(), q: 'dark', progress: new Set(['progress', 'completed']), left: true, missing: true, platforms: new Set(['Switch', 'PC']) };
  const qs = seriesStateToQuery({ filter: f, sort: 'finished' });
  assert.equal(qs, 'q=dark&progress=completed&progress=progress&left=1&missing=1&platform=PC&platform=Switch&sort=finished');
  const back = seriesStateFromQuery(qs);
  assert.deepEqual(back.filter, f);
  assert.equal(back.sort, 'finished');
  assert.equal(seriesStateToQuery({ filter: emptySeriesFilter(), sort: 'name' }), '');
  const bad = seriesStateFromQuery('progress=zzz&sort=zzz&left=2');
  assert.equal(bad.filter.progress.size, 0);
  assert.equal(bad.filter.left, false);
  assert.equal(bad.sort, 'name');
});
