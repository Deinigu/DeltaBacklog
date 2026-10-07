import test from 'node:test';
import assert from 'node:assert/strict';
import { slotOrder, moveItem } from '../static/js/order.js';

test('slotOrder reuses the positions the ids already occupy', () => {
  const pos = new Map([[10, 5], [20, 9], [30, 12], [99, 7]]);
  const next = slotOrder([30, 10, 20], pos);
  assert.deepEqual([...next], [[30, 5], [10, 9], [20, 12]]);
  assert.equal(pos.get(99), 7); // a game outside the list is untouched
});

test('slotOrder is stable when nothing moved', () => {
  const pos = new Map([[1, 3], [2, 8]]);
  assert.deepEqual([...slotOrder([1, 2], pos)], [[1, 3], [2, 8]]);
});

test('moveItem moves one item and ignores out-of-range targets', () => {
  assert.deepEqual(moveItem([1, 2, 3, 4], 0, 2), [2, 3, 1, 4]);
  assert.deepEqual(moveItem([1, 2, 3, 4], 3, 0), [4, 1, 2, 3]);
  assert.deepEqual(moveItem([1, 2, 3], 0, -1), [1, 2, 3]);
  assert.deepEqual(moveItem([1, 2, 3], 2, 3), [1, 2, 3]);
});
