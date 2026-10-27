import test from 'node:test';
import assert from 'node:assert/strict';
import { store } from '../static/js/api.js';
import { SHORT_MINUTES, ttbMinutes, isShort, formatTime, ttbTiles, ttbSourceNote, parseTime, timeInputValue, hltbPatch, hasTimes, TTB_KINDS, totalTime } from '../static/js/ttb.js';
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

test('times are shown in minutes below an hour, half hours below ten hours, whole hours above', () => {
  assert.equal(formatTime(45), '45 min');
  assert.equal(formatTime(60), '1 h');
  assert.equal(formatTime(90), '1½ h');
  assert.equal(formatTime(120), '2 h');
  assert.equal(formatTime(450), '7½ h');
  assert.equal(formatTime(479), '7½ h');
  assert.equal(formatTime(480), '8 h');
  assert.equal(formatTime(719), '11 h');
  assert.equal(formatTime(599), '9½ h'); // under 10 h (short) never reads "10 h"
  assert.equal(formatTime(600), '10 h');
  assert.equal(formatTime(7200), '120 h');
  assert.equal(formatTime(null), '');
  assert.equal(formatTime(0), '');
});

test('the labels are the same everywhere', () => {
  assert.deepEqual(TTB_KINDS.map((k) => k.label), ['Main story', 'Main + extras', 'Completionist']);
});

test('tiles always list the three times; unknown ones are empty and the bar is relative to the longest', () => {
  const t = ttbTiles(g({ ttb_hastily: 300, ttb_completely: 1200 }));
  assert.deepEqual(t.map((x) => [x.label, x.text, x.share]), [['Main story', '5 h', 0.25], ['Main + extras', '', 0], ['Completionist', '20 h', 1]]);
  assert.deepEqual(ttbTiles(g()).map((x) => x.minutes), [null, null, null]);
  assert.equal(hasTimes(g()), false);
  assert.equal(hasTimes(g({ ttb_normally: 5 })), true);
});

test('the source note names where the times came from; older data without a source is IGDB', () => {
  assert.equal(ttbSourceNote(g({ ttb_normally: 5, ttb_source: 'hltb' })), 'from HowLongToBeat');
  assert.equal(ttbSourceNote(g({ ttb_normally: 5, ttb_source: 'manual' })), 'set by you');
  assert.equal(ttbSourceNote(g({ ttb_normally: 5, ttb_source: 'igdb' })), 'from IGDB');
  assert.equal(ttbSourceNote(g({ ttb_normally: 5 })), 'from IGDB');
  assert.equal(ttbSourceNote(g({ ttb_source: 'manual' })), ''); // no time, no note
});

test('typed times: hours by default, h and m forms, comma decimals; empty clears; nonsense is undefined', () => {
  const cases = [['7.5', 450], ['7,5', 450], ['12', 720], ['7h30', 450], ['7 h 30 min', 450], ['7h', 420], ['1:30', 90], ['2h5', 125],
    ['45m', 45], ['90 min', 90], ['0.5', 30], [' 8 ', 480], ['2H', 120]];
  for (const [text, want] of cases) assert.equal(parseTime(text), want, text);
  assert.equal(parseTime(''), null);
  assert.equal(parseTime('   '), null);
  assert.equal(parseTime(null), null);
  for (const bad of ['abc', '0', '-3', '7h30m5', '1.2.3', '7 hours and a bit', '2000000', '0m']) assert.equal(parseTime(bad), undefined, bad);
});

test('the edit input shows hours, minutes below an hour, and survives a round trip', () => {
  assert.equal(timeInputValue(450), '7.5');
  assert.equal(timeInputValue(45), '45m');
  assert.equal(timeInputValue(null), '');
  for (const m of [45, 90, 125, 450, 7200]) assert.ok(Math.abs(parseTime(timeInputValue(m)) - m) <= 1, String(m));
});

test('a HowLongToBeat candidate becomes a patch with its source; missing kinds clear', () => {
  assert.deepEqual(hltbPatch({ hastily: 180, normally: 0, completely: 450 }), { ttb_hastily: 180, ttb_normally: null, ttb_completely: 450, ttb_source: 'hltb' });
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

test('the under-10-hours range keeps only games under ten hours', () => {
  const s = emptyState();
  s.ttb.max = SHORT_MINUTES;
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
  cycle(s, 'mode', 'Co-operative'); cycle(s, 'perspective', 'First person'); s.ttb.max = 600;
  const json = filterToJSON(s);
  assert.deepEqual(json, { in: { mode: ['Co-operative'], perspective: ['First person'] }, ttb: { max: 600 } });
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

test('ttb filter: range is min inclusive, max exclusive, and the presets tile', async () => {
  const { emptyTtbFilter, matchesTtb, TTB_PRESETS, ttbValue, ttbRangeText } = await import('../static/js/ttb.js');
  const at = (m) => ({ ttb_normally: m });
  const f = { ...emptyTtbFilter(), min: 300, max: 900 };
  assert.equal(matchesTtb(f, at(300)), true);
  assert.equal(matchesTtb(f, at(899)), true);
  assert.equal(matchesTtb(f, at(900)), false);
  assert.equal(matchesTtb(f, {}), false); // no time: never in a range
  for (const m of [1, 299, 300, 899, 900, 2399, 2400, 9000]) {
    assert.equal(TTB_PRESETS.filter((p) => matchesTtb({ ...emptyTtbFilter(), min: p.min, max: p.max }, at(m))).length, 1, `${m} min`);
  }
  assert.equal(ttbRangeText(f), '5-15 h');
  assert.equal(ttbRangeText({ ...emptyTtbFilter(), max: 600 }), 'under 10 h');
  assert.equal(ttbValue({ ttb_hastily: 60, ttb_normally: 120 }, 'ttb_completely'), null); // a chosen estimate has no fallback
  assert.equal(ttbValue({ ttb_hastily: 60, ttb_normally: 120 }, 'ttb_hastily'), 60);
});

test('ttb filter: no time known', async () => {
  const { emptyTtbFilter, matchesTtb } = await import('../static/js/ttb.js');
  const known = { ttb_normally: 600 }, none = {};
  const only = { ...emptyTtbFilter(), unknown: 'in' }, hide = { ...emptyTtbFilter(), unknown: 'ex' };
  assert.deepEqual([matchesTtb(only, known), matchesTtb(only, none)], [false, true]);
  assert.deepEqual([matchesTtb(hide, known), matchesTtb(hide, none)], [true, false]);
  const both = { ...emptyTtbFilter(), max: 300, unknown: 'in' }; // short games or those with no time
  assert.deepEqual([matchesTtb(both, { ttb_normally: 100 }), matchesTtb(both, known), matchesTtb(both, none)], [true, false, true]);
  const rangeHide = { ...emptyTtbFilter(), max: 300, unknown: 'ex' };
  assert.deepEqual([matchesTtb(rangeHide, { ttb_normally: 100 }), matchesTtb(rangeHide, none)], [true, false]);
});

test('an empty range (max not above min) is refused', async () => {
  const { rangeOk } = await import('../static/js/ttb.js');
  assert.deepEqual([rangeOk(null, 600), rangeOk(1200, null), rangeOk(300, 600), rangeOk(1200, 600), rangeOk(600, 600)], [true, true, true, false, false]);
});
