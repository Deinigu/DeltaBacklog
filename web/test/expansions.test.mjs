import test from 'node:test';
import assert from 'node:assert/strict';
import { visible, isOwned, isReleased, isAwaited, leftToPlay, hasLeftToPlay, expansionRowLabel, expansionBadge, attentionRows, upcomingExpansionItems, expansionStats, expansionsOfYear,
  countExpansionsOfYear, expansionYears, expansionCoverURL, withYear, finishedYear, isStale, expansionsCell, FRESH_DAYS } from '../static/js/expansions.js';
import { releasePeriod, buildUpcoming } from '../static/js/upcoming.js';
import { GROUPS } from '../static/js/yearreview.js';
import { buildGroups, GROUPS as ATT, totalOpen, ignoredTotal } from '../static/js/attention.js';

const x = (id, o = {}) => ({ id, game_id: 1, title: 'Hearts of Stone', year: 2015, date: 1444694400, status: null, ownership: null, years: [], periods: [], ...o });
const game = (o = {}, exps = []) => ({ id: 1, title: 'The Witcher 3', status: 'finished', platforms: ['PC'], years: [2016], periods: [], expansions: exps, ...o });
// a game with nothing else for Needs attention to complain about
const clean = (g) => ({ ...g, cover: 'c', poster: 'p', poster_src: 'igdb', igdb_id: 1, genres: ['x'], release_year: 2020, ownership: 'Owned', ttb_normally: 60 });

test('owned means any ownership but Nope; left to play needs an owned, not-closed expansion', () => {
  assert.equal(isOwned(x(1, { ownership: 'Owned' })), true);
  assert.equal(isOwned(x(1, { ownership: 'Borrowed' })), true);
  assert.equal(isOwned(x(1, { ownership: 'Nope' })), false);
  assert.equal(isOwned(x(1, { ownership: ' nope ' })), false);
  assert.equal(isOwned(x(1)), false);
  assert.equal(leftToPlay(x(1, { ownership: 'Owned' })), true, 'no status = not started');
  for (const st of ['backlog', 'playing', 'hold', 'up_next', 'evergreen']) assert.equal(leftToPlay(x(1, { ownership: 'Owned', status: st })), true, st);
  for (const st of ['finished', 'dropped', 'not_interested']) assert.equal(leftToPlay(x(1, { ownership: 'Owned', status: st })), false, st);
  assert.equal(leftToPlay(x(1, { status: 'backlog' })), false, 'tracked but not owned');
});

test('hidden expansions never count', () => {
  const g = game({}, [x(1, { ownership: 'Owned', hidden: true }), x(2, { ownership: 'Owned', status: 'finished' })]);
  assert.equal(visible(g).length, 1);
  assert.equal(hasLeftToPlay(g), false);
  assert.equal(expansionBadge(g).text, '1/1 ✓');
  assert.deepEqual(visible({}), []);
});

test('the badge: finished / owned or tracked', () => {
  assert.equal(expansionBadge(game()), null);
  assert.equal(expansionBadge(game({}, [x(1)])), null, 'untracked and not owned: nothing to count');
  assert.equal(expansionBadge(game({}, [x(1, { ownership: 'Nope' })])), null);
  const g = game({}, [x(1, { status: 'finished', ownership: 'Owned' }), x(2, { ownership: 'Owned' }), x(3, { status: 'backlog' }), x(4)]);
  const b = expansionBadge(g);
  assert.deepEqual([b.finished, b.total, b.text, b.label], [1, 3, '1/3 ✓', '1 of 3 expansions finished']);
  assert.equal(expansionBadge(game({}, [x(1, { status: 'finished' })])).text, '1/1 ✓', 'finished but never marked owned still counts');
});

test('Needs attention: finished base game, owned expansion, not done', () => {
  const a = game({ id: 1, title: 'Alpha' }, [x(10, { game_id: 1, ownership: 'Owned', date: 200 }), x(11, { game_id: 1, ownership: 'Owned', status: 'finished' }), x(12, { game_id: 1, ownership: 'Owned', date: 100, title: 'Early' })]);
  const b = game({ id: 2, title: 'Beta', status: 'playing' }, [x(20, { game_id: 2, ownership: 'Owned' })]);
  const c = game({ id: 3, title: 'Aardvark' }, [x(30, { game_id: 3, ownership: 'Owned', hidden: true }), x(31, { game_id: 3, status: 'backlog' }), x(32, { game_id: 3, ownership: 'Owned', status: 'dropped' })]);
  assert.deepEqual(attentionRows([a, b, c]).map((r) => r.x.id), [12, 10], 'by game title, then release; nothing of an unfinished base game');
  const exp = buildGroups([a, b, c].map(clean), [], {}).find((g) => g.key === 'exp_left');
  assert.ok(exp && ATT.some((g) => g.key === 'exp_left'));
  assert.equal(exp.rows.length, 2);
  assert.equal(exp.rows[0].expansion.id, 12);
  assert.equal(exp.rows[0].game.id, 1);
});

