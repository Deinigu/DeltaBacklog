import test from 'node:test';
import assert from 'node:assert/strict';
import { emptyState, matches, FLAGS, stateFromQuery, stateToQuery, stateFromView, filterToJSON, activeCount } from '../static/js/filters.js';
import { emptyUpcomingFilter, upcomingFilterCount, upcomingPasses, upcomingStateFromQuery, upcomingStateToQuery } from '../static/js/upcomingfilter.js';

const g = (o = {}) => ({ id: 1, title: 'Witcher', _search: 'witcher', status: 'finished', platforms: [], genres: [], themes: [], years: [], periods: [], players: [], expansions: [], ...o });
const owned = { id: 1, ownership: 'Owned', status: null };

test('the Library flag "Expansions left to play" matches games with an owned expansion that is not done', () => {
  assert.ok(FLAGS.some((f) => f.key === 'exp_left' && f.label === 'Expansions left to play' && typeof f.test === 'function'));
  const s = emptyState();
  s.flags.add('exp_left');
  assert.equal(matches(s, g({ expansions: [owned] })), true);
  assert.equal(matches(s, g({ expansions: [{ ...owned, status: 'finished' }] })), false);
  assert.equal(matches(s, g({ expansions: [{ ...owned, hidden: true }] })), false);
  assert.equal(matches(s, g()), false);
  assert.equal(matches(s, g({ expansions: undefined })), false, 'an old payload without expansions');
  assert.equal(activeCount(s), 1);
  // the other flags still read the game's own field
  const f = emptyState();
  f.flags.add('favorite');
  assert.equal(matches(f, g({ favorite: true })), true);
  assert.equal(matches(f, g()), false);
});

test('the flag is kept by the address and by saved views; a view saved before it loads as before', () => {
  const s = emptyState();
  s.flags.add('exp_left');
  assert.equal(stateToQuery(s), 'exp_left=1');
  assert.ok(stateFromQuery('exp_left=1').flags.has('exp_left'));
  const json = filterToJSON(s);
  assert.deepEqual(json.flags, ['exp_left']);
  assert.ok(stateFromView({ filter: json }).flags.has('exp_left'));
  const old = stateFromView({ filter: { in: { status: ['playing'] }, flags: ['favorite'] }, sort: [] });
  assert.deepEqual([...old.flags], ['favorite']);
  assert.equal(stateFromView({ filter: { flags: ['nonsense'] } }).flags.size, 0, 'unknown flags are dropped');
});

test('Upcoming: the Expansions chip keeps only expansions, and the search also finds the game', () => {
  const f = emptyUpcomingFilter();
  const exp = { kind: 'expansion', title: 'Hearts', gameTitle: 'Witcher 3', seriesName: null, platforms: ['PC'], period: null };
  const miss = { kind: 'missing', title: 'Zed', seriesName: 'Zed Series', platforms: ['PC'], period: null };
  assert.equal(upcomingPasses(f, exp) && upcomingPasses(f, miss), true);
  f.expansions = true;
  assert.equal(upcomingPasses(f, exp), true);
  assert.equal(upcomingPasses(f, miss), false);
  assert.equal(upcomingFilterCount(f), 1);
  f.q = 'witcher';
  assert.equal(upcomingPasses(f, exp), true, 'the game title is searched');
  assert.equal(upcomingStateToQuery({ filter: f }), 'q=witcher&kind=expansion');
  const back = upcomingStateFromQuery('q=witcher&kind=expansion').filter;
  assert.deepEqual([back.q, back.expansions], ['witcher', true]);
  assert.equal(upcomingStateFromQuery('kind=other').filter.expansions, false);
});
