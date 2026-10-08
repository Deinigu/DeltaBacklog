import test from 'node:test';
import assert from 'node:assert/strict';
import { seededRng, buildPool, applyMood, moodCounts, emptyMood, notPlayedRecently, pickOne, hasNoFilters, playTarget } from '../static/js/pick.js';
import { emptyState, cycle } from '../static/js/filters.js';

let n = 0;
const game = (o = {}) => {
  const g = { id: ++n, title: 'G' + n, status: 'backlog', platforms: [], genres: [], themes: [], players: [], years: [], periods: [], ownership: null, vr: false, favorite: false, completed: false, ...o };
  g._search = g.title.toLowerCase();
  return g;
};
const ids = (list) => list.map((g) => g.id);

test('the same seed gives the same sequence, different seeds differ', () => {
  const a = seededRng(42), b = seededRng(42), c = seededRng(43);
  const sa = [a(), a(), a()], sb = [b(), b(), b()];
  assert.deepEqual(sa, sb);
  assert.notDeepEqual(sa, [c(), c(), c()]);
  assert.ok(sa.every((x) => x >= 0 && x < 1));
});

test('with no filters the pool is the Up next games', () => {
  const games = [game({ status: 'up_next' }), game({ status: 'up_next' }), game({ status: 'backlog' }), game({ status: 'finished' })];
  assert.deepEqual(ids(buildPool(emptyState(), games)), ids(games.slice(0, 2)));
});

test('with no filters and nothing Up next the pool is the Backlog', () => {
  const games = [game({ status: 'backlog' }), game({ status: 'playing' }), game({ status: 'backlog' }), game({ status: 'dropped' })];
  assert.deepEqual(ids(buildPool(emptyState(), games)), [games[0].id, games[2].id]);
});

test('with no filters, no Up next and no Backlog the pool is empty', () => {
  assert.deepEqual(buildPool(emptyState(), [game({ status: 'finished' }), game({ status: 'playing' })]), []);
  assert.deepEqual(buildPool(emptyState(), []), []);
});

test('sort and layout do not count as filters', () => {
  const s = emptyState();
  s.sorts = [{ field: 'title', dir: 'asc' }]; s.layout = 'table'; s.group = 'status';
  assert.equal(hasNoFilters(s), true);
  s.q = 'x';
  assert.equal(hasNoFilters(s), false);
});

test('a filtered pool is what matches, minus finished, dropped, not interested and evergreen', () => {
  const s = emptyState();
  s.flags.add('vr');
  const keep = [game({ vr: true, status: 'playing' }), game({ vr: true, status: 'hold' }), game({ vr: true, status: 'up_next' }), game({ vr: true, status: 'playing_evergreen' })];
  const out = ['finished', 'dropped', 'not_interested', 'evergreen'].map((status) => game({ vr: true, status }));
  const notVr = game({ vr: false, status: 'backlog' });
  assert.deepEqual(ids(buildPool(s, [...keep, ...out, notVr])), ids(keep));
});

test('a status the filters ask for explicitly is allowed', () => {
  const s = emptyState();
  cycle(s, 'status', 'finished');
  const fin = game({ status: 'finished' });
  const dropped = game({ status: 'dropped' });
  assert.deepEqual(ids(buildPool(s, [fin, dropped])), [fin.id]);
});

test('excluding a status does not make the other excluded statuses reappear', () => {
  const s = emptyState();
  cycle(s, 'status', 'backlog'); cycle(s, 'status', 'backlog'); // excluded
  const games = [game({ status: 'backlog' }), game({ status: 'finished' }), game({ status: 'playing' })];
  assert.deepEqual(ids(buildPool(s, games)), [games[2].id]);
});

test('search text counts as a filter', () => {
  const s = emptyState();
  s.q = 'zelda';
  const a = game({ title: 'Zelda', status: 'playing' });
  const b = game({ title: 'Mario', status: 'up_next' });
  assert.deepEqual(ids(buildPool(s, [a, b])), [a.id]);
});

test('not played recently: never played, or last calendar year at least 2 years ago', () => {
  assert.equal(notPlayedRecently(game({ years: [] }), 2026), true);
  assert.equal(notPlayedRecently(game({ years: [], periods: ['childhood'] }), 2026), true); // periods are not calendar years
  assert.equal(notPlayedRecently(game({ years: [], periods: ['always'] }), 2026), false); // played all the time
  assert.equal(notPlayedRecently(game({ years: [2010], periods: ['always'] }), 2026), false);
  assert.equal(notPlayedRecently(game({ years: [2024] }), 2026), true);
  assert.equal(notPlayedRecently(game({ years: [2010, 2024] }), 2026), true);
  assert.equal(notPlayedRecently(game({ years: [2025] }), 2026), false);
  assert.equal(notPlayedRecently(game({ years: [2010, 2026] }), 2026), false);
});

test('Play it keeps Evergreen games Evergreen and makes everything else Playing', () => {
  assert.equal(playTarget('evergreen'), 'playing_evergreen');
  assert.equal(playTarget('playing_evergreen'), 'playing_evergreen');
  for (const st of ['backlog', 'up_next', 'hold', 'playing', 'finished']) assert.equal(playTarget(st), 'playing');
});

