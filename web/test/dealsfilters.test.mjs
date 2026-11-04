import test from 'node:test';
import assert from 'node:assert/strict';
import {
  emptyDealsFilter, buildDeals, buildWishlist, rowPasses, lowKindsOf, endsWithin, priceFilterActive, dealsFilterCount, storesOf,
  dealsStateFromQuery, dealsStateToQuery, titleMatches,
} from '../static/js/deals.js';

const NOW = new Date('2026-11-04T12:00:00Z');
const day = (n) => new Date(NOW.getTime() + n * 86400000).toISOString();

let n = 0;
const game = (title, status = 'backlog') => ({ id: ++n, title, status });
const price = (g, o = {}) => ({ game_id: g.id, currency: 'EUR', shop: 'Steam', price: 1000, regular: 2000, cut: 50, low_price: 800, fetched_at: '2026-11-01T10:00:00Z', ...o });
const titles = (rows) => rows.map((r) => r.game.title);
const f = (o = {}) => ({ ...emptyDealsFilter(), ...o });

// Five library games with different prices, shops, lows and end dates.
const a = game('Alpha'), b = game('Bravo', 'up_next'), c = game('Charlie'), d = game('Délta'), e = game('Echo');
const info = {
  watched: [a, b, c, d, e].map((g) => g.id),
  prices: [
    price(a, { price: 400, regular: 2000, cut: 80, low_price: 400, expiry: day(2) }), // at the historical low, ends in 2 days
    price(b, { price: 1500, regular: 3000, cut: 50, low_price: 1000, shop: 'GOG', expiry: day(10) }),
    price(c, { price: 2500, regular: 2500, cut: 0, low_price: 2500, shop: 'GOG' }), // never discounted
    price(d, { price: 1050, regular: 2000, cut: 47, low_price: 1000, shop: 'Epic Game Store', expiry: day(-1) }), // near the low, ended yesterday
    price(e, { price: 3900, regular: 6000, cut: 35, low_price: 2000, expiry: day(3) }),
  ],
  alerts: [{ game_id: e.id, kind: 'two_year', seen: true }],
  matches: [],
};
const run = (filter, extra = {}) => titles(buildDeals([a, b, c, d, e], info, { filter, now: NOW, sort: 'name', ...extra }).rows);

test('no filter lists every game with a price', () => {
  assert.deepEqual(run(f()), ['Alpha', 'Bravo', 'Charlie', 'Délta', 'Echo']);
});

test('on sale only drops a game at its full price', () => {
  assert.deepEqual(run(f({ sale: true })), ['Alpha', 'Bravo', 'Délta', 'Echo']);
});

test('minimum discount', () => {
  assert.deepEqual(run(f({ minCut: 25 })), ['Alpha', 'Bravo', 'Délta', 'Echo']);
  assert.deepEqual(run(f({ minCut: 50 })), ['Alpha', 'Bravo']);
  assert.deepEqual(run(f({ minCut: 75 })), ['Alpha']);
});

test('maximum price is in whole currency units and includes the limit', () => {
  assert.deepEqual(run(f({ maxPrice: 5 })), ['Alpha']);
  assert.deepEqual(run(f({ maxPrice: 10 })), ['Alpha']);
  assert.deepEqual(run(f({ maxPrice: 20 })), ['Alpha', 'Bravo', 'Délta']);
  assert.deepEqual(run(f({ maxPrice: 40 })), ['Alpha', 'Bravo', 'Charlie', 'Délta', 'Echo']);
  assert.equal(rowPasses(f({ maxPrice: 10 }), { game: a, p: price(a, { price: 1000 }) }), true);
  assert.equal(rowPasses(f({ maxPrice: 10 }), { game: a, p: price(a, { price: 1001 }) }), false);
});

