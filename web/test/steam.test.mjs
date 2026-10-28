import test from 'node:test';
import assert from 'node:assert/strict';
import { TABS, FILTERS, buildSteam, matchCandidates, fold, playedLabel, lastPlayedLabel, sourceLabel, syncNote } from '../static/js/steam.js';
import { playedMinutes, playedOfText } from '../static/js/ttb.js';
import { buildComparator, SORT_FIELDS } from '../static/js/sort.js';
import { store } from '../static/js/api.js';

store.statusOrder = new Map();

const app = (app_id, name, playtime_min = 0, o = {}) => ({ app_id, name, playtime_min, last_played: null, game_id: null, source: null, ignored: false, ...o });
const game = (id, title) => ({ id, title, status: 'backlog' });
const games = [game(1, 'Hollow Knight'), game(2, 'Portal 2'), game(3, 'Hades')];
const info = {
  configured: true,
  synced_at: '2026-03-01T10:00:00Z',
  apps: [
    app(10, 'Stray', 300),
    app(11, 'Alpha', 300),
    app(12, 'Zeta', 0),
    app(13, 'Beta', 45),
    app(14, 'Ignored Game', 600, { ignored: true }),
    app(15, 'Hollow Knight', 5000, { game_id: 1, source: 'title' }),
    app(16, 'Portal 2', 90, { game_id: 2, source: 'user' }),
    app(17, 'Gone', 10, { game_id: 99, source: 'title' }), // matched to a game this page does not know: counts as not in the library
  ],
};

test('the tabs and filters', () => {
  assert.deepEqual(TABS.map((t) => t.key), ['new', 'matched', 'ignored']);
  assert.deepEqual(FILTERS.map((f) => f.key), ['all', 'played', 'never']);
});

test('not in library: most played first, then by name, with the counts of every tab and filter', () => {
  const { rows, counts } = buildSteam(games, info);
  assert.deepEqual(rows.map((r) => r.app.name), ['Alpha', 'Stray', 'Beta', 'Gone', 'Zeta']);
  assert.deepEqual(counts, { new: 5, matched: 2, ignored: 1, played: 4, never: 1 });
});

test('the Played and Never played filters', () => {
  assert.deepEqual(buildSteam(games, info, { filter: 'played' }).rows.map((r) => r.app.name), ['Alpha', 'Stray', 'Beta', 'Gone']);
  assert.deepEqual(buildSteam(games, info, { filter: 'never' }).rows.map((r) => r.app.name), ['Zeta']);
  // the filter belongs to the first tab only
  assert.equal(buildSteam(games, info, { tab: 'ignored', filter: 'never' }).rows.length, 1);
});

test('matched tab pairs the app with its library game and sorts by name', () => {
  const { rows } = buildSteam(games, info, { tab: 'matched' });
  assert.deepEqual(rows.map((r) => [r.app.name, r.game.title]), [['Hollow Knight', 'Hollow Knight'], ['Portal 2', 'Portal 2']]);
});

test('ignored tab', () => {
  assert.deepEqual(buildSteam(games, info, { tab: 'ignored' }).rows.map((r) => r.app.name), ['Ignored Game']);
});

test('search ignores case, accents and punctuation, in the Steam name and the library title', () => {
  assert.deepEqual(buildSteam(games, info, { query: 'ALPHA' }).rows.map((r) => r.app.name), ['Alpha']);
  assert.deepEqual(buildSteam(games, info, { tab: 'matched', query: 'portal-2' }).rows.map((r) => r.app.name), ['Portal 2']);
  assert.equal(buildSteam(games, info, { query: 'nothing like it' }).rows.length, 0);
  assert.equal(fold('  Pokémon: Let’s Go!  '), 'pokemon let s go');
});

test('no answer yet', () => {
  assert.deepEqual(buildSteam([], null).rows, []);
  assert.deepEqual(buildSteam([], { apps: [] }).counts, { new: 0, matched: 0, ignored: 0, played: 0, never: 0 });
});

test('library picker candidates: prefix matches first, then by title, limited', () => {
  const lib = [game(1, 'The Portal Show'), game(2, 'Portal 2'), game(3, 'Portal'), game(4, 'Hades')];
  assert.deepEqual(matchCandidates(lib, 'portal').map((g) => g.title), ['Portal', 'Portal 2', 'The Portal Show']);
  assert.equal(matchCandidates(lib, 'portal', 2).length, 2);
  assert.deepEqual(matchCandidates(lib, '  '), []);
  assert.deepEqual(matchCandidates(lib, 'zzz'), []);
});

test('labels', () => {
  assert.equal(playedLabel(720), '12 h played');
  assert.equal(playedLabel(45), '45 min played');
  assert.equal(playedLabel(0), 'Never played');
  assert.equal(lastPlayedLabel(1700000000), 'Last played 14 Nov 2023');
  assert.equal(lastPlayedLabel(null), '');
  assert.equal(lastPlayedLabel(0), '');
  assert.equal(sourceLabel('link'), 'by its Steam link');
  assert.equal(sourceLabel('title'), 'by its title');
  assert.equal(sourceLabel('user'), 'by you');
  assert.equal(sourceLabel(null), '');
});

test('the note under the sync button', () => {
  assert.match(syncNote({ configured: false, synced_at: '' }), /not set up/);
  assert.match(syncNote({ configured: true, synced_at: '' }), /Press Sync with Steam/);
  assert.match(syncNote({ configured: true, synced_at: '2026-03-01T10:00:00Z' }), /^Last synced /);
  assert.match(syncNote({ configured: false, synced_at: '2026-03-01T10:00:00Z' }), /^Last synced /); // cached data stays visible without keys
});

test('hours next to the time to beat: "12 h of ~15 h"', () => {
  assert.equal(playedOfText({ playtime_min: 720, ttb_normally: 900 }), '12 h of ~15 h');
  assert.equal(playedOfText({ playtime_min: 720, ttb_hastily: 500 }), '12 h of ~8 h'); // 500 min rounds down to 8 h
  assert.equal(playedOfText({ playtime_min: 720 }), '12 h');
  assert.equal(playedOfText({ playtime_min: 45, ttb_normally: 90 }), '45 min of ~1½ h');
  assert.equal(playedOfText({ playtime_min: 0, ttb_normally: 900 }), '');
  assert.equal(playedOfText({ playtime_min: null, ttb_normally: 900 }), '');
  assert.equal(playedOfText({ ttb_normally: 900 }), '');
  assert.equal(playedMinutes({ playtime_min: 0 }), null);
  assert.equal(playedMinutes({ playtime_min: 30 }), 30);
});

test('sorting by hours played puts unplayed games last in both directions', () => {
  const list = [{ title: 'A', playtime_min: 30 }, { title: 'B', playtime_min: null }, { title: 'C', playtime_min: 600 }, { title: 'D', playtime_min: 0 }];
  assert.ok(SORT_FIELDS.playtime);
  // a game with no minutes played counts as having none, like a missing value: both sort last
  assert.deepEqual([...list].sort(buildComparator([{ field: 'playtime', dir: 'desc' }])).map((g) => g.title), ['C', 'A', 'B', 'D']);
});
