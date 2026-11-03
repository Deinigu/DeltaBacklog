import test from 'node:test';
import assert from 'node:assert/strict';
import { buildWishlist, unseenCount, oldestCheck, steamStoreURL } from '../static/js/deals.js';

const price = (o = {}) => ({ currency: 'EUR', shop: 'Steam', price: 1000, regular: 2000, cut: 50, url: 'https://shop.example/', low_price: 800, low_shop: 'GOG', low_at: '2025-11-27T10:00:00Z', fetched_at: '2026-10-01T10:00:00Z', ...o });
const item = (appid, title, o = {}) => ({ appid, title, itad_id: `i${appid}`, matched: true, slug: '', added_at: '', price: price(), alert: null, muted: false, ...o });
const titles = (rows) => rows.map((r) => r.item.title);

test('no wishlist (no STEAM_ID, or an old server): nothing to list, no crash', () => {
  for (const info of [undefined, null, {}, { wishlist: null }, { wishlist: { items: [] } }]) {
    const out = buildWishlist(info);
    assert.deepEqual([out.rows.length, out.unmatched.length, out.waiting.length], [0, 0, 0]);
  }
});

test('wishlist rows are sorted like the library: sales first, biggest cut first, then by title', () => {
  const info = { wishlist: { items: [
    item(1, 'Small', { price: price({ cut: 20, price: 1600 }) }),
    item(2, 'Big', { price: price({ cut: 75, price: 500 }) }),
    item(3, 'Full price', { price: price({ cut: 0, price: 2000, low_price: 800 }) }),
    item(4, 'Another big', { price: price({ cut: 75, price: 500 }) }),
  ] } };
  assert.deepEqual(titles(buildWishlist(info).rows), ['Another big', 'Big', 'Small', 'Full price']);
  assert.deepEqual(titles(buildWishlist(info, { sort: 'price' }).rows), ['Another big', 'Big', 'Small', 'Full price']);
  const first = buildWishlist(info).rows[0];
  assert.equal(first.badge, 'low'); // 5.00 is under its recorded low of 8.00
  assert.equal(first.p.cut, 75);
});

test('badges and alerts come through on wishlist rows', () => {
  const info = { wishlist: { items: [
    item(1, 'At the low', { price: price({ price: 800, low_price: 800 }), alert: { kind: 'historical', price: 800, at: '2026-10-02T00:00:00Z', seen: false } }),
    item(2, 'Plain'),
  ] } };
  const { rows } = buildWishlist(info);
  assert.equal(rows[0].badge, 'low');
  assert.equal(rows[0].alert.kind, 'historical');
  assert.equal(rows[1].alert, null);
  assert.deepEqual(titles(buildWishlist(info, { alertsOnly: true }).rows), ['At the low']);
});

test('games without a price are waiting (not read yet) or unmatched (ITAD does not know them)', () => {
  const info = { wishlist: { items: [
    item(1, 'Priced'),
    item(2, 'Zeta waiting', { price: null }),
    item(3, 'Alpha waiting', { price: null, itad_id: '', matched: false }),
    item(4, 'Unknown app', { price: null, itad_id: '', matched: true }),
  ] } };
  const out = buildWishlist(info);
  assert.deepEqual(titles(out.rows), ['Priced']);
  assert.deepEqual(out.waiting.map((r) => r.item.title), ['Alpha waiting', 'Zeta waiting']);
  assert.deepEqual(out.unmatched.map((r) => r.item.title), ['Unknown app']);
  // With the Alerts filter only priced games with an alert remain.
  const only = buildWishlist(info, { alertsOnly: true });
  assert.equal(only.rows.length + only.waiting.length + only.unmatched.length, 0);
});

test('the menu badge counts unseen wishlist alerts too, and the last check looks at wishlist prices', () => {
  const info = {
    alerts: [{ game_id: 1, kind: 'historical', price: 1, at: 'x', seen: false }],
    wishlist: { items: [item(1, 'A', { alert: { kind: 'two_year', price: 1, at: 'x', seen: false } }), item(2, 'B', { alert: { kind: 'two_year', price: 1, at: 'x', seen: true } }), item(3, 'C', { price: price({ fetched_at: '2026-09-01T00:00:00Z' }) })] },
  };
  assert.equal(unseenCount(info), 2);
  assert.equal(unseenCount({ wishlist: null }), 0);
  assert.equal(oldestCheck(info), '2026-09-01T00:00:00Z');
  assert.equal(oldestCheck({ wishlist: { items: [item(1, 'A', { price: null })] } }), null);
});

test('the Steam store link is built from the app id only', () => {
  assert.equal(steamStoreURL(367520), 'https://store.steampowered.com/app/367520');
  assert.equal(steamStoreURL('1/../x'), 'https://store.steampowered.com/app/1%2F..%2Fx');
});