test('low types: historical, lowest in 2 years, near', () => {
  assert.deepEqual(run(f({ lows: new Set(['historical']) })), ['Alpha']);
  assert.deepEqual(run(f({ lows: new Set(['two_year']) })), ['Echo']);
  assert.deepEqual(run(f({ lows: new Set(['near']) })), ['Délta']);
  assert.deepEqual(run(f({ lows: new Set(['historical', 'near']) })), ['Alpha', 'Délta']);
  assert.deepEqual([...lowKindsOf({ badge: 'low', alert: { kind: 'two_year' } })].sort(), ['historical', 'two_year']);
  assert.equal(lowKindsOf({ badge: null, alert: null }).size, 0);
});

test('alerts only', () => {
  assert.deepEqual(run(f({ alerts: true })), ['Echo']);
});

test('stores', () => {
  assert.deepEqual(run(f({ stores: new Set(['GOG']) })), ['Bravo', 'Charlie']);
  assert.deepEqual(run(f({ stores: new Set(['GOG', 'Epic Game Store']) })), ['Bravo', 'Charlie', 'Délta']);
  assert.deepEqual(storesOf(buildDeals([a, b, c, d, e], info, { now: NOW }).rows), [
    { name: 'GOG', count: 2 }, { name: 'Steam', count: 2 }, { name: 'Epic Game Store', count: 1 }]);
});

test('ends within 3 days: not past, not later, not without a date', () => {
  assert.deepEqual(run(f({ ends: true })), ['Alpha', 'Echo']);
  assert.equal(endsWithin({ price: 1, expiry: day(3) }, NOW), true);
  assert.equal(endsWithin({ price: 1, expiry: day(3.01) }, NOW), false);
  assert.equal(endsWithin({ price: 1, expiry: day(-0.01) }, NOW), false);
  assert.equal(endsWithin({ price: 1, expiry: null }, NOW), false);
  assert.equal(endsWithin({ price: null, expiry: day(1) }, NOW), false);
  assert.equal(endsWithin({ price: 1, expiry: 'garbage' }, NOW), false);
});

test('status filter', () => {
  assert.deepEqual(run(f({ statuses: new Set(['up_next']) })), ['Bravo']);
  assert.deepEqual(run(f({ statuses: new Set(['backlog']) })), ['Alpha', 'Charlie', 'Délta', 'Echo']);
});

test('title search ignores case and accents and wants every word', () => {
  assert.deepEqual(run(f({ q: 'delta' })), ['Délta']);
  assert.deepEqual(run(f({ q: 'ALP' })), ['Alpha']);
  assert.equal(titleMatches('hollow knight', 'Hollow Knight: Silksong'), true);
  assert.equal(titleMatches('hollow zelda', 'Hollow Knight'), false);
  assert.equal(titleMatches('', 'Anything'), true);
});

test('filters combine', () => {
  assert.deepEqual(run(f({ sale: true, minCut: 50, stores: new Set(['Steam']) })), ['Alpha']);
  assert.deepEqual(run(f({ maxPrice: 20, stores: new Set(['GOG']) })), ['Bravo']);
  assert.deepEqual(run(f({ minCut: 75, maxPrice: 5, lows: new Set(['historical']), ends: true })), ['Alpha']);
  assert.deepEqual(run(f({ minCut: 75, stores: new Set(['GOG']) })), []);
});

test('"added" does nothing in the library list (its games have no added date)', () => {
  assert.deepEqual(run(f({ added: true })), ['Alpha', 'Bravo', 'Charlie', 'Délta', 'Echo']);
});

test('the older options still work', () => {
  assert.deepEqual(titles(buildDeals([a, b, c, d, e], info, { statuses: new Set(['up_next']), now: NOW }).rows), ['Bravo']);
  assert.deepEqual(titles(buildDeals([a, b, c, d, e], info, { alertsOnly: true, now: NOW }).rows), ['Echo']);
});

