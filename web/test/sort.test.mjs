import test from 'node:test';
import assert from 'node:assert/strict';
import { store } from '../static/js/api.js';
import { buildComparator, groupGames, parseSort, formatSort, DEFAULT_SORT } from '../static/js/sort.js';

store.statusOrder = new Map([['playing', 0], ['backlog', 1], ['finished', 2]]);
store.statusByCode = new Map([['playing', { label: 'Playing' }], ['backlog', { label: 'Backlog' }], ['finished', { label: 'Finished' }]]);

const g = (title, o = {}) => ({ title, status: 'backlog', platforms: [], genres: [], years: [], periods: [], favorite: false, completed: false, series: null, release_year: null, queue_pos: null, added_at: '', updated_at: '', ownership: null, ...o });
const titles = (list, levels) => [...list].sort(buildComparator(levels)).map((x) => x.title);

test('natural title order', () => {
  const list = [g('Fallout 10'), g('Fallout 2'), g('fallout 1')];
  assert.deepEqual(titles(list, [{ field: 'title', dir: 'asc' }]), ['fallout 1', 'Fallout 2', 'Fallout 10']);
});

test('multi-level: status, then last year desc, then title', () => {
  const list = [
    g('B', { status: 'finished', years: [2020] }), g('A', { status: 'finished', years: [2024] }),
    g('C', { status: 'playing', years: [2019] }), g('D', { status: 'finished', years: [2024] }),
  ];
  const lv = [{ field: 'status', dir: 'asc' }, { field: 'last_year', dir: 'desc' }, { field: 'title', dir: 'asc' }];
  assert.deepEqual(titles(list, lv), ['C', 'A', 'D', 'B']);
});

test('nulls sort last in both directions', () => {
  const list = [g('none'), g('old', { release_year: 1999 }), g('new', { release_year: 2020 })];
  assert.deepEqual(titles(list, [{ field: 'release', dir: 'asc' }]), ['old', 'new', 'none']);
  assert.deepEqual(titles(list, [{ field: 'release', dir: 'desc' }]), ['new', 'old', 'none']);
});

test('series sorts by name, then title; games without a series go last', () => {
  const list = [g('x3', { series: 'X' }), g('a1', { series: 'A' }), g('x1', { series: 'X' }), g('solo')];
  assert.deepEqual(titles(list, [{ field: 'series', dir: 'asc' }]), ['a1', 'x1', 'x3', 'solo']);
});

test('default order is status, then title (manual order is ignored)', () => {
  const list = [g('z', { status: 'playing', queue_pos: 1 }), g('y', { status: 'playing', queue_pos: 2 }), g('a', { status: 'backlog', queue_pos: 1 })];
  assert.deepEqual(titles(list, []), ['y', 'z', 'a']);
  assert.deepEqual(DEFAULT_SORT.map((l) => l.field), ['status', 'title']);
});

test('an explicit manual order still follows the queue (Up next, Playing now starter views)', () => {
  const list = [g('b', { queue_pos: 1 }), g('a', { queue_pos: 2 })];
  assert.deepEqual(titles(list, [{ field: 'manual', dir: 'asc' }]), ['b', 'a']);
  const mixed = [g('z', { status: 'playing', queue_pos: 1 }), g('y', { status: 'playing', queue_pos: 2 }), g('a', { status: 'backlog', queue_pos: 1 })];
  assert.deepEqual(titles(mixed, [{ field: 'status', dir: 'asc' }, { field: 'manual', dir: 'asc' }]), ['z', 'y', 'a']);
});

test('the empty sort is the default, and an empty sort url means the default', () => {
  const list = [g('b', { status: 'finished' }), g('c', { status: 'playing' }), g('a', { status: 'playing' })];
  assert.deepEqual(titles(list, parseSort('')), titles(list, DEFAULT_SORT));
  assert.deepEqual(titles(list, []), ['a', 'c', 'b']);
});

test('grouping by status keeps title order inside each group by default', () => {
  const list = [g('z', { status: 'playing', queue_pos: 1 }), g('a', { status: 'playing', queue_pos: 2 })];
  const groups = groupGames([...list].sort(buildComparator([])), 'status');
  assert.deepEqual(groups[0].games.map((x) => x.title), ['a', 'z']);
});

test('favorites first', () => {
  const list = [g('a'), g('b', { favorite: true })];
  assert.deepEqual(titles(list, [{ field: 'favorite', dir: 'desc' }]), ['b', 'a']);
});

test('grouping by year puts a game under every year, newest first, "None" last', () => {
  const list = [g('a', { years: [2024, 2025] }), g('b', { years: [2025], periods: ['childhood'] }), g('c')];
  const groups = groupGames(list, 'year');
  assert.deepEqual(groups.map((x) => x.label), ['2025', '2024', 'Childhood', 'None']);
  assert.deepEqual(groups[0].games.map((x) => x.title), ['a', 'b']);
});

test('sort url round trip ignores unknown fields', () => {
  assert.equal(formatSort(parseSort('status:asc,bogus:desc,last_year:desc')), 'status:asc,last_year:desc');
});

test('sorts by time to beat, unknown last in both directions', async () => {
  const { buildComparator } = await import('../static/js/sort.js');
  const mk = (title, m) => ({ title, ttb_normally: m });
  const list = [mk('B', 600), mk('A', null), mk('C', 60)];
  assert.deepEqual([...list].sort(buildComparator([{ field: 'ttb', dir: 'asc' }])).map((g) => g.title), ['C', 'B', 'A']);
  assert.deepEqual([...list].sort(buildComparator([{ field: 'ttb', dir: 'desc' }])).map((g) => g.title), ['B', 'C', 'A']);
});
