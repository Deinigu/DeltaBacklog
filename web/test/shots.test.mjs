import test from 'node:test';
import assert from 'node:assert/strict';
import { moveShot, stepIndex, formatBytes, formatTaken, countLabel, pickImages, uploadSummary } from '../static/js/shots.js';

test('moveShot swaps with the neighbour and refuses the ends', () => {
  assert.deepEqual(moveShot([1, 2, 3], 2, -1), [2, 1, 3]);
  assert.deepEqual(moveShot([1, 2, 3], 2, 1), [1, 3, 2]);
  assert.equal(moveShot([1, 2, 3], 1, -1), null);
  assert.equal(moveShot([1, 2, 3], 3, 1), null);
  assert.equal(moveShot([1, 2, 3], 9, 1), null);
  const a = [1, 2];
  moveShot(a, 1, 1);
  assert.deepEqual(a, [1, 2]);
});

test('stepIndex wraps', () => {
  assert.equal(stepIndex(0, 3, -1), 2);
  assert.equal(stepIndex(2, 3, 1), 0);
  assert.equal(stepIndex(1, 3, 1), 2);
  assert.equal(stepIndex(0, 0, 1), -1);
});

test('formatBytes', () => {
  assert.equal(formatBytes(900), '900 B');
  assert.equal(formatBytes(340 * 1024), '340 KB');
  assert.equal(formatBytes(2.4 * 1048576), '2.4 MB');
  assert.equal(formatBytes(25 * 1048576), '25 MB');
  assert.equal(formatBytes(NaN), '');
});

test('formatTaken', () => {
  assert.equal(formatTaken('2023-11-14T22:13:20Z'), '14 Nov 2023');
  assert.equal(formatTaken('2024-05-06T07:08:09'), '6 May 2024');
  assert.equal(formatTaken(''), '');
  assert.equal(formatTaken('2024-13-01'), '');
  assert.equal(formatTaken(undefined), '');
});

test('countLabel', () => {
  assert.equal(countLabel(0), 'Screenshots');
  assert.equal(countLabel(4), 'Screenshots (4)');
});

test('pickImages keeps JPEG, PNG and WebP, and names the rest', () => {
  const f = (name, type) => ({ name, type });
  const r = pickImages([f('a.png', 'image/png'), f('b.gif', 'image/gif'), f('c.webp', ''), f('d.heic', 'image/heic'), f('e.txt', 'text/plain'), f('f.JPG', '')]);
  assert.deepEqual(r.ok.map((x) => x.name), ['a.png', 'c.webp', 'f.JPG']);
  assert.deepEqual(r.skipped, ['b.gif', 'd.heic', 'e.txt']);
});

test('uploadSummary', () => {
  assert.equal(uploadSummary(1, 0), 'Added 1 screenshot');
  assert.equal(uploadSummary(3, 0), 'Added 3 screenshots');
  assert.equal(uploadSummary(2, 1), 'Added 2 screenshots, 1 failed');
  assert.equal(uploadSummary(0, 2), '2 failed');
});

import { pendingIds, withoutPending, clampIndex } from '../static/js/shots.js';

test('pending deletes are filtered out and per game', () => {
  const pending = new Map([[2, { gameId: 1, s: { id: 2 } }], [5, { gameId: 9, s: { id: 5 } }]]);
  assert.deepEqual(pendingIds(pending, 1), [2]);
  assert.deepEqual(withoutPending([{ id: 1 }, { id: 2 }], pending).map((s) => s.id), [1]);
});

test('clampIndex', () => {
  assert.equal(clampIndex(5, 3), 2);
  assert.equal(clampIndex(-1, 3), 0);
  assert.equal(clampIndex(0, 0), -1);
});

import { thumbPlan } from '../static/js/shots.js';

test('thumbPlan crops extreme ratios like the server', () => {
  assert.deepEqual(thumbPlan(1080, 40000), { cw: 1080, ch: 2160, tw: 480, th: 960 });
  assert.deepEqual(thumbPlan(400, 20000), { cw: 400, ch: 800, tw: 400, th: 800 });
  assert.deepEqual(thumbPlan(20000, 100), { cw: 400, ch: 100, tw: 400, th: 100 });
  assert.deepEqual(thumbPlan(7000, 5700), { cw: 7000, ch: 5700, tw: 480, th: 390 });
  assert.deepEqual(thumbPlan(100, 50), { cw: 100, ch: 50, tw: 100, th: 50 });
  assert.equal(thumbPlan(0, 5).tw, 0);
});