test('mood: any selected platform, any selected genre, and the parts combine', () => {
  const a = game({ platforms: ['PC'], genres: ['RPG'] });
  const b = game({ platforms: ['Switch'], genres: ['RPG'] });
  const c = game({ platforms: ['PC', 'Switch'], genres: ['Puzzle'] });
  const d = game({ platforms: ['PS5'], genres: ['Puzzle'] });
  const pool = [a, b, c, d], y = 2026;
  const m = emptyMood();
  assert.deepEqual(ids(applyMood(pool, m, y)), ids(pool));
  m.platforms.add('PC'); m.platforms.add('Switch');
  assert.deepEqual(ids(applyMood(pool, m, y)), [a.id, b.id, c.id]);
  m.genres.add('RPG');
  assert.deepEqual(ids(applyMood(pool, m, y)), [a.id, b.id]);
});

test('mood: VR only and not played recently', () => {
  const a = game({ vr: true, years: [2020] });
  const b = game({ vr: true, years: [2026] });
  const c = game({ vr: false });
  const m = emptyMood();
  m.vr = true;
  assert.deepEqual(ids(applyMood([a, b, c], m, 2026)), [a.id, b.id]);
  m.stale = true;
  assert.deepEqual(ids(applyMood([a, b, c], m, 2026)), [a.id]);
});

test('chip counts show what a choice would leave given the other choices', () => {
  const a = game({ platforms: ['PC'], genres: ['RPG'], years: [2020] });
  const b = game({ platforms: ['PC', 'Switch'], genres: ['Puzzle'], years: [2026], vr: true });
  const c = game({ platforms: ['Switch'], genres: ['RPG'] });
  const pool = [a, b, c];
  const m = emptyMood();
  let k = moodCounts(pool, m, 2026);
  assert.deepEqual([...k.platform], [['PC', 2], ['Switch', 2]]);
  assert.deepEqual([...k.genre], [['RPG', 2], ['Puzzle', 1]]);
  assert.equal(k.vr, 1);
  assert.equal(k.stale, 2);
  m.genres.add('RPG');
  k = moodCounts(pool, m, 2026);
  assert.equal(k.platform.get('PC'), 1);       // platform counts respect the genre choice
  assert.equal(k.platform.get('Switch'), 1);
  assert.equal(k.genre.get('Puzzle'), 1);      // genre counts ignore the genre choice itself
  assert.equal(k.vr, 0);
  assert.equal(k.stale, 2);
});

test('chips with no match still appear with a zero count', () => {
  const a = game({ platforms: ['PC'], years: [2026] });
  const m = emptyMood();
  m.stale = true;
  assert.equal(moodCounts([a], m, 2026).platform.get('PC'), 0);
});

test('picking without repeats visits every game exactly once, then reports exhaustion', () => {
  const pool = Array.from({ length: 6 }, () => game());
  const rng = seededRng(7), shown = new Set(), skipped = new Set(), seen = [];
  for (let i = 0; i < pool.length; i++) {
    const r = pickOne(pool, shown, skipped, rng);
    assert.ok(r.game);
    assert.equal(r.left, pool.length - 1 - i);
    assert.equal(r.exhausted, false);
    shown.add(r.game.id); seen.push(r.game.id);
  }
  assert.deepEqual([...seen].sort(), ids(pool).sort());
  const end = pickOne(pool, shown, skipped, rng);
  assert.deepEqual(end, { game: null, left: 0, exhausted: true, allSkipped: false });
  shown.clear(); // start over
  assert.ok(pickOne(pool, shown, skipped, rng).game);
});

test('the same seed picks the same game', () => {
  const pool = Array.from({ length: 10 }, () => game());
  const a = pickOne(pool, new Set(), new Set(), seededRng(5)).game.id;
  const b = pickOne(pool, new Set(), new Set(), seededRng(5)).game.id;
  assert.equal(a, b);
});

test('rng values at the edges stay in range', () => {
  const pool = [game(), game(), game()];
  assert.equal(pickOne(pool, new Set(), new Set(), () => 0).game.id, pool[0].id);
  assert.equal(pickOne(pool, new Set(), new Set(), () => 0.9999999).game.id, pool[2].id);
});

test('skipped games are never picked', () => {
  const pool = [game(), game(), game()];
  const skipped = new Set([pool[0].id, pool[1].id]);
  for (let s = 0; s < 20; s++) assert.equal(pickOne(pool, new Set(), skipped, seededRng(s)).game.id, pool[2].id);
});

test('skipped games do not count as left, and starting over does not bring them back', () => {
  const pool = [game(), game(), game()];
  const skipped = new Set([pool[0].id]);
  const shown = new Set();
  const r = pickOne(pool, shown, skipped, seededRng(1));
  assert.equal(r.left, 1);
  shown.add(r.game.id);
  const r2 = pickOne(pool, shown, skipped, seededRng(1));
  shown.add(r2.game.id);
  assert.equal(pickOne(pool, shown, skipped).exhausted, true);
  shown.clear();
  for (let s = 0; s < 10; s++) assert.notEqual(pickOne(pool, shown, skipped, seededRng(s)).game.id, pool[0].id);
});

test('when every candidate is skipped it says so', () => {
  const pool = [game(), game()];
  const r = pickOne(pool, new Set(), new Set(ids(pool)));
  assert.deepEqual(r, { game: null, left: 0, exhausted: true, allSkipped: true });
});

test('an empty pool is not "exhausted": nothing matches', () => {
  assert.deepEqual(pickOne([], new Set(), new Set()), { game: null, left: 0, exhausted: false, allSkipped: false });
});

test('a single game is picked, then the pool is exhausted', () => {
  const only = game();
  const shown = new Set();
  const r = pickOne([only], shown, new Set());
  assert.equal(r.game.id, only.id);
  assert.equal(r.left, 0);
  shown.add(only.id);
  assert.equal(pickOne([only], shown, new Set()).exhausted, true);
});
