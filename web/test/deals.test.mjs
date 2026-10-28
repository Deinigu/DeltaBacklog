import test from 'node:test';
import assert from 'node:assert/strict';
import { formatMoney, formatDate, badgeOf, buildDeals, oldestCheck, safeLink, priceLine, priceOf, matchOf, isWatched } from '../static/js/deals.js';

let n = 0;
const game = (title, status = 'backlog') => ({ id: ++n, title, status });
const price = (g, o = {}) => ({ game_id: g.id, currency: 'EUR', shop: 'Steam', price: 1000, regular: 2000, cut: 50, url: 'https://shop.example/', low_price: 800, low_shop: 'GOG', low_at: '2025-11-27T10:00:00Z', fetched_at: '2026-10-01T10:00:00Z', ...o });
const titles = (rows) => rows.map((r) => r.game.title);

test('money is formatted in en-GB with the price’s own currency', () => {
  assert.equal(formatMoney(749, 'EUR'), '€7.49');
  assert.equal(formatMoney(1999, 'GBP'), '£19.99');
  assert.equal(formatMoney(5, 'EUR'), '€0.05');
  assert.equal(formatMoney(null, 'EUR'), '');
  assert.equal(formatMoney(749, 'not a code'), '7.49 not a code');
  assert.equal(formatDate('2025-11-27T10:00:00Z'), '27 Nov 2025');
  assert.equal(formatDate(null), '');
  assert.equal(formatDate('garbage'), '');
});

test('badges: at the low, within 10% of it, or none', () => {
  assert.equal(badgeOf({ price: 800, low_price: 800 }), 'low');
  assert.equal(badgeOf({ price: 700, low_price: 800 }), 'low'); // under a recorded low (the low was in another currency or shop)
  assert.equal(badgeOf({ price: 880, low_price: 800 }), 'near'); // exactly +10%
  assert.equal(badgeOf({ price: 881, low_price: 800 }), null);
  assert.equal(badgeOf({ price: null, low_price: 800 }), null);
  assert.equal(badgeOf({ price: 800, low_price: null }), null);
  assert.equal(badgeOf(null), null);
});

test('sorted by discount: sales first (biggest cut), then near the low, then the rest', () => {
  const a = game('A'), b = game('B'), c = game('C'), d = game('D'), e = game('E');
  const info = {
    prices: [
      price(a, { cut: 20, price: 1600 }),
      price(b, { cut: 75, price: 500 }),
      price(c, { cut: 0, price: 810, low_price: 800 }), // not on sale, but 1% over its low
      price(d, { cut: 0, price: 2000, low_price: 800 }),
      price(e, { cut: 50, price: 1000, low_price: 500 }),
    ],
    matches: [],
  };
  const { rows } = buildDeals([a, b, c, d, e], info);
  assert.deepEqual(titles(rows), ['B', 'E', 'A', 'C', 'D']);
  assert.equal(rows[0].badge, 'low'); // 5.00 is under its recorded 8.00 low
  assert.equal(rows.find((r) => r.game === c).badge, 'near');
});

test('sorted by closeness to the low, and by price', () => {
  const a = game('A'), b = game('B'), c = game('C');
  const info = { prices: [price(a, { price: 900, low_price: 800, cut: 10 }), price(b, { price: 800, low_price: 800, cut: 0 }), price(c, { price: 100, low_price: null, cut: 0 })], matches: [] };
  assert.deepEqual(titles(buildDeals([a, b, c], info, { sort: 'low' }).rows), ['B', 'A', 'C']);
  assert.deepEqual(titles(buildDeals([a, b, c], info, { sort: 'price' }).rows), ['C', 'B', 'A']);
  assert.deepEqual(titles(buildDeals([a, b, c], info, { sort: 'bogus' }).rows), ['A', 'B', 'C'], 'an unknown sort falls back to the discount sort');
});

