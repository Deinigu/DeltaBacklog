import test from 'node:test';
import assert from 'node:assert/strict';
import { MORE_PAGES, isMorePath } from '../static/js/more.js';

test('More holds Series, Stats, Year in review, Rankings and Views', () => {
  assert.deepEqual(MORE_PAGES.map((p) => p.path), ['series', 'stats', 'years', 'rankings', 'views']);
  for (const p of ['series', 'stats', 'years', 'rankings', 'views']) assert.ok(isMorePath(p), p);
  for (const p of ['library', 'view/3', 'add']) assert.ok(!isMorePath(p), p);
});
