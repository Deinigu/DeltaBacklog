import test from 'node:test';
import assert from 'node:assert/strict';
import { releasePeriod, formatRelease, isReleased, today, buildUpcoming } from '../static/js/upcoming.js';

const utc = (y, m, d) => Date.UTC(y, m - 1, d) / 1000;
const NOW = new Date(2026, 9, 27, 12, 0, 0); // 27 Oct 2026, noon on the viewer's clock

let n = 0;
const game = (title, o = {}) => ({ id: ++n, title, status: 'backlog', series: null, platforms: [], release_date: null, release_format: null, years: [], periods: [], release_year: null, ...o });
const wish = (title, date, format, o = {}) => game(title, { status: 'wishlist', release_date: date, release_format: format, ...o });
const miss = (id, title, o = {}) => ({ igdb_id: id, collection_id: 1, title, year: null, date: null, format: null, platforms: null, cover: null, ...o });

test('precision is kept: a year release is "2027", never a day', () => {
  assert.equal(formatRelease(utc(2027, 12, 31), 2, 'en'), '2027');
  assert.equal(formatRelease(utc(2027, 5, 1), 1, 'en'), 'May 2027');
  assert.equal(formatRelease(utc(2027, 9, 30), 5, 'en'), 'Q3 2027');
  assert.equal(formatRelease(utc(2027, 3, 4), 0, 'en'), 'Mar 4, 2027');
  assert.equal(formatRelease(null, null, 'en'), 'TBA');
  assert.equal(formatRelease(0, 0, 'en'), 'TBA');
});

test('periods: bounds of a month, quarter and year', () => {
  const m = releasePeriod(utc(2028, 2, 1), 1);
  assert.deepEqual([m.precision, m.month, m.endDay - m.startDay], ['month', 2, 28]); // 2028 is a leap year: 29 days
  const q = releasePeriod(utc(2027, 6, 30), 4);
  assert.deepEqual([q.precision, q.quarter, q.month, q.endMonth], ['quarter', 2, 4, 6]);
  const y = releasePeriod(utc(2027, 12, 31), 2);
  assert.equal(y.endDay - y.startDay, 364);
  assert.equal(releasePeriod(utc(2027, 1, 1), 99).precision, 'day'); // a format IGDB adds later: an exact day
});

test('released: a day counts on the day itself, a vaguer period only once it is over', () => {
  const t = today(NOW);
  assert.ok(isReleased(releasePeriod(utc(2026, 10, 27), 0), t));
  assert.ok(!isReleased(releasePeriod(utc(2026, 10, 28), 0), t));
  assert.ok(!isReleased(releasePeriod(utc(2026, 10, 1), 1), t)); // October 2026 is not over
  assert.ok(isReleased(releasePeriod(utc(2026, 9, 1), 1), t));
  assert.ok(!isReleased(releasePeriod(utc(2026, 12, 31), 2), t));
});

test('months: This month, Next month, then named months in time order; Later / TBA last', () => {
  const games = [
    wish('Dec', utc(2026, 12, 1), 0), wish('Soon', utc(2026, 10, 30), 0), wish('Nov B', utc(2026, 11, 20), 0), wish('Nov A', utc(2026, 11, 5), 0),
    wish('Unknown', null, null),
    game('Owned, not wishlist', { release_date: utc(2026, 11, 5), release_format: 0 }),
  ];
  const { groups, recent } = buildUpcoming(games, new Map(), NOW, { locale: 'en' });
  assert.deepEqual(groups.map((g) => g.label), ['This month', 'Next month', 'December 2026', 'Later / TBA']);
  assert.deepEqual(groups[1].items.map((i) => i.title), ['Nov A', 'Nov B']);
  assert.deepEqual(groups[3].items.map((i) => i.title), ['Unknown']);
  assert.equal(recent.length, 0);
});

