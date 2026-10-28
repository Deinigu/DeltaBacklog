import test from 'node:test';
import assert from 'node:assert/strict';
import { isSource, sourcesOf, becauseText, similarIndex, setHiddenFlag, similarFor } from '../static/js/similar.js';

let n = 0;
const game = (title, igdb, o = {}) => ({ id: ++n, title, igdb_id: igdb, status: 'backlog', favorite: false, years: [], periods: [], ...o });
const done = (title, igdb, years = [], o = {}) => game(title, igdb, { status: 'finished', years, ...o });
const sugg = (id, title, sources, o = {}) => ({ igdb_id: id, title, year: 2010, cover: null, v: '', parent: 0, date: null, format: null, sources, hidden: false, ...o });

test('sources: linked, not wished for, finished or favourite', () => {
  assert.ok(isSource(done('A', 1)));
  assert.ok(isSource(game('B', 2, { favorite: true })));
  assert.ok(!isSource(game('C', 3)));
  assert.ok(!isSource(done('D', null)));
  assert.ok(!isSource(game('E', 5, { status: 'wishlist', favorite: true })));
});

test('sources of a suggestion come newest first and drop the ones that stopped being sources', () => {
  const a = done('Old', 1, [2015]), b = done('New', 2, [2024]), c = game('Dropped now', 3), d = done('Same year, added later', 4, [2024]);
  const s = sugg(10, 'X', [1, 2, 3, 4, 99]);
  assert.deepEqual(sourcesOf(s, [a, b, c, d]).map((g) => g.title), ['Same year, added later', 'New', 'Old']);
});

test('because text: up to three titles, then "and N more"', () => {
  const g = (t) => ({ title: t });
  assert.equal(becauseText([]), '');
  assert.equal(becauseText([g('A')]), 'Because you played A');
  assert.equal(becauseText([g('A'), g('B')]), 'Because you played A and B');
  assert.equal(becauseText([g('A'), g('B'), g('C')]), 'Because you played A, B and C');
  assert.equal(becauseText([g('A'), g('B'), g('C'), g('D'), g('E')]), 'Because you played A, B and C and 2 more');
});

test('ranking: more sources first, then the newest source, then year', () => {
  const a = done('A', 1, [2010]), b = done('B', 2, [2024]), c = done('C', 3, [2012]);
  const resp = { suggestions: [
    sugg(10, 'One old', [1]),
    sugg(11, 'One new', [2]),
    sugg(12, 'Three', [1, 2, 3]),
    sugg(13, 'Two', [1, 3]),
    sugg(14, 'Alone newer year', [2], { year: 2020 }),
  ] };
  const { items } = similarIndex(resp, [a, b, c]);
  assert.deepEqual(items.map((s) => s.title), ['Three', 'Two', 'Alone newer year', 'One new', 'One old']);
});

test('owned games and their families are left out; a remaster of an owned game is owned, a remake is not', () => {
  const src = done('Src', 1, [2020]);
  const own = game('Owned HD', 50);
  const resp = { owned_roots: [8], suggestions: [
    sugg(8, 'Original of the owned HD', [1]),
    sugg(50, 'Owned itself', [1]),
    sugg(51, 'Remaster of owned (parent 50)', [1], { parent: 50 }),
    sugg(52, 'Remake', [1]),
    sugg(53, 'Edition of a known root', [1], { parent: 8 }),
  ] };
  const { items } = similarIndex(resp, [src, own]);
  assert.deepEqual(items.map((s) => s.title), ['Remake']);
});

test('a suggestion that was added since is dropped without a refresh', () => {
  const src = done('Src', 1);
  const resp = { suggestions: [sugg(10, 'Later added', [1])] };
  assert.equal(similarIndex(resp, [src]).items.length, 1);
  assert.equal(similarIndex(resp, [src, game('Later added', 10)]).items.length, 0);
});

test('wishlist games are marked, not suggested', () => {
  const src = done('Src', 1);
  const wish = game('Wished', 10, { status: 'wishlist' });
  const resp = { wishlist_roots: [20], suggestions: [sugg(10, 'Wished', [1]), sugg(21, 'Edition of wished', [1], { parent: 20 }), sugg(11, 'Free', [1])] };
  const { items, wished } = similarIndex(resp, [src, wish]);
  assert.deepEqual(items.map((s) => s.title), ['Free']);
  assert.deepEqual(wished.map((s) => s.title).sort(), ['Edition of wished', 'Wished']);
  assert.equal(wished.find((s) => s.igdb_id === 10).game, wish);
  // a wished game that was moved to the backlog is owned and gone
  wish.status = 'backlog';
  assert.deepEqual(similarIndex(resp, [src, wish]).wished.map((s) => s.title), ['Edition of wished']);
});

test('hidden suggestions are listed apart and survive rebuilding the index (games-changed)', () => {
  const src = done('Src', 1);
  const games = [src];
  const resp = { suggestions: [sugg(10, 'A', [1]), sugg(11, 'B', [1])] };
  setHiddenFlag(resp, 10, true);
  let r = similarIndex(resp, games); // games-changed rebuilds the index from the same answer
  assert.deepEqual([r.items.map((s) => s.title), r.hidden.map((s) => s.title)], [['B'], ['A']]);
  setHiddenFlag(resp, 10, false);
  r = similarIndex(resp, games);
  assert.deepEqual([r.items.length, r.hidden.length], [2, 0]);
});

test('a suggestion with no source left disappears (the game went back to the backlog)', () => {
  const src = done('Src', 1);
  const resp = { suggestions: [sugg(10, 'A', [1])] };
  src.status = 'backlog';
  assert.equal(similarIndex(resp, [src]).items.length, 0);
});

test('suggestions for one game (the drawer)', () => {
  const a = done('A', 1), b = done('B', 2);
  const resp = { suggestions: [sugg(10, 'FromA', [1]), sugg(11, 'FromB', [2]), sugg(12, 'FromBoth', [1, 2])] };
  assert.deepEqual(similarFor(resp, [a, b], a).map((s) => s.title), ['FromBoth', 'FromA']);
  assert.deepEqual(similarFor(resp, [a, b], game('Plain', 3)), []);
});
