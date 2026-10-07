import test from 'node:test';
import assert from 'node:assert/strict';
import { playedBadges, playedSummary } from '../static/js/years.js';

const g = (years, periods = []) => ({ years, periods });

test('a few years are all shown, oldest first', () => {
  const b = playedBadges(g([2025, 2026]));
  assert.deepEqual(b.years, [{ year: 2025 }, { year: 2026 }]);
  assert.equal(b.earlier, 0);
});

test('only the most recent years fit; older ones are counted', () => {
  const b = playedBadges(g([2014, 2016, 2019, 2024, 2025, 2026]), 3);
  assert.deepEqual(b.years.map((y) => y.year), [2024, 2025, 2026]);
  assert.equal(b.earlier, 3);
});

test('years are sorted and de-duplicated even if the data is not', () => {
  const b = playedBadges(g([2026, 2020, 2020, 2022]), 3);
  assert.deepEqual(b.years.map((y) => y.year), [2020, 2022, 2026]);
  assert.equal(b.earlier, 0);
});

test('periods become icons; unknown periods are ignored', () => {
  const b = playedBadges(g([], ['childhood', 'always', 'someday']));
  assert.deepEqual(b.periods.map((p) => p.key), ['childhood', 'always']);
  assert.ok(b.periods.every((p) => p.icon && p.label));
});

test('a game never played has nothing to show', () => {
  assert.deepEqual(playedBadges(g([])), { years: [], earlier: 0, periods: [] });
});

test('summary sentence', () => {
  assert.equal(playedSummary(g([2025, 2019], ['childhood'])), 'Played in 2019, 2025 and as a child');
  assert.equal(playedSummary(g([], ['always'])), 'Played on and off');
  assert.equal(playedSummary(g([])), 'Not played yet');
});