test('a filter only a price can pass leaves the games without a price out of the other sections', () => {
  const x = game('Xeno'), y = game('Yoyo'), z = game('Zed');
  const inf = { watched: [x.id, y.id, z.id], prices: [], matches: [{ game_id: x.id, itad_id: null }, { game_id: y.id, itad_id: 'abc' }], alerts: [] };
  const all = buildDeals([x, y, z], inf, { now: NOW });
  assert.deepEqual([all.unmatched.length, all.waiting.length], [1, 1]);
  const sale = buildDeals([x, y, z], inf, { filter: f({ sale: true }), now: NOW });
  assert.deepEqual([sale.unmatched.length, sale.waiting.length], [0, 0]);
  const q = buildDeals([x, y, z], inf, { filter: f({ q: 'yoy' }), now: NOW });
  assert.deepEqual([q.unmatched.length, q.waiting.length], [0, 1]); // the search alone still shows the matching ones
  assert.equal(priceFilterActive(f()), false);
  assert.equal(priceFilterActive(f({ q: 'x', statuses: new Set(['backlog']) })), false);
  assert.equal(priceFilterActive(f({ ends: true })), true);
  assert.equal(priceFilterActive(f({ added: true })), false);
  assert.equal(priceFilterActive(f({ added: true }), true), true);
});

test('the filter count for the badge', () => {
  assert.equal(dealsFilterCount(f()), 0);
  assert.equal(dealsFilterCount(f({ q: 'zelda' })), 0);
  assert.equal(dealsFilterCount(f({ sale: true, minCut: 50, maxPrice: 10, lows: new Set(['near', 'historical']), alerts: true, stores: new Set(['GOG']), ends: true, statuses: new Set(['backlog']) })), 9);
  // the wishlist has no status, and only it has "added"
  assert.equal(dealsFilterCount(f({ statuses: new Set(['backlog']), added: true }), true), 1);
  assert.equal(dealsFilterCount(f({ statuses: new Set(['backlog']), added: true }), false), 1);
});

// ---- the wishlist ----
const wish = (appid, title, p, o = {}) => ({ appid, title, matched: true, itad_id: 'x', added_at: day(-5), price: p, alert: null, ...o });
const wp = (o = {}) => ({ currency: 'EUR', shop: 'Steam', price: 1000, regular: 2000, cut: 50, low_price: 800, ...o });
const winfo = { wishlist: { items: [
  wish(1, 'Old wish', wp({ price: 3000, cut: 0, regular: 3000 }), { added_at: day(-60) }),
  wish(2, 'New wish', wp({ cut: 75, price: 500, low_price: 500, expiry: day(1) }), { added_at: day(-3) }),
  wish(3, 'Alert wish', wp({ price: 1500 }), { added_at: day(-29), alert: { kind: 'historical', seen: true } }),
  wish(4, 'Nowhere', null, { matched: true, itad_id: null }),
  wish(5, 'Waiting', null, { matched: false }),
] } };
const wrun = (filter) => buildWishlist(winfo, { filter, now: NOW, sort: 'name' });

test('wishlist: the same filters, and added in the last 30 days', () => {
  assert.deepEqual(wrun(f()).rows.map((r) => r.item.title), ['Alert wish', 'New wish', 'Old wish']);
  assert.deepEqual(wrun(f({ added: true })).rows.map((r) => r.item.title), ['Alert wish', 'New wish']);
  assert.deepEqual(wrun(f({ sale: true, minCut: 75 })).rows.map((r) => r.item.title), ['New wish']);
  assert.deepEqual(wrun(f({ ends: true })).rows.map((r) => r.item.title), ['New wish']);
  assert.deepEqual(wrun(f({ alerts: true })).rows.map((r) => r.item.title), ['Alert wish']);
  assert.deepEqual(wrun(f({ maxPrice: 10 })).rows.map((r) => r.item.title), ['New wish']);
  assert.deepEqual(wrun(f({ q: 'wish alert' })).rows.map((r) => r.item.title), ['Alert wish']);
});

test('wishlist: a status filter does not apply (no wishlist game has a status)', () => {
  assert.equal(wrun(f({ statuses: new Set(['backlog']) })).rows.length, 3);
});

