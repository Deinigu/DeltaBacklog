import test from 'node:test';
import assert from 'node:assert/strict';
import { TTB_RULE, TTB_STATUSES, ttbState, ttbCoverage, ttbGroupOfStatus } from '../static/js/ttbscope.js';
import { buildGroups, GROUPS } from '../static/js/attention.js';
import { estimate, timeLeft, finishedTime, shortestWins, WAITING } from '../static/js/stats.js';

const game = (id, status, o = {}) => ({
  id, title: `G${String(id).padStart(2, '0')}`, status, cover: 'c.jpg', poster: 'p.jpg', poster_src: 'igdb', poster_user: false, igdb_id: 1, igdb_not_found: false,
  platforms: ['PC'], genres: ['RPG'], release_year: 2020, ownership: 'Owned', years: [], periods: [], added_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  ttb_hastily: null, ttb_normally: null, ttb_completely: null, ttb_not_found: false, ...o });

// finished, dropped, evergreen, backlog, up next, playing; some with times, some without, some with the mark
let n = 0;
const library = [
  ...['finished', 'dropped', 'evergreen', 'playing_evergreen', 'hold', 'backlog', 'up_next', 'playing'].flatMap((status) => [
    game(++n, status, { ttb_normally: 600 }),               // has a time
    game(++n, status),                                      // no time
    game(++n, status, { ttb_not_found: true }),             // no time on HowLongToBeat
    game(++n, status, { ttb_hastily: 120, ttb_not_found: true }), // a time found later: the mark is moot
  ]),
];

test('the rule is the one list: waiting and finished games, never dropped or evergreen', () => {
  assert.deepEqual(TTB_RULE.map((r) => r.status).sort(), ['backlog', 'finished', 'playing', 'up_next']);
  assert.ok(TTB_RULE.every((r) => r.why));
  for (const s of ['dropped', 'evergreen', 'playing_evergreen', 'hold']) assert.ok(!TTB_STATUSES.has(s), s);
  for (const s of WAITING) assert.ok(TTB_STATUSES.has(s), `Stats' waiting status ${s} must be in the rule`);
  assert.equal(ttbGroupOfStatus('finished'), 'no_ttb_finished');
  assert.equal(ttbGroupOfStatus('backlog'), 'no_ttb');
  assert.ok(GROUPS.some((g) => g.key === 'no_ttb') && GROUPS.some((g) => g.key === 'no_ttb_finished'));
});

test('ttbState', () => {
  assert.equal(ttbState(game(1, 'dropped')), null);
  assert.equal(ttbState(game(1, 'finished', { ttb_normally: 60 })), 'known');
  assert.equal(ttbState(game(1, 'finished')), 'missing');
  assert.equal(ttbState(game(1, 'finished', { ttb_not_found: true })), 'none');
  assert.equal(ttbState(game(1, 'finished', { ttb_not_found: true, ttb_completely: 30 })), 'known');
});

// The drift guard: whatever Stats counts as "without a time" must be listed on Needs attention, and nothing else is.
test('Stats and Needs attention agree on which games lack a time', () => {
  const groups = buildGroups(library, [], {}, true).filter((g) => g.key === 'no_ttb' || g.key === 'no_ttb_finished');
  const listed = (pred) => new Set(groups.flatMap((g) => g.rows.filter(pred).map((r) => r.game.id)));
  const listedAll = listed(() => true), listedOpen = listed((r) => !r.ignored), listedMarked = listed((r) => r.ignored);

  // what Stats computes for one game: the finished figure, the left-to-play figure (and Playing, which Needs attention also lists)
  const statsOf = (g) => {
    const parts = [finishedTime([g]).all, timeLeft([g])];
    if (g.status === 'playing') parts.push(estimate([g]));
    return parts.reduce((a, e) => ({ total: a.total + e.total, known: a.known + e.known, missing: a.missing + e.missing, none: a.none + e.none }), { total: 0, known: 0, missing: 0, none: 0 });
  };
  const withoutTime = library.filter((g) => { const s = statsOf(g); return s.total > 0 && s.known === 0; });
  const statsMissing = new Set(withoutTime.filter((g) => statsOf(g).missing === 1).map((g) => g.id));
  const statsNone = new Set(withoutTime.filter((g) => statsOf(g).none === 1).map((g) => g.id));

  assert.deepEqual([...listedAll].sort((a, b) => a - b), withoutTime.map((g) => g.id).sort((a, b) => a - b), 'every game without a time is listed, and only those');
  assert.deepEqual([...listedOpen].sort(), [...statsMissing].sort(), 'open rows = what Stats calls missing');
  assert.deepEqual([...listedMarked].sort(), [...statsNone].sort(), 'ignored rows = what Stats calls "no time exists"');
  // every listed game is one Stats would use (its status is in the rule)
  for (const id of listedAll) assert.ok(TTB_STATUSES.has(library.find((g) => g.id === id).status));
  // each group holds its own statuses
  for (const grp of groups) for (const r of grp.rows) assert.equal(ttbGroupOfStatus(r.game.status), grp.key);

  // counts match, per status area: Stats' totals against the lists
  const fin = estimate(library.filter((g) => g.status === 'finished'));
  const wait = timeLeft(library);
  const count = (key, pred) => groups.find((g) => g.key === key).rows.filter(pred).length;
  assert.equal(count('no_ttb_finished', (r) => !r.ignored), fin.missing);
  assert.equal(count('no_ttb_finished', (r) => r.ignored), fin.none);
  assert.equal(count('no_ttb', (r) => !r.ignored), wait.missing + estimate(library.filter((g) => g.status === 'playing')).missing);
  assert.equal(ttbCoverage(library).missing, [...listedOpen].length);
  assert.equal(ttbCoverage(library).none, [...listedMarked].length);
  assert.equal(ttbCoverage(library).total, ttbCoverage(library).known + ttbCoverage(library).missing + ttbCoverage(library).none);
});

test('Stats adds up only games in the rule', () => {
  const only = (s) => library.filter((g) => g.status === s);
  for (const s of ['dropped', 'evergreen', 'hold', 'playing_evergreen']) assert.deepEqual(estimate(only(s)), { minutes: 0, hours: 0, known: 0, total: 0, missing: 0, none: 0 }, s);
  assert.equal(finishedTime(library).all.total, only('finished').length);
  assert.ok(shortestWins(library, 99).every((g) => TTB_STATUSES.has(g.status)));
  assert.ok(timeLeft(library).total === only('backlog').length + only('up_next').length);
});
