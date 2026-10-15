import test from 'node:test';
import assert from 'node:assert/strict';
import { galleryQuery, groupRuns, remaining, shotsLabel, runLabel, stepAcross, emptyText } from '../static/js/gallery.js';

test('galleryQuery leaves empty filters out', () => {
  assert.equal(galleryQuery({}), '?sort=game&limit=120');
  assert.equal(galleryQuery({ sort: 'newest' }), '?limit=120');
  assert.equal(galleryQuery({ q: ' hades ', year: 2024, sort: 'newest', offset: 120, limit: 60 }), '?q=hades&year=2024&offset=120&limit=60');
  assert.equal(galleryQuery({ q: 'a&b' }), '?q=a%26b&sort=game&limit=120');
});

test('groupRuns keeps the index in the whole list and merges appended pages', () => {
  const s = (id, game_id, game_title) => ({ id, game_id, game_title });
  const list = [s(1, 1, 'A'), s(2, 1, 'A'), s(3, 2, 'B'), s(4, 1, 'A')];
  const runs = groupRuns(list);
  assert.deepEqual(runs.map((r) => [r.gameId, r.items.map((i) => i.index)]), [[1, [0, 1]], [2, [2]], [1, [3]]]);
  assert.equal(runs[1].title, 'B');
  assert.deepEqual(groupRuns([]), []);
  assert.deepEqual(groupRuns([s(5, 3, 'C')], 10)[0].items.map((i) => i.index), [10]); // a later page starts at its offset
});

test('remaining and labels', () => {
  assert.equal(remaining(120, 300), 180);
  assert.equal(remaining(300, 300), 0);
  assert.equal(remaining(310, 300), 0);
  assert.equal(shotsLabel(1), '1 screenshot');
  assert.equal(shotsLabel(0), '0 screenshots');
});

test('runLabel marks a run that may continue', () => {
  assert.equal(runLabel(3, false), '3 screenshots');
  assert.equal(runLabel(1, false), '1 screenshot');
  assert.equal(runLabel(3, true), '3+ screenshots');
});

test('stepAcross asks for more at the end of a partial list, else wraps', () => {
  assert.deepEqual(stepAcross(2, 3, 10, 1), { index: 2, more: true });
  assert.deepEqual(stepAcross(2, 3, 3, 1), { index: 0, more: false });
  assert.deepEqual(stepAcross(0, 3, 10, -1), { index: 2, more: false });
  assert.deepEqual(stepAcross(1, 3, 10, 1), { index: 2, more: false });
});

test('emptyText depends on the filters', () => {
  assert.match(emptyText({}), /No screenshots yet/);
  assert.match(emptyText({ q: 'x' }), /match/);
  assert.match(emptyText({ year: '2020' }), /match/);
});
