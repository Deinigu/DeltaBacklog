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
  s.flags.add('vr'); s.q = 'abc'; s.sorts = [{ field: 'title', dir: 'desc' }]; s.group = 'status'; s.layout = 'table';
  const view = { filter: JSON.parse(JSON.stringify(filterToJSON(s))), sort: s.sorts, group_by: s.group, layout: s.layout };
  const back = stateFromView(view);
  assert.equal(signature(back), signature(s));
  assert.equal(activeCount(back), 4);
});

test('url round trip', () => {
  const s = emptyState();
  cycle(s, 'genre', 'Hack and slash/Beat \'em up'); cycle(s, 'platform', 'PC'); cycle(s, 'platform', 'PC'); cycle(s, 'platform', 'PS3'); s.all.add('platform'); s.flags.add('favorite');
  s.sorts = [{ field: 'status', dir: 'asc' }]; s.layout = 'table';
  const back = stateFromQuery(stateToQuery(s));
  assert.equal(signature(back), signature(s));
});

test('unknown keys in a stored view are ignored', () => {
  const s = stateFromView({ filter: { all: ['status', 'nope'], flags: ['bogus'], in: { nope: ['x'] } }, sort: [], group_by: 'nope' });
  assert.equal(s.all.size, 0);
  assert.equal(s.flags.size, 0);
  assert.equal(s.group, '');
});

test('layout defaults to cards and unknown values are ignored', () => {
  assert.equal(emptyState().layout, 'cards');
  assert.equal(stateFromView({ filter: {}, sort: [], layout: 'grid' }).layout, 'cards');
  assert.equal(stateFromQuery('layout=table').layout, 'table');
});

test('facetSignature changes when a filter value appears or disappears, not when games merely change', async () => {
  const { facetSignature } = await import('../static/js/filters.js');
  const a = [g('A', { genres: ['RPG'], platforms: ['PC'] }), g('B', { genres: ['Indie'], platforms: ['PC'] })];
  const base = facetSignature(a);
  assert.equal(facetSignature([...a].reverse()), base, 'order does not matter');
  assert.equal(facetSignature([g('A', { genres: ['RPG'], platforms: ['PC'], favorite: true }), a[1]]), base, 'unrelated fields do not matter');
  assert.notEqual(facetSignature([a[0], g('B', { genres: ['Indie', 'Roguelike'], platforms: ['PC'] })]), base, 'a new genre');
  assert.notEqual(facetSignature([a[0], g('B', { genres: ['Indie'], platforms: ['PC', 'Switch'] })]), base, 'a new platform');
  assert.notEqual(facetSignature([a[0]]), base, 'a value that disappeared (Indie)');
  assert.notEqual(facetSignature([a[0], g('B', { genres: ['Indie'], platforms: ['PC'], years: [2024] })]), base, 'a new year');
});

test('time to beat filter round-trips through saved views and the URL', () => {
  const s = emptyState();
  s.ttb.min = 300; s.ttb.max = 900; s.ttb.unknown = 'in'; s.ttb.kind = 'ttb_hastily';
  const json = filterToJSON(s);
  assert.deepEqual(json.ttb, { min: 300, max: 900, unknown: 'in', kind: 'ttb_hastily' });
  const back = stateFromView({ filter: JSON.parse(JSON.stringify(json)) });
  assert.deepEqual(back.ttb, s.ttb);
  assert.equal(signature(back), signature(s));
  assert.deepEqual(stateFromQuery(stateToQuery(s)).ttb, s.ttb);
  assert.equal(activeCount(s), 2); // the range and "no time known"
  assert.equal(filterToJSON(emptyState()).ttb, undefined);
});

test('time to beat filter drops garbage from saved JSON', () => {
  const t = stateFromView({ filter: { ttb: { min: -5, max: 'x', unknown: 'maybe', kind: 'nope' } } }).ttb;
  assert.deepEqual(t, { min: null, max: null, unknown: '', kind: '' });
  assert.deepEqual(stateFromView({ filter: { ttb: { min: 900, max: 300 } } }).ttb.min, null); // empty range
});

test('old "short" flag becomes a range under 10 h', () => {
  const v = stateFromView({ filter: { flags: ['short', 'vr'] } });
  assert.equal(v.ttb.max, 600);
  assert.deepEqual([...v.flags], ['vr']);
  const keep = stateFromView({ filter: { flags: ['short'], ttb: { max: 300 } } });
  assert.equal(keep.ttb.max, 300); // the stricter bound wins
  const q = stateFromQuery('short=1');
  assert.equal(q.ttb.max, 600);
  assert.deepEqual(filterToJSON(v), { flags: ['vr'], ttb: { max: 600 } });
  const gm = (m) => ({ ...games[0], ttb_normally: m });
  assert.equal(matches(v, { ...gm(599), vr: true }), true);
  assert.equal(matches(v, { ...gm(600), vr: true }), false);
});

test('a stored sort level on a field that no longer exists is dropped (the removed Steam hours)', () => {
  const s = stateFromView({ filter: {}, sort: [{ field: 'playtime', dir: 'desc' }, { field: 'title', dir: 'asc' }, { field: 'toString', dir: 'asc' }], group_by: '' });
  assert.deepEqual(s.sorts, [{ field: 'title', dir: 'asc' }]);
});
