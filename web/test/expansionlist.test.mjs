import test from 'node:test';
import assert from 'node:assert/strict';
import { emptyExpansionFilter, expansionFilterCount, expansionPasses, filterExpansions, countExpansions, poolOf, platformsOf, statusKey, ownershipKey,
  expansionStateFromQuery, expansionStateToQuery, DEFAULT_SORT, NOT_TRACKED, poolCount, runToLoad, steamOfferCount, isSteamOffer } from '../static/js/expansionlist.js';

const x = (id, o = {}) => ({ id, title: `Exp ${id}`, status: null, ownership: null, date: null, updated_at: '2026-01-01T00:00:00Z', ...o });
const games = [
  { id: 1, title: 'Zelda', status: 'finished', platforms: ['Switch'], expansions: [x(1, { date: 300, status: 'finished', ownership: 'Owned' }), x(2, { date: 100, ownership: 'Nope', notes: 'maybe one day' }), x(5, { date: 400, ownership: 'Nope' })] },
  { id: 2, title: 'Alpha', status: 'backlog', platforms: ['PC', 'PS5'], expansions: [x(3, { date: 200, status: 'backlog', updated_at: '2026-03-01T00:00:00Z' }), x(4, { hidden: true, ownership: 'Owned' })] },
];
const pool = poolOf(games);
const ids = (rows) => rows.map((r) => r.x.id);

test('the pool holds your expansions, hidden ones too; the filter shows one kind at a time', () => {
  assert.equal(pool.length, 4, 'Exp 5 (only on IGDB, not added: Nope and nothing else) is left out');
  assert.ok(!ids(pool).includes(5));
  assert.deepEqual(ids(filterExpansions(pool, emptyExpansionFilter())), [3, 2, 1], 'default: by game title, then release; hidden left out');
  const f = emptyExpansionFilter();
  f.hidden = true;
  assert.deepEqual(ids(filterExpansions(pool, f)), [4]);
  assert.equal(poolCount(pool, emptyExpansionFilter()), 3);
  assert.equal(poolCount(pool, f), 1);
});

test('status and ownership keys', () => {
  assert.equal(statusKey(x(1)), NOT_TRACKED);
  assert.equal(statusKey(x(1, { status: 'finished' })), 'finished');
  assert.deepEqual([ownershipKey(x(1)), ownershipKey(x(1, { ownership: 'Nope' })), ownershipKey(x(1, { ownership: 'Borrowed' }))], ['unset', 'not_owned', 'owned']);
});

test('filters: search in both titles, status, ownership, platform of the base game, base game finished', () => {
  const run = (set) => { const f = emptyExpansionFilter(); set(f); return ids(filterExpansions(pool, f)); };
  assert.deepEqual(run((f) => { f.q = 'zelda'; }), [2, 1], 'the game title matches too');
  assert.deepEqual(run((f) => { f.q = 'exp 3'; }), [3]);
  assert.deepEqual(run((f) => { f.q = 'zelda exp 2'; }), [2], 'words of both titles');
  assert.deepEqual(run((f) => f.status.add(NOT_TRACKED)), [2]);
  assert.deepEqual(run((f) => { f.status.add('finished'); f.status.add('backlog'); }), [3, 1]);
  assert.deepEqual(run((f) => f.ownership.add('owned')), [1]);
  assert.deepEqual(run((f) => f.ownership.add('not_owned')), [2]);
  assert.deepEqual(run((f) => f.ownership.add('unset')), [3]);
  assert.deepEqual(run((f) => f.platforms.add('PS5')), [3]);
  assert.deepEqual(run((f) => { f.baseFinished = true; }), [2, 1]);
  assert.deepEqual(run((f) => { f.baseFinished = true; f.status.add('finished'); }), [1], 'filters combine');
});

test('sorts: game, release date (newest first, TBA first), title, recently changed', () => {
  const f = emptyExpansionFilter();
  assert.deepEqual(ids(filterExpansions(pool, f, 'release')), [1, 3, 2]);
  assert.deepEqual(ids(filterExpansions(pool, f, 'title')), [1, 2, 3]);
  assert.deepEqual(ids(filterExpansions(pool, f, 'changed')), [3, 1, 2]);
  assert.deepEqual(ids(filterExpansions(pool, f, 'nonsense')), [3, 2, 1], 'an unknown sort is the default');
  const tba = [{ x: x(9, { status: 'backlog' }), game: games[0] }, { x: x(8, { date: 50, status: 'backlog' }), game: games[0] }];
  assert.deepEqual(ids(filterExpansions(tba, f, 'release')), [9, 8], 'TBA does not turn the comparison into NaN');
});

test('counts for the chips: the filter with that group set to one value', () => {
  const f = emptyExpansionFilter();
  assert.equal(countExpansions(pool, f, 'status', 'finished'), 1);
  assert.equal(countExpansions(pool, f, 'status', NOT_TRACKED), 1);
  assert.equal(countExpansions(pool, f, 'platforms', 'PC'), 1);
  f.q = 'zelda';
  assert.equal(countExpansions(pool, f, 'ownership', 'owned'), 1, 'the search stays');
  assert.deepEqual(platformsOf(pool).map((p) => [p.value, p.count]), [['PC', 2], ['PS5', 2], ['Switch', 2]]);
  assert.equal(expansionFilterCount(f), 0, 'the search is not a filter');
});

