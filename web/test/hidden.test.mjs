import test from 'node:test';
import assert from 'node:assert/strict';
import { groupHidden, hiddenCount, withoutItem, becauseTitles, hiddenOn } from '../static/js/hidden.js';

const resp = {
  deals: [{ id: 4, title: 'Deal', cover: 'a.jpg', cover_v: 'x', poster: 'p.jpg', poster_v: 'y', hidden_at: '2026-03-04T10:00:00Z' }],
  similar: [{ igdb_id: 9, title: 'Sim', year: 2010, cover: null, v: '', because: ['A', 'B'], hidden_at: '2026-03-05T10:00:00Z' }],
  series: [{ collection_id: 7, igdb_id: 101, title: 'Zed II', year: 2005, cover: 'i101.jpg', v: 'b', series: 'Zed Series', hidden_at: '' }],
};

test('groups follow Deals, Similar games, Series and carry the unhide reference', () => {
  const g = groupHidden(resp);
  assert.deepEqual(g.map((x) => x.label), ['Deals', 'Similar games', 'Series']);
  assert.deepEqual(g[0].items[0].ref, { id: 4 });
  assert.equal(g[0].items[0].poster, 'p.jpg');
  assert.equal(g[0].items[0].poster_v, 'y');
  assert.equal(g[1].items[0].note, 'Because you played A and B');
  assert.deepEqual(g[2].items[0].ref, { collection_id: 7, igdb_id: 101 });
  assert.equal(g[2].items[0].note, 'Series: Zed Series');
  assert.equal(g[2].items[0].when, '');
  assert.equal(hiddenCount(g), 3);
});

test('empty groups are left out, and an empty or missing answer gives no groups', () => {
  assert.deepEqual(groupHidden({ deals: [], similar: [resp.similar[0]], series: [] }).map((x) => x.kind), ['similar']);
  assert.deepEqual(groupHidden({}), []);
  assert.deepEqual(groupHidden(null), []);
});

test('withoutItem drops one row and the group that empties', () => {
  const g = groupHidden(resp);
  const left = withoutItem(g, 'deals:4');
  assert.deepEqual(left.map((x) => x.kind), ['similar', 'series']);
  assert.equal(hiddenCount(withoutItem(g, 'series:7:101')), 2);
  assert.equal(hiddenCount(withoutItem(g, 'nope')), 3);
});

test('becauseTitles and hiddenOn', () => {
  assert.equal(becauseTitles([]), '');
  assert.equal(becauseTitles(['A', 'B', 'C', 'D', 'E']), 'Because you played A, B and C and 2 more');
  assert.equal(hiddenOn(''), '');
  assert.equal(hiddenOn('nonsense'), '');
  assert.equal(hiddenOn('2026-03-04T10:00:00Z', 'en-GB'), 'Hidden on 4 Mar 2026');
});