test('wishlist: added is exactly 30 days', () => {
  const row = (days) => ({ item: { title: 't', added_at: day(-days) }, p: wp() });
  assert.equal(rowPasses(f({ added: true }), row(30), NOW), true);
  assert.equal(rowPasses(f({ added: true }), row(31), NOW), false);
  assert.equal(rowPasses(f({ added: true }), { item: { title: 't', added_at: null }, p: wp() }, NOW), false);
});

test('wishlist: the unmatched and waiting sections follow the search and vanish under a price filter', () => {
  const all = wrun(f());
  assert.deepEqual([all.unmatched.length, all.waiting.length], [1, 1]);
  const q = wrun(f({ q: 'nowh' }));
  assert.deepEqual([q.unmatched.length, q.waiting.length], [1, 0]);
  const sale = wrun(f({ sale: true }));
  assert.deepEqual([sale.unmatched.length, sale.waiting.length], [0, 0]);
});

// ---- the address ----
const parse = (s) => dealsStateFromQuery(s);

test('the default page has an empty query and an empty query gives the default page', () => {
  const s = parse('');
  assert.equal(s.view, 'library');
  assert.equal(s.sort, 'cut');
  assert.equal(s.dir, 1);
  assert.equal(dealsStateToQuery(s), '');
  assert.equal(dealsFilterCount(s.filter), 0);
});

test('a state survives the round trip through the address', () => {
  const filter = f({ q: 'zelda', statuses: new Set(['up_next', 'backlog']), sale: true, minCut: 50, maxPrice: 10, lows: new Set(['near', 'historical']),
    alerts: true, stores: new Set(['Steam', 'GOG']), ends: true, added: true });
  const state = { view: 'wishlist', filter, sort: 'price', dir: -1 };
  const qs = dealsStateToQuery(state);
  assert.equal(qs, 'view=wishlist&q=zelda&status=backlog%2Cup_next&sale=1&min=50&max=10&low=historical%2Cnear&alerts=1&store=GOG&store=Steam&ends=1&added=1&sort=price&dir=-1');
  const back = parse(qs);
  assert.equal(back.view, 'wishlist');
  assert.equal(back.sort, 'price');
  assert.equal(back.dir, -1);
  assert.deepEqual(back.filter, filter);
  assert.equal(dealsStateToQuery(back), qs); // stable
});

test('a hand-written address is read leniently and unknown values are dropped', () => {
  const s = parse('?view=nope&status=backlog,bogus&min=60&max=7&low=near,bogus&sale=yes&sort=weird&dir=2&store=&store=Steam');
  assert.equal(s.view, 'library');
  assert.equal(s.sort, 'cut');
  assert.equal(s.dir, 1);
  assert.deepEqual([...s.filter.statuses], ['backlog']);
  assert.equal(s.filter.minCut, 0);
  assert.equal(s.filter.maxPrice, 0);
  assert.deepEqual([...s.filter.lows], ['near']);
  assert.equal(s.filter.sale, false);
  assert.deepEqual([...s.filter.stores], ['Steam']);
});

test('the search is normalised and a shop with a comma or an ampersand survives', () => {
  assert.equal(parse('q=Pok%C3%A9mon').filter.q, 'pokemon');
  const filter = f({ stores: new Set(['Games, Inc & Co']) });
  const qs = dealsStateToQuery({ view: 'library', filter });
  assert.deepEqual([...parse(qs).filter.stores], ['Games, Inc & Co']);
});

test('sorts: every sort key is accepted, the default is not written', () => {
  for (const key of ['cut', 'low', 'price', 'name', 'lowest', 'shop', 'ends', 'added']) assert.equal(parse(`sort=${key}`).sort, key);
  assert.equal(dealsStateToQuery({ view: 'library', filter: f(), sort: 'cut', dir: 1 }), '');
  assert.equal(dealsStateToQuery({ view: 'library', filter: f(), sort: 'name', dir: 1 }), 'sort=name');
});