test('the address round trips and drops what it does not know', () => {
  const f = emptyExpansionFilter();
  f.q = 'hearts';
  f.status.add('finished'); f.status.add(NOT_TRACKED);
  f.ownership.add('owned');
  f.platforms.add('PC');
  f.baseFinished = true;
  f.hidden = true;
  const qs = expansionStateToQuery({ filter: f, sort: 'release' });
  assert.equal(qs, 'q=hearts&status=finished&status=none&own=owned&platform=PC&base=finished&hidden=1&sort=release');
  const back = expansionStateFromQuery(qs);
  assert.deepEqual([back.filter.q, [...back.filter.status].sort(), [...back.filter.ownership], [...back.filter.platforms], back.filter.baseFinished, back.filter.hidden, back.sort],
    ['hearts', ['finished', 'none'], ['owned'], ['PC'], true, true, 'release']);
  assert.equal(expansionFilterCount(back.filter), 6);
  assert.equal(expansionStateToQuery({ filter: emptyExpansionFilter(), sort: DEFAULT_SORT }), '');
  const junk = expansionStateFromQuery('?own=maybe&sort=nope&base=no&hidden=2');
  assert.deepEqual([junk.filter.ownership.size, junk.sort, junk.filter.baseFinished, junk.filter.hidden], [0, DEFAULT_SORT, false, false]);
  assert.equal(expansionStateFromQuery('').sort, DEFAULT_SORT);
});

test('expansionPasses: a hidden expansion only shows when asked', () => {
  const row = pool.find((r) => r.x.id === 4);
  assert.equal(expansionPasses(emptyExpansionFilter(), row), false);
  const f = emptyExpansionFilter(); f.hidden = true;
  assert.equal(expansionPasses(f, row), true);
});

test('a run that ended while the page was closed is loaded once', () => {
  const done = { running: false, finished_at: '2026-11-17T10:00:00Z', report: 'Found 3 expansions for 2 games.' };
  assert.equal(runToLoad(done, null), '2026-11-17T10:00:00Z', 'never loaded: load it');
  assert.equal(runToLoad(done, '2026-11-17T10:00:00Z'), null, 'already loaded');
  assert.equal(runToLoad({ ...done, finished_at: '2026-11-17T11:00:00Z' }, '2026-11-17T10:00:00Z'), '2026-11-17T11:00:00Z', 'a newer run');
  assert.equal(runToLoad({ running: true, finished_at: '2026-11-17T10:00:00Z' }, null), null, 'still going: the poll loads it');
  assert.equal(runToLoad({ running: false }, null), null, 'no run since the server started');
  assert.equal(runToLoad(null, null), null);
});

test('hidden view: every hidden expansion, added or not (the Hidden page counts them all); Steam view: owned on Steam, not added', () => {
  const gs = [{ id: 9, title: 'Skyrim', status: 'finished', platforms: ['PC'], expansions: [
    x(10, { hidden: true }), // a wrong IGDB entry, hidden, never added
    x(11, { kind: 'dlc', steam_owned: true }), // owned on Steam, not added: an offer
    x(12, { kind: 'dlc', steam_owned: true, status: 'backlog' }), // added: one of yours
    x(13, { kind: 'dlc' }), // only on IGDB
    x(14, { kind: 'dlc', steam_owned: true, hidden: true }), // a hidden offer: only in the hidden view
  ] }];
  const p = poolOf(gs);
  assert.deepEqual(ids(p).sort(), [10, 11, 12, 14]);
  assert.deepEqual(ids(filterExpansions(p, emptyExpansionFilter())), [12]);
  const hid = emptyExpansionFilter(); hid.hidden = true;
  assert.deepEqual(ids(filterExpansions(p, hid)).sort(), [10, 14]);
  const st = emptyExpansionFilter(); st.steam = true;
  assert.deepEqual(ids(filterExpansions(p, st)), [11]);
  assert.equal(poolCount(p, st), 1);
  assert.equal(poolCount(p, emptyExpansionFilter()), 1);
  assert.equal(steamOfferCount(p), 1);
  assert.equal(countExpansions(p, emptyExpansionFilter(), 'steam', true), 1);
  assert.equal(isSteamOffer(x(1, { steam_owned: true, ownership: 'Owned' })), false, 'owned: added');
  assert.equal(isSteamOffer(x(1, { steam_owned: true, ownership: 'Nope' })), false, "Nope is the owner's choice (the Steam import leaves it too)");
  assert.equal(expansionFilterCount(st), 1);
  assert.equal(expansionStateToQuery({ filter: st, sort: DEFAULT_SORT }), 'steam=1');
  assert.equal(expansionStateFromQuery('?steam=1').filter.steam, true);
  assert.equal(expansionStateFromQuery('?steam=2').filter.steam, false);
});
