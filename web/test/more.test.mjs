import test from 'node:test';
import assert from 'node:assert/strict';
import { MORE_PAGES, isMorePath } from '../static/js/more.js';

test('More holds Series, Upcoming, Similar games, Stats, Year in review, Rankings, Screenshots and Views', () => {
  assert.deepEqual(MORE_PAGES.map((p) => p.path), ['series', 'upcoming', 'similar', 'stats', 'years', 'rankings', 'screenshots', 'views']);
  for (const p of ['series', 'upcoming', 'similar', 'stats', 'years', 'rankings', 'screenshots', 'views']) assert.ok(isMorePath(p), p);
  for (const p of ['library', 'view/3', 'add']) assert.ok(!isMorePath(p), p);
});
