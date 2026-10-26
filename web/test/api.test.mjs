import test from 'node:test';
import assert from 'node:assert/strict';
import { localPatch, applyPosters, store } from '../static/js/api.js';

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
  assert.match(a, /^covers\/7\.jpg\?v=/); // relative: the app may be served under a sub-path
  assert.equal(coverURL({ cover: null }), null);
});

test('retargetImg points a cover image at its new version, lazy or loaded', async () => {
  const { retargetImg } = await import('../static/js/util.js');
  const lazy = { dataset: { src: 'covers/1.jpg?v=a' }, hasAttribute: () => false };
  assert.equal(retargetImg(lazy, 'covers/1.jpg?v=b'), true);
  assert.equal(lazy.dataset.src, 'covers/1.jpg?v=b');
  assert.equal(lazy.src, undefined); // not requested yet: the observer will use data-src
  const loaded = { dataset: { src: 'covers/1.jpg?v=a' }, src: 'covers/1.jpg?v=a', hasAttribute: () => true };
  retargetImg(loaded, 'covers/1.jpg?v=b');
  assert.equal(loaded.src, 'covers/1.jpg?v=b');
  assert.equal(retargetImg(loaded, 'covers/1.jpg?v=b'), false); // unchanged
});

test('applyPosters returns the ids of the games whose poster changed', () => {
  store.byId.set(1, { id: 1, poster: null, poster_v: 0 });
  store.byId.set(2, { id: 2, poster: 'p', poster_v: 3 });
  const ids = applyPosters([{ game_id: 1, poster: 'p', poster_v: 1 }, { game_id: 2, poster: 'p', poster_v: 3 }, { game_id: 9, poster: 'p', poster_v: 1 }]);
  assert.deepEqual(ids, [1]);
  assert.deepEqual(applyPosters([{ game_id: 1, poster: 'p', poster_v: 1 }]), []);
});
