import test from 'node:test';
import assert from 'node:assert/strict';
import { problemsOf, buildGroups, posterState, totalOpen, ignoredTotal, TTB_STATUSES } from '../static/js/attention.js';

const ok = (o = {}) => ({ id: 1, title: 'A', status: 'finished', cover: 'c.jpg', poster: 'p.jpg', poster_src: 'igdb', poster_user: false, igdb_id: 5, igdb_not_found: false,
  platforms: ['PC'], genres: ['RPG'], release_year: 2020, ownership: 'Owned', ttb_normally: 600, ...o });
const keys = (g, u) => problemsOf(g, u).map(([k]) => k);

test('a complete game has no problems', () => assert.deepEqual(keys(ok()), []));

test('unlinked, uncertain and not-on-IGDB', () => {
  assert.deepEqual(keys(ok({ igdb_id: null })), ['unlinked']);
  assert.deepEqual(keys(ok({ igdb_id: null }), new Set([1])), ['uncertain']);
  assert.deepEqual(keys(ok({ igdb_id: null, igdb_not_found: true })), []);
});

test('poster: none, crop, a crop the owner chose is fine', () => {
  assert.equal(posterState(ok({ poster: null })), 'none');
  assert.equal(posterState(ok({ poster_src: 'crop' })), 'crop');
  assert.equal(posterState(ok({ poster_src: 'crop', poster_user: true })), null);
  assert.deepEqual(problemsOf(ok({ poster_src: 'crop' })), [['poster', 'Only a crop of the cover']]);
});

test('time to beat only matters for games about to be played', () => {
  assert.deepEqual([...TTB_STATUSES].sort(), ['backlog', 'playing', 'up_next']);
  assert.deepEqual(keys(ok({ status: 'backlog', ttb_normally: null })), ['no_ttb']);
  assert.deepEqual(keys(ok({ status: 'finished', ttb_normally: null })), []);
  assert.deepEqual(keys(ok({ status: 'backlog', ttb_normally: null, ttb_hastily: 90 })), []);
});

test('missing fields', () => {
  assert.deepEqual(keys(ok({ cover: null, platforms: [], release_year: null, ownership: null, genres: [] })),
    ['no_cover', 'no_platform', 'no_year', 'no_ownership', 'no_genres']);
  assert.deepEqual(keys(ok({ ownership: null, status: 'wishlist' })), []);
});

test('groups: ordered, sorted by title, empty ones left out, ignore per group', () => {
  const games = [ok({ id: 2, title: 'beta', cover: null }), ok({ id: 1, title: 'Alpha', cover: null, release_year: null }), ok({ id: 3 })];
  let g = buildGroups(games);
  assert.deepEqual(g.map((x) => x.key), ['no_cover', 'no_year']);
  assert.deepEqual(g[0].rows.map((r) => r.game.title), ['Alpha', 'beta']);
  assert.equal(totalOpen(g), 3);

  g = buildGroups(games, [], { no_cover: [1] });
  assert.deepEqual(g[0].rows.map((r) => r.game.id), [2]);
  assert.equal(g[0].ignoredCount, 1);
  assert.equal(g.find((x) => x.key === 'no_year').rows.length, 1, 'ignoring is per group');
  assert.equal(ignoredTotal(games, [], { no_cover: [1] }), 1);

  g = buildGroups(games, [], { no_cover: [1] }, true);
  assert.deepEqual(g[0].rows.map((r) => [r.game.id, r.ignored]), [[1, true], [2, false]]);
  assert.equal(totalOpen(g), 2);
});

test('uncertain rows carry the top proposal', () => {
  const q = [{ game_id: 1, candidates: [{ igdb_id: 9, name: 'Hades', score: 80 }, { igdb_id: 10, name: 'x', score: 70 }] }, { game_id: 2, candidates: [] }];
  const g = buildGroups([ok({ id: 1, igdb_id: null }), ok({ id: 2, title: 'B', igdb_id: null })], q);
  assert.equal(g.find((x) => x.key === 'uncertain').rows[0].candidate.igdb_id, 9);
  assert.deepEqual(g.find((x) => x.key === 'unlinked').rows.map((r) => r.game.id), [2]);
});