test('Needs attention: ignoring works by expansion id, and is counted like the other groups', () => {
  const g = [clean(game({ id: 1 }, [x(10, { ownership: 'Owned' }), x(11, { ownership: 'Owned' })]))];
  const ignored = { exp_left: [10], no_cover: [10] }; // 10 is also a game id elsewhere: only its own group counts
  const open = buildGroups(g, [], ignored, false);
  assert.deepEqual(open.find((r) => r.key === 'exp_left').rows.map((r) => r.expansion.id), [11]);
  assert.equal(open.find((r) => r.key === 'exp_left').ignoredCount, 1);
  const all = buildGroups(g, [], ignored, true);
  assert.deepEqual(all.find((r) => r.key === 'exp_left').rows.map((r) => [r.expansion.id, r.ignored]), [[10, true], [11, false]]);
  assert.equal(totalOpen(open), 1);
  assert.equal(ignoredTotal(g, [], ignored), 1);
});

test('Upcoming: expansions of library games; finished, dropped, ruled out and hidden ones are not listed', () => {
  const day = (y, m, d) => Date.UTC(y, m - 1, d) / 1000;
  const g = game({ platforms: ['PC', 'PS5'] }, [
    x(1, { title: 'Future', date: day(2026, 8, 1) }),
    x(2, { title: 'TBA', date: null }),
    x(3, { title: 'Out', date: day(2026, 1, 1) }),
    x(5, { title: 'Dropped', date: day(2027, 1, 1), status: 'dropped' }),
    x(6, { title: 'Hidden', date: day(2027, 1, 1), hidden: true }),
    x(7, { title: 'Backlog', date: day(2027, 2, 1), status: 'backlog' }),
    x(8, { title: 'Done', date: day(2027, 2, 1), status: 'finished' }),
    x(9, { title: 'Unwanted', date: null, status: 'not_interested' }),
  ]);
  const items = upcomingExpansionItems([g], releasePeriod);
  assert.deepEqual(items.map((i) => i.title), ['Future', 'TBA', 'Out', 'Backlog']);
  const f = items[0];
  assert.deepEqual([f.id, f.kind, f.gameTitle, f.platforms, f.format, f.period.precision, f.seriesName], ['x1', 'expansion', 'The Witcher 3', ['PC', 'PS5'], 0, 'day', null]);
  assert.equal(items[1].period, null);
  assert.equal(items[1].format, null);
});

test('Upcoming: an expansion without a date is TBA only while nothing says it is out (owned, played, playing)', () => {
  assert.equal(isAwaited(x(1, { date: null })), true);
  assert.equal(isAwaited(x(1, { date: null, status: 'backlog' })), true, 'on the backlog: still waiting for it');
  assert.equal(isAwaited(x(1, { date: null, ownership: 'Nope' })), true, 'Nope is not owning it');
  assert.equal(isAwaited(x(1, { date: null, ownership: 'Owned' })), false);
  assert.equal(isAwaited(x(1, { date: null, years: [2019] })), false);
  assert.equal(isAwaited(x(1, { date: null, periods: ['childhood'] })), false);
  for (const st of ['playing', 'playing_evergreen', 'evergreen', 'hold']) assert.equal(isAwaited(x(1, { date: null, status: st })), false, st);
  const g = game({}, [x(1, { title: 'TBA', date: null }), x(2, { title: 'Old owned', date: null, ownership: 'Owned' }),
    x(3, { title: 'Pre-order', date: Date.UTC(2099, 0, 1) / 1000, ownership: 'Owned' }), x(4, { title: 'Played', date: null, years: [2015] })]);
  assert.deepEqual(upcomingExpansionItems([g], releasePeriod).map((i) => i.title), ['TBA', 'Pre-order'], 'a dated pre-order stays');
});

test('Upcoming page: expansions are grouped like games, and ones released long ago drop out', () => {
  const now = new Date(2026, 4, 10);
  const day = (y, m, d) => Date.UTC(y, m - 1, d) / 1000;
  const g = game({}, [
    x(1, { title: 'Future', date: day(2026, 8, 1) }), x(2, { title: 'TBA', date: null }), x(3, { title: 'Old', date: day(2026, 1, 1) }),
    x(4, { title: 'Last week', date: day(2026, 5, 3) }), x(5, { title: 'Today', date: day(2026, 5, 10) }),
  ]);
  const res = buildUpcoming([g], new Map(), now);
  assert.deepEqual(res.recent.map((i) => i.title), ['Today', 'Last week'], 'released within 60 days: recently released, newest first');
  assert.deepEqual(res.groups.map((gr) => [gr.label, gr.items.map((i) => i.title)]), [['August 2026', ['Future']], ['Later / TBA', ['TBA']]]);
  assert.equal(res.total, 2);
});

