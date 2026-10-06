import test from 'node:test';
import assert from 'node:assert/strict';
import { store } from '../static/js/api.js';
import { emptyState, matches, cycle, chipState, filterToJSON, stateFromView, stateToQuery, stateFromQuery, signature, activeCount } from '../static/js/filters.js';

store.statusOrder = new Map();
const g = (title, o = {}) => ({ title, _search: title.toLowerCase(), status: 'backlog', platforms: [], genres: [], themes: [], players: [], years: [], periods: [], ownership: null, favorite: false, vr: false, completed: false, ...o });
const games = [
  g('Hades', { genres: ['RPG', 'Indie'], status: 'finished', favorite: true }),
  g('Skyrim', { genres: ['RPG'], status: 'backlog' }),
  g('Celeste', { genres: ['Indie', 'Platform'], status: 'backlog' }),
  g('Tetris', { genres: ['Puzzle'], status: 'playing' }),
];
const names = (s) => games.filter((x) => matches(s, x)).map((x) => x.title);

test('Any vs All within a group', () => {
  const s = emptyState();
  cycle(s, 'genre', 'RPG'); cycle(s, 'genre', 'Indie');
  assert.deepEqual(names(s), ['Hades', 'Skyrim', 'Celeste']); // any
  s.all.add('genre');
  assert.deepEqual(names(s), ['Hades']); // all
});

test('exclude removes games and combines with include', () => {
  const s = emptyState();
  cycle(s, 'genre', 'RPG');
  cycle(s, 'status', 'finished'); cycle(s, 'status', 'finished'); // include -> exclude
  assert.equal(chipState(s, 'status', 'finished'), 'ex');
  assert.deepEqual(names(s), ['Skyrim']);
});

test('chip cycles off -> in -> ex -> off, and All resets below two includes', () => {
  const s = emptyState();
  cycle(s, 'genre', 'RPG'); cycle(s, 'genre', 'Indie'); s.all.add('genre');
  cycle(s, 'genre', 'Indie'); // Indie now excluded, one include left
  assert.equal(s.all.has('genre'), false);
  cycle(s, 'genre', 'Indie');
  assert.equal(chipState(s, 'genre', 'Indie'), 'off');
});

test('flags and search', () => {
  const s = emptyState();
  s.flags.add('favorite');
  assert.deepEqual(names(s), ['Hades']);
  const q = emptyState(); q.q = 'sky';
  assert.deepEqual(names(q), ['Skyrim']);
});

test('skip ignores one group (facet counting)', () => {
  const s = emptyState();
  cycle(s, 'status', 'playing');
  assert.equal(games.filter((x) => matches(s, x, 'status')).length, 4);
});

test('saved view JSON round trip', () => {
  const s = emptyState();
  cycle(s, 'genre', 'RPG'); cycle(s, 'genre', 'Indie'); s.all.add('genre');
  cycle(s, 'status', 'finished'); cycle(s, 'status', 'finished');
  s.flags.add('vr'); s.q = 'abc'; s.sorts = [{ field: 'title', dir: 'desc' }]; s.group = 'status';
  const view = { filter: JSON.parse(JSON.stringify(filterToJSON(s))), sort: s.sorts, group_by: s.group };
  const back = stateFromView(view);
  assert.equal(signature(back), signature(s));
  assert.equal(activeCount(back), 4);
});

test('url round trip', () => {
  const s = emptyState();
  cycle(s, 'genre', 'Hack and slash/Beat \'em up'); cycle(s, 'platform', 'PC'); cycle(s, 'platform', 'PC'); cycle(s, 'platform', 'PS3'); s.all.add('platform'); s.flags.add('favorite');
  s.sorts = [{ field: 'status', dir: 'asc' }];
  const back = stateFromQuery(stateToQuery(s));
  assert.equal(signature(back), signature(s));
});

test('unknown keys in a stored view are ignored', () => {
  const s = stateFromView({ filter: { all: ['status', 'nope'], flags: ['bogus'], in: { nope: ['x'] } }, sort: [], group_by: 'nope' });
  assert.equal(s.all.size, 0);
  assert.equal(s.flags.size, 0);
  assert.equal(s.group, '');
});
