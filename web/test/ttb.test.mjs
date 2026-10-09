import test from 'node:test';
import assert from 'node:assert/strict';
import { store } from '../static/js/api.js';
import { SHORT_MINUTES, ttbMinutes, isShort, formatTime, ttbParts, totalTime } from '../static/js/ttb.js';
import { emptyState, matches, cycle, GROUPS, FLAGS, stateFromView, stateFromQuery, stateToQuery, filterToJSON, activeCount, facetSignature } from '../static/js/filters.js';
import { emptyMood, applyMood, moodCounts, moodIsEmpty } from '../static/js/pick.js';
import { backlogTime } from '../static/js/stats.js';

store.statusOrder = new Map();
let n = 0;
const g = (o = {}) => {
  const x = { id: ++n, title: 'G' + n, status: 'backlog', platforms: [], genres: [], themes: [], players: [], years: [], periods: [], ownership: null, vr: false, favorite: false, completed: false, ...o };
  x._search = x.title.toLowerCase();
  return x;
};
const ids = (list) => list.map((x) => x.id);

test('the estimate is the usual time, else quick, else complete; unknown is null', () => {
  assert.equal(ttbMinutes(g({ ttb_normally: 120, ttb_hastily: 60 })), 120);
  assert.equal(ttbMinutes(g({ ttb_hastily: 60, ttb_completely: 300 })), 60);
  assert.equal(ttbMinutes(g({ ttb_completely: 300 })), 300);
  assert.equal(ttbMinutes(g()), null);
  assert.equal(ttbMinutes(g({ ttb_normally: null, ttb_hastily: 0 })), null);
});

test('short means under 10 hours, and a game without a time is never short', () => {
  assert.equal(isShort(g({ ttb_normally: SHORT_MINUTES - 1 })), true);
  assert.equal(isShort(g({ ttb_normally: SHORT_MINUTES })), false);
  assert.equal(isShort(g()), false);
});

test('times are shown in minutes below an hour, one decimal below ten hours, whole hours above', () => {
  assert.equal(formatTime(45), '45 min');
  assert.equal(formatTime(90), '1.5 h');
  assert.equal(formatTime(120), '2 h');
  assert.equal(formatTime(450), '7.5 h');
  assert.equal(formatTime(719), '12 h');
  assert.equal(formatTime(2500), '42 h');
  assert.equal(formatTime(null), '');
  assert.equal(formatTime(0), '');
});

test('drawer parts leave out what IGDB does not know', () => {
  assert.deepEqual(ttbParts(g({ ttb_hastily: 600, ttb_completely: 1500 })), [{ label: 'Quick', text: '10 h' }, { label: 'Complete', text: '25 h' }]);
  assert.deepEqual(ttbParts(g()), []);
});

test('total time adds the estimates and counts games with and without one', () => {
  const t = totalTime([g({ ttb_normally: 90 }), g({ ttb_hastily: 30 }), g()]);
  assert.deepEqual(t, { minutes: 120, hours: 2, known: 2, unknown: 1 });
});

test('backlog time covers backlog and up next only', () => {
  const r = backlogTime([
    g({ status: 'backlog', ttb_normally: 600 }), g({ status: 'up_next', ttb_normally: 1200 }), g({ status: 'backlog' }),
    g({ status: 'finished', ttb_normally: 6000 }), g({ status: 'hold', ttb_normally: 6000 }),
  ]);
  assert.deepEqual(r, { hours: 30, known: 2, unknown: 1, total: 3 });
  assert.deepEqual(backlogTime([]), { hours: 0, known: 0, unknown: 0, total: 0 });
});

// ---- filters ----
const library = [
  g({ title: 'Short coop', modes: ['Single player', 'Co-operative'], perspectives: ['First person'], ttb_normally: 300 }),
  g({ title: 'Long solo', modes: ['Single player'], perspectives: ['Third person'], ttb_normally: 3000 }),
  g({ title: 'Unknown' }), // not enriched: no modes, perspectives or times at all (older cached data may lack the keys)
];
const found = (s) => library.filter((x) => matches(s, x)).map((x) => x.title);

test('game mode and perspective filters work like the other groups', () => {
  assert.ok(GROUPS.some((x) => x.key === 'mode' && x.multi) && GROUPS.some((x) => x.key === 'perspective' && x.multi));
  const s = emptyState();
  cycle(s, 'mode', 'Co-operative');
  assert.deepEqual(found(s), ['Short coop']);
  cycle(s, 'mode', 'Co-operative'); // exclude
  assert.deepEqual(found(s), ['Long solo', 'Unknown']);
  const t = emptyState();
  cycle(t, 'mode', 'Single player'); cycle(t, 'perspective', 'First person');
  assert.deepEqual(found(t), ['Short coop']);
  t.in.mode.add('Co-operative'); t.all.add('mode');
  assert.deepEqual(found(t), ['Short coop']);
});

test('the short game flag keeps only games under ten hours', () => {
  assert.ok(FLAGS.some((f) => f.key === 'short'));
  const s = emptyState();
  s.flags.add('short');
  assert.deepEqual(found(s), ['Short coop']);
  assert.equal(activeCount(s), 1);
});

test('existing flags still read the game field', () => {
  const s = emptyState();
  s.flags.add('vr');
  assert.deepEqual(library.filter((x) => matches(s, { ...x, vr: x.title === 'Unknown' })).map((x) => x.title), ['Unknown']);
});

test('saved views: old ones load, new filters round-trip through JSON and the URL', () => {
  const old = stateFromView({ filter: { in: { genre: ['RPG'] }, flags: ['vr', 'favorite'] }, sort: [] });
  assert.deepEqual([...old.flags], ['vr', 'favorite']);
  assert.equal(old.in.mode.size, 0);

  const s = emptyState();
  cycle(s, 'mode', 'Co-operative'); cycle(s, 'perspective', 'First person'); s.flags.add('short');
  const json = filterToJSON(s);
  assert.deepEqual(json, { in: { mode: ['Co-operative'], perspective: ['First person'] }, flags: ['short'] });
  const back = stateFromView({ filter: JSON.parse(JSON.stringify(json)), sort: [] });
  assert.deepEqual(filterToJSON(back), json);

  const q = stateToQuery(s);
  assert.deepEqual(filterToJSON(stateFromQuery(q)), json);
});

test('a view saved with a flag this version does not know is still read', () => {
  const s = stateFromView({ filter: { flags: ['vr', 'from-the-future'] }, sort: [] });
  assert.deepEqual([...s.flags], ['vr']);
});

test('facet signature notices a new mode', () => {
  const before = facetSignature(library);
  library.push(g({ modes: ['Battle Royale'] }));
  assert.notEqual(facetSignature(library), before);
  library.pop();
});

// ---- Surprise me: "quick game" mood ----
test('mood: quick game keeps games under ten hours and the chip count says how many', () => {
  const a = g({ ttb_normally: 300 }), b = g({ ttb_normally: 3000 }), c = g();
  const m = emptyMood();
  assert.equal(moodIsEmpty(m), true);
  assert.equal(moodCounts([a, b, c], m, 2026).quick, 1);
  m.quick = true;
  assert.equal(moodIsEmpty(m), false);
  assert.deepEqual(ids(applyMood([a, b, c], m, 2026)), [a.id]);
  m.vr = true; // the other choices still combine with AND
  assert.deepEqual(ids(applyMood([a, b, c], m, 2026)), []);
});