test('Stats: finished and owned, finished per year (last year played), never counted as games', () => {
  const g = game({}, [
    x(1, { status: 'finished', ownership: 'Owned', years: [2016, 2017] }),
    x(2, { status: 'finished', years: [2019] }),
    x(3, { status: 'finished', years: [], periods: ['childhood'] }),
    x(4, { ownership: 'Owned', status: 'playing' }),
    x(5, { ownership: 'Nope' }),
    x(6, { ownership: 'Owned', status: 'finished', years: [2019], hidden: true }),
  ]);
  const s = expansionStats([g, game({ id: 2 }, [])]);
  assert.deepEqual([s.total, s.owned, s.finished, s.unplaced], [5, 4, 3, 1]);
  assert.deepEqual(s.perYear, [{ year: 2017, count: 1 }, { year: 2019, count: 1 }]);
  assert.equal(finishedYear(x(1, { years: [2001, 2003] })), 2003);
  assert.equal(finishedYear(x(1)), null);
  assert.deepEqual(expansionStats([]), { total: 0, owned: 0, finished: 0, perYear: [], unplaced: 0 });
});

test('Year in review: expansions played that year, grouped by outcome', () => {
  const g = game({}, [x(1, { status: 'finished', years: [2016] }), x(2, { status: 'dropped', years: [2016, 2017] }), x(3, { status: 'playing', years: [2016] }),
    x(4, { status: null, years: [2016] }), x(5, { status: 'finished', years: [2018] }), x(6, { hidden: true, years: [2016] })]);
  const by = expansionsOfYear([g], 2016, GROUPS);
  assert.deepEqual(by.map((b) => [b.key, b.items.map((i) => i.x.id)]), [['finished', [1]], ['dropped', [2]], ['playing', [3]], ['played', [4]]]);
  assert.equal(countExpansionsOfYear([g], 2016), 4);
  assert.equal(countExpansionsOfYear([g], 2020), 0);
  assert.deepEqual([...expansionYears([g])].sort(), [2016, 2017, 2018]);
});

test('poster URL and title with year', () => {
  assert.equal(expansionCoverURL(x(1)), null);
  assert.equal(expansionCoverURL(x(1, { cover: 'x100.jpg', cover_v: 'abc' })), 'covers/x100.jpg?v=abc');
  assert.equal(withYear(x(1)), 'Hearts of Stone (2015)');
  assert.equal(withYear(x(1, { year: null })), 'Hearts of Stone');
});

test('stale: a linked game never read, or not read within 30 days, is read when its drawer opens', () => {
  const now = Date.parse('2026-06-30T00:00:00Z');
  assert.equal(isStale({ igdb_id: null }, now), false, 'not linked: nothing to read');
  assert.equal(isStale({ igdb_id: 5 }, now), true, 'never read');
  assert.equal(isStale({ igdb_id: 5, expansions_fetched: '2026-06-10T00:00:00Z' }, now), false);
  assert.equal(isStale({ igdb_id: 5, expansions_fetched: '2026-05-31T00:00:00Z' }, now), false, 'exactly 30 days is still fresh');
  assert.equal(isStale({ igdb_id: 5, expansions_fetched: '2026-05-30T00:00:00Z' }, now), true);
  assert.equal(isStale({ igdb_id: 5, expansions_fetched: 'garbage' }, now), true);
  assert.equal(FRESH_DAYS, 30);
});

test('the table and CSV cell lists the visible expansions with their state', () => {
  const g = game({}, [x(1, { title: 'Hearts', status: 'finished', ownership: 'Owned' }), x(2, { title: 'Wine' }), x(3, { title: 'Wrong', hidden: true }), x(4, { title: 'Pack', ownership: 'Nope' })]);
  assert.equal(expansionsCell(g, (c) => c.toUpperCase()), 'Hearts (FINISHED, Owned); Wine (Not tracked); Pack (Not tracked, Nope)');
  assert.equal(expansionsCell(game()), '');
});

test('an expansion not out yet is not left to play (a pre-order); one without a date is', () => {
  const now = Date.UTC(2026, 10, 17);
  const soon = x(1, { ownership: 'Owned', date: now / 1000 + 86400 * 40 });
  const out = x(2, { ownership: 'Owned', date: now / 1000 - 86400 });
  const tba = x(3, { ownership: 'Owned', date: null });
  assert.equal(isReleased(soon, now), false);
  assert.equal(leftToPlay(soon, now), false);
  assert.equal(leftToPlay(out, now), true);
  assert.equal(leftToPlay(tba, now), true, 'no date: an owned old one');
  assert.equal(hasLeftToPlay(game({}, [soon]), now), false, 'the Library filter skips a game whose only one is not out');
  assert.equal(hasLeftToPlay(game({}, [soon, out]), now), true);
  assert.deepEqual(attentionRows([game({}, [soon, out, tba])], now).map((r) => r.x.id), [2, 3], 'Needs attention leaves the pre-order out');
});

test('the row label says what the row shows', () => {
  const label = (c) => ({ finished: 'Finished' })[c] ?? c;
  assert.equal(expansionRowLabel(x(1, { status: 'finished', ownership: 'Owned', years: [2016], periods: ['childhood'] }), label),
    'Hearts of Stone (2015), Finished, Owned, played childhood, 2016: edit');
  assert.equal(expansionRowLabel(x(1, { year: null, hidden: true }), label, { title: 'The Witcher 3' }),
    'Hearts of Stone, expansion of The Witcher 3, Not tracked, hidden: edit');
});
