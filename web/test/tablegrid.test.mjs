import test from 'node:test';
import assert from 'node:assert/strict';
import { nextCell, isEditKey } from '../static/js/tablegrid.js';

const grid = { rows: 5, cols: 4, page: 3 };

test('arrows move one cell and stop at the edges', () => {
  assert.deepEqual(nextCell({ row: 2, col: 1 }, 'ArrowDown', grid), { row: 3, col: 1 });
  assert.deepEqual(nextCell({ row: 2, col: 1 }, 'ArrowUp', grid), { row: 1, col: 1 });
  assert.deepEqual(nextCell({ row: 2, col: 1 }, 'ArrowRight', grid), { row: 2, col: 2 });
  assert.deepEqual(nextCell({ row: 2, col: 1 }, 'ArrowLeft', grid), { row: 2, col: 0 });
  assert.deepEqual(nextCell({ row: 0, col: 0 }, 'ArrowUp', grid), { row: 0, col: 0 });
  assert.deepEqual(nextCell({ row: 0, col: 0 }, 'ArrowLeft', grid), { row: 0, col: 0 });
  assert.deepEqual(nextCell({ row: 4, col: 3 }, 'ArrowDown', grid), { row: 4, col: 3 });
  assert.deepEqual(nextCell({ row: 4, col: 3 }, 'ArrowRight', grid), { row: 4, col: 3 });
});

test('Home/End go to the row ends, with Ctrl to the grid corners; PageUp/PageDown jump a page', () => {
  assert.deepEqual(nextCell({ row: 2, col: 2 }, 'Home', grid), { row: 2, col: 0 });
  assert.deepEqual(nextCell({ row: 2, col: 2 }, 'End', grid), { row: 2, col: 3 });
  assert.deepEqual(nextCell({ row: 2, col: 2 }, 'Home', { ...grid, ctrl: true }), { row: 0, col: 0 });
  assert.deepEqual(nextCell({ row: 2, col: 2 }, 'End', { ...grid, ctrl: true }), { row: 4, col: 3 });
  assert.deepEqual(nextCell({ row: 1, col: 2 }, 'PageDown', grid), { row: 4, col: 2 });
  assert.deepEqual(nextCell({ row: 1, col: 2 }, 'PageUp', grid), { row: 0, col: 2 });
});

test('other keys are not moves; an empty grid has nowhere to go', () => {
  assert.equal(nextCell({ row: 0, col: 0 }, 'a', grid), null);
  assert.equal(nextCell({ row: 0, col: 0 }, 'Enter', grid), null);
  assert.equal(nextCell({ row: 0, col: 0 }, 'ArrowDown', { rows: 0, cols: 4 }), null);
  assert.equal(nextCell({ row: 9, col: 9 }, 'ArrowLeft', grid).row, 4); // a stale position is pulled back into the grid
});

test('Enter and F2 start an edit', () => {
  assert.equal(isEditKey('Enter'), true);
  assert.equal(isEditKey('F2'), true);
  assert.equal(isEditKey('Escape'), false);
  assert.equal(isEditKey(' '), false);
});
