import test from 'node:test';
import assert from 'node:assert/strict';
import { localPatch } from '../static/js/api.js';

test('localPatch turns cleared text and zero numbers into null, like the server', () => {
  assert.deepEqual(localPatch({ series: '', developer: '', ownership: '' }), { series: null, developer: null, ownership: null });
  assert.deepEqual(localPatch({ release_year: 0 }), { release_year: null });
});

test('localPatch leaves real values and other fields alone', () => {
  const p = { series: 'Hades', release_year: 2025, notes: '', title: 'X', platforms: [] };
  assert.deepEqual(localPatch(p), p);
  assert.notEqual(localPatch(p), p); // returns a copy
});

test('coverURL changes with the cover version so a new cover is never served from the old cache entry', async () => {
  const { coverURL } = await import('../static/js/api.js');
  const a = coverURL({ cover: '7.jpg', cover_v: '2026-10-13T10:00:00.100Z' });
  const b = coverURL({ cover: '7.jpg', cover_v: '2026-10-13T10:00:00.200Z' });
  assert.notEqual(a, b);
  assert.match(a, /^\/covers\/7\.jpg\?v=/);
  assert.equal(coverURL({ cover: null }), null);
});
