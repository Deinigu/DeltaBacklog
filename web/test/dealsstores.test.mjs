import test from 'node:test';
import assert from 'node:assert/strict';
import { altLine, moveStore, addStore, removeStore, parseThreshold, unusedShops, MAX_STORES } from '../static/js/deals.js';

test('altLine names the cheaper offer at another shop', () => {
  assert.equal(altLine({ price: 1500, currency: 'EUR', alt_shop: 'Epic Game Store', alt_price: 999 }), '€9.99 at Epic Game Store');
  assert.equal(altLine({ price: 1500, currency: 'EUR', alt_shop: null, alt_price: null }), '');
  assert.equal(altLine({ price: 1500, currency: 'EUR' }), '');
  assert.equal(altLine({ price: 900, currency: 'EUR', alt_shop: 'Epic', alt_price: 900 }), ''); // not cheaper: no hint
  assert.equal(altLine(null), '');
});

test('moving a store swaps neighbours, never past the ends, never mutating', () => {
  const l = ['Steam', 'GOG', 'Epic'];
  assert.deepEqual(moveStore(l, 1, -1), ['GOG', 'Steam', 'Epic']);
  assert.deepEqual(moveStore(l, 1, 1), ['Steam', 'Epic', 'GOG']);
  assert.equal(moveStore(l, 0, -1), l);
  assert.equal(moveStore(l, 2, 1), l);
  assert.equal(moveStore(l, 5, 1), l);
  assert.deepEqual(l, ['Steam', 'GOG', 'Epic']);
});

test('adding a store trims, ignores duplicates (case aside), blanks and a full list', () => {
  const l = ['Steam'];
  assert.deepEqual(addStore(l, '  GOG '), ['Steam', 'GOG']);
  assert.equal(addStore(l, 'steam'), l);
  assert.equal(addStore(l, '   '), l);
  assert.equal(addStore(l, null), l);
  assert.equal(addStore(l, 'x'.repeat(61)), l);
  const full = Array.from({ length: MAX_STORES }, (_, i) => `S${i}`);
  assert.equal(addStore(full, 'More'), full);
  assert.deepEqual(removeStore(['a', 'b', 'c'], 1), ['a', 'c']);
});

test('the threshold is a whole number from 0 to 100', () => {
  assert.equal(parseThreshold('15'), 15);
  assert.equal(parseThreshold(' 0 '), 0);
  assert.equal(parseThreshold('100'), 100);
  assert.equal(parseThreshold('101'), null);
  assert.equal(parseThreshold('-1'), null);
  assert.equal(parseThreshold('1.5'), null);
  assert.equal(parseThreshold(''), null);
});

test('the add box offers the shops not ranked yet', () => {
  assert.deepEqual(unusedShops(['GOG', 'Steam', 'Epic'], ['steam']), ['GOG', 'Epic']);
  assert.deepEqual(unusedShops(undefined, []), []);
});
