import test from 'node:test';
import assert from 'node:assert/strict';
import { buildWishlist, buildDeals, tagsOf, lowLine, SORT_KEYS, COLUMNS } from '../static/js/deals.js';

const P = (o) => ({ currency: 'EUR', shop: 'Steam', price: 1000, regular: 2000, cut: 50, low_price: 800, low_shop: 'GOG', expiry: null, ...o });
const item = (appid, title, p, o = {}) => ({ appid, title, matched: true, itad_id: 'x' + appid, price: p, alert: null, added_at: null, ...o });
const wish = (items, opts) => buildWishlist({ wishlist: { items } }, opts).rows.map((r) => r.item.title);

test('every column key is a sort key', () => {
  for (const c of COLUMNS) assert.ok(SORT_KEYS.some((s) => s.key === c.key), c.key);
});

const items = [
  item(1, 'Beta', P({ price: 500, cut: 20, shop: 'Steam', low_price: 400, expiry: '2026-12-01T00:00:00Z' }), { added_at: '2026-01-01T00:00:00Z' }),
  item(2, 'alpha', P({ price: 900, cut: 75, shop: 'GOG', low_price: 100, expiry: '2026-11-10T00:00:00Z' }), { added_at: '2026-03-01T00:00:00Z' }),
  item(3, 'Gamma', P({ price: 300, cut: 0, regular: 300, shop: 'Epic', low_price: null }), { added_at: null }),
];

test('column sorts: natural order and reversed', () => {
  assert.deepEqual(wish(items, { sort: 'name' }), ['alpha', 'Beta', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'name', dir: -1 }), ['Gamma', 'Beta', 'alpha']);
  assert.deepEqual(wish(items, { sort: 'price' }), ['Gamma', 'Beta', 'alpha']);
  assert.deepEqual(wish(items, { sort: 'cut' }), ['alpha', 'Beta', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'shop' }), ['Gamma', 'alpha', 'Beta']);
});

test('rows without a value stay last in both directions', () => {
  assert.deepEqual(wish(items, { sort: 'lowest' }), ['alpha', 'Beta', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'lowest', dir: -1 }), ['Beta', 'alpha', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'ends' }), ['alpha', 'Beta', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'ends', dir: -1 }), ['Beta', 'alpha', 'Gamma']);
  assert.deepEqual(wish(items, { sort: 'added' }), ['alpha', 'Beta', 'Gamma']); // most recent first
  assert.deepEqual(wish(items, { sort: 'added', dir: -1 }), ['Beta', 'alpha', 'Gamma']);
});

test('the library list sorts by name and shop too', () => {
  const games = [{ id: 1, title: 'B', status: 'backlog' }, { id: 2, title: 'A', status: 'backlog' }];
  const info = { watched: [1, 2], prices: [{ game_id: 1, ...P({}) }, { game_id: 2, ...P({ shop: 'GOG' }) }] };
  assert.deepEqual(buildDeals(games, info, { sort: 'name' }).rows.map((r) => r.game.title), ['A', 'B']);
  assert.deepEqual(buildDeals(games, info, { sort: 'shop' }).rows.map((r) => r.game.title), ['A', 'B']);
  assert.deepEqual(buildDeals(games, info, { sort: 'shop', dir: -1 }).rows.map((r) => r.game.title), ['B', 'A']);
});

test('tags: one tag when badge and alert say the same, New only with an alert', () => {
  assert.deepEqual(tagsOf('low', { kind: 'historical' }, true).map((t) => t.text), ['Historical low', 'New']);
  assert.deepEqual(tagsOf('near', { kind: 'two_year' }, false).map((t) => t.text), ['Near low (within 10%)', 'Lowest in 2 years']);
  assert.deepEqual(tagsOf(null, { kind: 'two_year' }, false).map((t) => t.text), ['Lowest in 2 years']);
  assert.deepEqual(tagsOf(null, null, true), []);
});

test('the muted line', () => {
  assert.equal(lowLine({ low_price: 749, currency: 'EUR', low_shop: 'GOG' }), 'Lowest €7.49 · GOG');
  assert.equal(lowLine({ low_price: null }), 'No price history');
});

import { checkedLabel } from '../static/js/deals.js';
test('checked label: the time today, the day and time before', () => {
  const now = new Date(2026, 10, 3, 18, 0);
  assert.equal(checkedLabel(new Date(2026, 10, 3, 16, 56).toISOString(), now), '16:56');
  assert.equal(checkedLabel(new Date(2026, 9, 27, 9, 5).toISOString(), now), '27 Oct 09:05');
  assert.equal(checkedLabel('garbage', now), '');
});

test('September is "Sep" like every other short month', async () => {
  const { formatDate, noSept } = await import('../static/js/deals.js');
  assert.equal(formatDate('2025-09-04T10:00:00Z'), '4 Sep 2025');
  assert.equal(formatDate('2025-10-04T10:00:00Z'), '4 Oct 2025');
  assert.equal(noSept('Septimus, 4 Sept'), 'Septimus, 4 Sep');
  const { formatRelease } = await import('../static/js/upcoming.js');
  assert.ok(!/Sept\b/.test(formatRelease(Date.UTC(2026, 8, 4) / 1000, 0)));
});
