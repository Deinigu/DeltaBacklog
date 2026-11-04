import test from 'node:test';
import assert from 'node:assert/strict';
import { matchesText, words, override, toggleIn, tally, withSelected, params, qOf, setOf, idsOf, intIn, appendSet, filterByName } from '../static/js/listfilter.js';

test('a search matches when every word is in one of the texts, ignoring case and accents', () => {
  assert.ok(matchesText('', 'anything'));
  assert.ok(matchesText('  ', 'anything'));
  assert.ok(matchesText('zelda wild', 'The Legend of Zelda', 'Breath of the Wild'));
  assert.ok(matchesText('pokemon', 'Pokémon Red'));
  assert.ok(matchesText('POKÉMON', 'pokemon red'));
  assert.ok(!matchesText('zelda mario', 'The Legend of Zelda'));
  assert.ok(!matchesText('x', null, undefined, ''));
  assert.deepEqual(words('  A  b '), ['a', 'b']);
});

test('override replaces one key; a Set key gets a Set of that value', () => {
  const f = { a: new Set(['x', 'y']), b: 3, c: 'k' };
  const g = override(f, 'a', 'z');
  assert.deepEqual([...g.a], ['z']);
  assert.deepEqual([...f.a], ['x', 'y']);
  assert.equal(override(f, 'b', 0).b, 0);
  assert.equal(f.b, 3);
});

test('toggleIn adds and removes', () => {
  const s = new Set();
  toggleIn(s, 1); assert.ok(s.has(1));
  toggleIn(s, 1); assert.ok(!s.has(1));
});

test('tally counts distinct values per item, the most first, then by name; empty values are left out', () => {
  const items = [{ p: ['PC', 'PC', 'PS5'] }, { p: ['PC'] }, { p: [] }, { p: ['Switch', ''] }, { p: 'Xbox' }, { p: null }];
  assert.deepEqual(tally(items, (i) => i.p), [{ value: 'PC', count: 2 }, { value: 'PS5', count: 1 }, { value: 'Switch', count: 1 }, { value: 'Xbox', count: 1 }]);
});

test('withSelected keeps a selected value that no item has any more', () => {
  assert.deepEqual(withSelected(['a', 'b'], new Set(['b', 'z'])), ['a', 'b', 'z']);
});

test('query helpers drop what is unknown or out of range', () => {
  const qs = params('?q=%20zelda%20&p=a&p=b&p=&p=c&n=3,x,-1,4.5,7');
  assert.equal(qOf(qs), 'zelda');
  assert.deepEqual([...setOf(qs, 'p')], ['a', 'b', 'c']);
  assert.deepEqual([...setOf(qs, 'p', ['b', 'z'])], ['b']);
  assert.deepEqual([...idsOf(qs, 'n')], [3, 7]);
  assert.equal(intIn('2020', 1950, 2100), 2020);
  assert.equal(intIn('1900', 1950, 2100), 0);
  assert.equal(intIn('abc', 1950, 2100), 0);
  assert.equal(intIn(null, 1950, 2100), 0);
  assert.equal(qOf(params('q=' + 'x'.repeat(500))).length, 100);
  assert.equal(setOf(params(Array.from({ length: 100 }, (_, i) => `p=v${i}`).join('&')), 'p').size, 40);
});

test('appendSet writes values in a fixed order', () => {
  const qs = new URLSearchParams();
  appendSet(qs, 'k', new Set(['b', 'a', 'x']), ['a', 'b']);
  appendSet(qs, 'j', new Set(['b', 'a']));
  assert.equal(qs.toString(), 'k=a&k=b&k=x&j=a&j=b');
});

test('filterByName', () => {
  const l = [{ n: 'Best of 2025', d: 'tiers' }, { n: 'Souls', d: '' }];
  assert.equal(filterByName(l, '', (r) => [r.n]), l);
  assert.deepEqual(filterByName(l, 'best 2025', (r) => [r.n]).map((r) => r.n), ['Best of 2025']);
  assert.deepEqual(filterByName(l, 'tiers', (r) => [r.n, r.d]).map((r) => r.n), ['Best of 2025']);
  assert.deepEqual(filterByName(l, 'zzz', (r) => [r.n]), []);
});