test('a game nobody sells right now still lists, after the ones on sale', () => {
  const a = game('A'), b = game('B');
  const info = { prices: [price(a, { price: null, regular: null, cut: null, shop: null, url: null }), price(b, { cut: 10 })], matches: [] };
  const { rows } = buildDeals([a, b], info);
  assert.deepEqual(titles(rows), ['B', 'A']);
  assert.equal(rows[1].badge, null);
});

test('only Wishlist, Backlog and Up next; the status chips filter', () => {
  const w = game('W', 'wishlist'), b = game('B', 'backlog'), u = game('U', 'up_next'), f = game('F', 'finished'), p = game('P', 'playing');
  const info = { prices: [w, b, u, f, p].map((g) => price(g)), matches: [] };
  assert.deepEqual(titles(buildDeals([w, b, u, f, p], info).rows).sort(), ['B', 'U', 'W']);
  assert.deepEqual(titles(buildDeals([w, b, u, f, p], info, { statuses: new Set(['wishlist']) }).rows), ['W']);
  assert.deepEqual(titles(buildDeals([w, b, u, f, p], info, { statuses: new Set(['wishlist', 'up_next']) }).rows).sort(), ['U', 'W']);
  assert.deepEqual(titles(buildDeals([w, b, u, f, p], info, { statuses: new Set(['finished']) }).rows), [], 'a status that gets no prices shows nothing');
  assert.equal(titles(buildDeals([w, b, u], info, { statuses: new Set() }).rows).length, 3, 'an empty selection means all');
});

test('games without a price are told apart: no match, and matched but not read yet', () => {
  const a = game('A'), b = game('B'), c = game('C'), d = game('D');
  const info = { prices: [price(a)], matches: [{ game_id: b.id, itad_id: '', source: 'title' }, { game_id: c.id, itad_id: 'x', source: 'steam' }] };
  const r = buildDeals([a, b, c, d], info);
  assert.deepEqual(titles(r.rows), ['A']);
  assert.deepEqual(r.unmatched.map((x) => x.game.title), ['B']);
  assert.deepEqual(r.waiting.map((x) => x.game.title), ['C']); // D was never looked at: it is in none of the lists
});

test('lookups, the oldest check and links', () => {
  const a = game('A');
  const info = { prices: [price(a, { fetched_at: '2026-10-05T00:00:00Z' }), price({ id: 999 }, { fetched_at: '2026-10-01T00:00:00Z' })], matches: [{ game_id: a.id, itad_id: 'x' }] };
  assert.equal(priceOf(info, a.id).fetched_at, '2026-10-05T00:00:00Z');
  assert.equal(priceOf(info, 12345), null);
  assert.equal(matchOf(info, a.id).itad_id, 'x');
  assert.equal(oldestCheck(info), '2026-10-01T00:00:00Z');
  assert.equal(oldestCheck({ prices: [] }), null);
  assert.equal(oldestCheck(null), null);
  assert.equal(safeLink('https://store.steampowered.com/app/1'), 'https://store.steampowered.com/app/1');
  assert.equal(safeLink('javascript:alert(1)'), null);
  assert.equal(safeLink('data:text/html,x'), null);
  assert.equal(safeLink('not a url'), null);
  assert.equal(safeLink(null), null);
  assert.ok(isWatched({ status: 'wishlist' }) && !isWatched({ status: 'finished' }));
});

test('the price line', () => {
  assert.equal(priceLine(price(game('A'), { price: 749, regular: 1499, cut: 50 })), '€7.49 at Steam (-50%, was €14.99)');
  assert.equal(priceLine(price(game('A'), { price: 1499, regular: 1499, cut: 0 })), '€14.99 at Steam');
  assert.equal(priceLine(price(game('A'), { price: null, cut: null, shop: null })), 'No shop lists it right now');
  assert.equal(priceLine(price(game('A'), { price: null, low_price: null, shop: null })), 'No price found');
  assert.equal(priceLine(null), 'No price found');
});
