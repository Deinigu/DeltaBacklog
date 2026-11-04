import test from 'node:test';
import assert from 'node:assert/strict';
import { buildUpcoming } from '../static/js/upcoming.js';
import { WHEN, emptyUpcomingFilter, upcomingFilterCount, whenKeys, filterUpcoming, countUpcoming, allItems, seriesOf, platformsOf, upcomingStateFromQuery, upcomingStateToQuery } from '../static/js/upcomingfilter.js';

const utc = (y, m, d) => Date.UTC(y, m - 1, d) / 1000;
const NOW = new Date(2026, 9, 27, 12, 0, 0); // 27 Oct 2026

let id = 100;
const game = (title, series) => ({ id: ++id, title, series, status: 'backlog', platforms: [], years: [], periods: [], release_year: null });
const miss = (title, date, format, o = {}) => ({ igdb_id: ++id, collection_id: 1, title, year: null, date, format, platforms: null, cover: null, ...o });
const games = [game('Own Zed', 'Zed'), game('Own Hades', 'Hades')];
const index = new Map([
  ['zed', { missing: [
    miss('Zed 3', utc(2026, 10, 30), 0, { platforms: 'PC, PS5' }), // this month
    miss('Zed 4', utc(2026, 12, 1), 0, { platforms: 'PC' }), // next 3 months
    miss('Zed 5', utc(2027, 3, 1), 1, { platforms: 'Switch' }), // later
    miss('Zed Old', utc(2026, 9, 10), 0), // out 47 days ago: recently released
  ], hidden: [], collections: [] }],
  ['hades', { missing: [
    miss('Hades 3', utc(2026, 12, 20), 1), // December 2026: next 3 months and this year
    miss('Hades TBA', null, null),
    miss('Hades Year', utc(2026, 12, 31), 2, { platforms: 'PC' }), // "2026": this year, next 3 months (it covers Nov and Dec) and this month
    miss('Hades Q3', utc(2028, 9, 30), 5), // Q3 2028: later
  ], hidden: [], collections: [] }],
]);
const res = buildUpcoming(games, index, NOW);
const F = (o) => ({ ...emptyUpcomingFilter(), ...o });
const titles = (f) => { const r = filterUpcoming(res, f, NOW); return [...r.recent, ...r.groups.flatMap((g) => g.items)].map((i) => i.title).sort(); };

test('no filter keeps everything', () => {
  const r = filterUpcoming(res, F({}), NOW);
  assert.equal(r.total, res.total);
  assert.equal(r.recent.length, 1);
  assert.deepEqual(r.groups.map((g) => g.key), res.groups.map((g) => g.key));
});

test('search by title or series name', () => {
  assert.deepEqual(titles(F({ q: 'zed 3' })), ['Zed 3']);
  assert.deepEqual(titles(F({ q: 'hades' })), ['Hades 3', 'Hades Q3', 'Hades TBA', 'Hades Year']);
});

test('when: this month, next 3 months, this year, later, TBA', () => {
  const when = (...k) => titles(F({ when: new Set(k) }));
  assert.deepEqual(when('month'), ['Hades Year', 'Zed 3']);
  assert.deepEqual(when('next3'), ['Hades 3', 'Hades Year', 'Zed 4']);
  assert.deepEqual(when('year'), ['Hades 3', 'Hades Year', 'Zed 3', 'Zed 4']);
  assert.deepEqual(when('later'), ['Hades Q3', 'Zed 5']);
  assert.deepEqual(when('tba'), ['Hades TBA']);
  assert.deepEqual(when('month', 'tba'), ['Hades TBA', 'Hades Year', 'Zed 3']);
});

test('a game already out is in no time window (it is only listed as recently released)', () => {
  const old = allItems(res).find((i) => i.title === 'Zed Old');
  assert.equal(whenKeys(old, NOW).size, 0);
  assert.ok(!titles(F({ when: new Set(WHEN.map((w) => w.key)) })).includes('Zed Old'));
  assert.ok(titles(F({})).includes('Zed Old'));
});

test('series and platform: any of the chosen', () => {
  assert.deepEqual(titles(F({ series: new Set(['Zed']) })), ['Zed 3', 'Zed 4', 'Zed 5', 'Zed Old']);
  assert.deepEqual(titles(F({ platforms: new Set(['PC']) })), ['Hades Year', 'Zed 3', 'Zed 4']);
  assert.deepEqual(titles(F({ platforms: new Set(['Switch', 'PS5']) })), ['Zed 3', 'Zed 5']);
  assert.deepEqual(titles(F({ series: new Set(['Hades']), platforms: new Set(['PC']), when: new Set(['month']) })), ['Hades Year']);
});

test('groups left empty are dropped and the total counts the upcoming games shown', () => {
  const r = filterUpcoming(res, F({ when: new Set(['tba']) }), NOW);
  assert.deepEqual(r.groups.map((g) => g.key), ['later']);
  assert.equal(r.total, 1);
  assert.equal(r.recent.length, 0);
});

test('chip counts: the filter with that group set to the chip alone', () => {
  const f = F({ series: new Set(['Hades']), when: new Set(['tba']) });
  assert.equal(countUpcoming(res, f, 'platforms', 'PC', NOW), 0); // series Hades + TBA + PC
  assert.equal(countUpcoming(res, f, 'when', 'month', NOW), 1); // series Hades + this month
  assert.equal(countUpcoming(res, f, 'series', 'Zed', NOW), 0); // Zed + TBA: the only TBA game is Hades's
});

test('options come with counts', () => {
  assert.deepEqual(seriesOf(res), [{ value: 'Hades', count: 4 }, { value: 'Zed', count: 4 }]);
  assert.deepEqual(platformsOf(res), [{ value: 'PC', count: 3 }, { value: 'PS5', count: 1 }, { value: 'Switch', count: 1 }]);
  assert.equal(upcomingFilterCount(F({ q: 'a', when: new Set(['month']), series: new Set(['Zed']), platforms: new Set(['PC', 'PS5']) })), 4);
});

test('the address round-trips and drops what is unknown', () => {
  const f = F({ q: 'zed', when: new Set(['tba', 'month']), series: new Set(['Zed', 'Hades']), platforms: new Set(['PC']) });
  const qs = upcomingStateToQuery({ filter: f });
  assert.equal(qs, 'q=zed&when=month%2Ctba&series=Hades&series=Zed&platform=PC');
  assert.deepEqual(upcomingStateFromQuery(qs).filter, f);
  assert.equal(upcomingStateToQuery({ filter: F({}) }), '');
  assert.equal(upcomingStateFromQuery('when=soon,month').filter.when.size, 1);
});
