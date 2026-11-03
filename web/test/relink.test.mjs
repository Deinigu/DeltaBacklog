import test from 'node:test';
import assert from 'node:assert/strict';
import { changeText, appliedText, errorText, hasUndo } from '../static/js/relink.js';

test('a change reads as field: old → new', () => {
  assert.equal(changeText({ label: 'Genres', old: 'Action', new: 'Indie, Roguelike' }), 'Genres: Action → Indie, Roguelike');
  assert.equal(changeText({ label: 'Developer', old: '', new: 'Supergiant' }), 'Developer: (empty) → Supergiant');
});

test('the toast says what happened', () => {
  assert.equal(appliedText([{}, {}], 'replace'), 'Updated 2 fields from IGDB');
  assert.equal(appliedText([{}], 'empty'), 'Filled 1 field from IGDB');
  assert.match(appliedText([], 'empty'), /Nothing to fill/);
  assert.match(appliedText([], 'replace'), /nothing different/);
});

test('errors map to words', () => {
  assert.match(errorText({ status: 503 }), /not set up/);
  assert.match(errorText({ status: 502 }), /did not answer/);
  assert.match(errorText(new Error('x')), /Could not update/);
});

test('undo is offered only with something to restore', () => {
  assert.equal(hasUndo({}), false);
  assert.equal(hasUndo(null), false);
  assert.equal(hasUndo({ developer: '' }), true);
});