test('a quarter or a year sits after the months it covers; a far release goes to Later', () => {
  const games = [wish('Year', utc(2027, 12, 31), 2), wish('Q1', utc(2027, 3, 31), 3), wish('Mar', utc(2027, 3, 10), 0), wish('Jan', utc(2027, 1, 15), 1), wish('Far', utc(2030, 5, 5), 0)];
  const { groups } = buildUpcoming(games, new Map(), NOW, { locale: 'en' });
  assert.deepEqual(groups.map((g) => g.label), ['January 2027', 'March 2027', 'Q1 2027', '2027', 'Later / TBA']);
  assert.deepEqual(groups[4].items.map((i) => i.title), ['Far']);
});

test('recently released: the last 60 days, newest first; older ones are not listed', () => {
  const games = [
    wish('Old', utc(2026, 8, 1), 0),        // 87 days ago
    wish('Edge', utc(2026, 8, 28), 0),      // exactly 60 days ago
    wish('Last week', utc(2026, 10, 20), 0),
    wish('Today', utc(2026, 10, 27), 0),
    wish('Last month', utc(2026, 9, 10), 1), // September is over, its end was 27 days ago
  ];
  const { groups, recent } = buildUpcoming(games, new Map(), NOW);
  assert.deepEqual(recent.map((i) => i.title), ['Today', 'Last week', 'Last month', 'Edge']);
  assert.equal(groups.length, 0);
});

test('games of the series the library lacks join the wishlist; no duplicates; the series name is the user’s', () => {
  const games = [game('Zed', { series: 'Zeds' }), wish('Wish', utc(2026, 11, 2), 0, { series: 'Zeds' })];
  const index = new Map([
    ['zeds', { missing: [miss(1, 'Zed II', { date: utc(2026, 11, 9), format: 0, platforms: 'PC, PS5' }), miss(2, 'Zed TBA')], hidden: [], collections: [] }],
    ['other', { missing: [miss(2, 'Zed TBA')], hidden: [], collections: [] }],
  ]);
  const { groups, total } = buildUpcoming(games, index, NOW, { locale: 'en' });
  assert.deepEqual(groups.map((g) => g.label), ['Next month', 'Later / TBA']);
  const next = groups[0].items;
  assert.deepEqual(next.map((i) => [i.title, i.kind, i.seriesName]), [['Wish', 'owned', 'Zeds'], ['Zed II', 'missing', 'Zeds']]);
  assert.deepEqual(next[1].platforms, ['PC', 'PS5']);
  assert.deepEqual(groups[1].items.map((i) => i.title), ['Zed TBA']);
  assert.equal(total, 3);
});

test('time zones: months follow the UTC date of the release, "today" the viewer’s own calendar date', () => {
  const games = [wish('First', utc(2026, 12, 1), 0), wish('Last', utc(2026, 11, 30), 0)];
  const original = process.env.TZ;
  try {
    for (const tz of ['UTC', 'America/Los_Angeles', 'Pacific/Kiritimati', 'Asia/Tokyo']) {
      process.env.TZ = tz;
      const now = new Date(2026, 9, 27, 12); // local noon in that zone
      const { groups } = buildUpcoming(games, new Map(), now, { locale: 'en' });
      assert.deepEqual(groups.map((g) => [g.label, g.items.map((i) => i.title)]), [['Next month', ['Last']], ['December 2026', ['First']]], tz);
      assert.equal(formatRelease(utc(2026, 12, 1), 0, 'en'), 'Dec 1, 2026', tz);
    }
    // 23:30 on 31 Oct in Los Angeles is already 1 Nov in UTC, but the viewer's date is still the 31st
    process.env.TZ = 'America/Los_Angeles';
    const late = new Date(2026, 9, 31, 23, 30);
    const g2 = [wish('Halloween', utc(2026, 10, 31), 0), wish('Nov', utc(2026, 11, 1), 0)];
    const r = buildUpcoming(g2, new Map(), late, { locale: 'en' });
    assert.deepEqual(r.recent.map((i) => i.title), ['Halloween']);
    assert.deepEqual(r.groups.map((g) => g.label), ['Next month']);
  } finally {
    if (original === undefined) delete process.env.TZ; else process.env.TZ = original;
  }
});
