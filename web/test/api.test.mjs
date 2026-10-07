import test from 'node:test';
import assert from 'node:assert/strict';
import { localPatch } from '../static/js/api.js';

test('localPatch turns cleared text and zero numbers into null, like the server', () => {
  assert.deepEqual(localPatch({ series: '', developer: '', ownership: '' }), { series: null, developer: null, ownership: null });
  assert.deepEqual(localPatch({ release_year: 0, series_order: 0 }), { release_year: null, series_order: null });
});

test('localPatch leaves real values and other fields alone', () => {
  const p = { series: 'Hades', release_year: 2025, notes: '', title: 'X', platforms: [] };
  assert.deepEqual(localPatch(p), p);
  assert.notEqual(localPatch(p), p); // returns a copy
});
