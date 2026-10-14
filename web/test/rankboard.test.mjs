import test from 'node:test';
import assert from 'node:assert/strict';
import {
  boardFrom, toBody, allGames, locate, place, shift, carry, addGames, removeGame, keepOnly, updateTier, insertTier, moveTier,
  removeTier, restoreTier, duplicateTier, dropIndex, tileWidth, tileHeight, wrapText, exportLayout,
} from '../static/js/rankboard.js';

const board = () => boardFrom({
  tiers: [{ id: 10, label: 'S', color: '#ff7f7f' }, { id: 11, label: 'Me ha gustado mucho', color: '#ffbf7f' }, { id: 12, label: 'Vaya mierda', color: '#6e6e6e' }],
  entries: [
    { game_id: 1, tier_id: 10 }, { game_id: 2, tier_id: 10 }, { game_id: 3, tier_id: 11 },
    { game_id: 4, tier_id: null }, { game_id: 5, tier_id: null }, { game_id: 1, tier_id: null }, // a repeated game is ignored
  ],
});
const shape = (s) => [...s.tiers.map((t) => `${t.id}:${t.games.join(',')}`), `-:${s.pool.join(',')}`].join(' | ');

test('boardFrom reads the server payload', () => {
  assert.equal(shape(board()), '10:1,2 | 11:3 | 12: | -:4,5');
  assert.deepEqual(allGames(board()), [1, 2, 3, 4, 5]);
  assert.deepEqual(toBody(board()).tiers[0], { id: 10, label: 'S', color: '#ff7f7f', games: [1, 2] });
  assert.deepEqual(toBody(board()).pool, [4, 5]);
});

test('locate finds a game in a tier or in the pool', () => {
  assert.deepEqual(locate(board(), 2), { tier: 10, index: 1 });
  assert.deepEqual(locate(board(), 5), { tier: null, index: 1 });
  assert.equal(locate(board(), 99), null);
});

test('place moves a game, counting the index without the game itself', () => {
  const b = board();
  assert.equal(shape(place(b, 4, 10, 0)), '10:4,1,2 | 11:3 | 12: | -:5');
  assert.equal(shape(place(b, 1, 10, 1)), '10:2,1 | 11:3 | 12: | -:4,5'); // already in the list: lands at index 1
  assert.equal(shape(place(b, 1, 10, 0)), '10:1,2 | 11:3 | 12: | -:4,5');
  assert.equal(shape(place(b, 2, 11)), '10:1 | 11:3,2 | 12: | -:4,5');
  assert.equal(shape(place(b, 3, null, 0)), '10:1,2 | 11: | 12: | -:3,4,5');
  assert.equal(shape(place(b, 9, 12)), '10:1,2 | 11:3 | 12:9 | -:4,5'); // a new game is added
  assert.equal(shape(place(b, 1, 10, -5)), '10:1,2 | 11:3 | 12: | -:4,5');
  assert.equal(place(b, 1, 99), b); // unknown tier: unchanged
  assert.equal(shape(b), '10:1,2 | 11:3 | 12: | -:4,5'); // the input is never changed
});

test('shift moves a game earlier or later inside its list', () => {
  const b = board();
  assert.equal(shape(shift(b, 2, -1)), '10:2,1 | 11:3 | 12: | -:4,5');
  assert.equal(shape(shift(b, 1, 1)), '10:2,1 | 11:3 | 12: | -:4,5');
  assert.equal(shift(b, 1, -1), b);
  assert.equal(shift(b, 2, 1), b);
  assert.equal(shape(shift(b, 4, 1)), '10:1,2 | 11:3 | 12: | -:5,4');
  assert.equal(shift(b, 99, 1), b);
});

test('carry moves a game to the next or previous tier', () => {
  const b = board();
  assert.equal(shape(carry(b, 1, 1)), '10:2 | 11:3,1 | 12: | -:4,5');
  assert.equal(shape(carry(b, 3, -1)), '10:1,2,3 | 11: | 12: | -:4,5');
  assert.equal(shape(carry(b, 1, -1)), '10:2 | 11:3 | 12: | -:4,5,1'); // above the first tier: back to the pool
  assert.equal(shape(carry(b, 3, 1)), '10:1,2 | 11: | 12:3 | -:4,5');
  assert.equal(shape(carry(b, 4, 1)), '10:1,2,4 | 11:3 | 12: | -:5'); // from the pool, down enters the first tier
  assert.equal(shape(carry(b, 4, -1)), '10:1,2 | 11:3 | 12:4 | -:5'); // up enters the last
  assert.equal(shape(carry(place(b, 3, 12), 3, 1)), '10:1,2 | 11: | 12: | -:4,5,3'); // below the last tier: the pool
});

test('adding, removing and pruning games', () => {
  const b = board();
  assert.equal(shape(addGames(b, [5, 6, 6, 7])), '10:1,2 | 11:3 | 12: | -:4,5,6,7');
  assert.equal(addGames(b, [1, 4]), b);
  assert.equal(shape(removeGame(b, 2)), '10:1 | 11:3 | 12: | -:4,5');
  assert.equal(shape(removeGame(b, 4)), '10:1,2 | 11:3 | 12: | -:5');
  assert.equal(shape(keepOnly(b, (id) => id % 2 === 1)), '10:1 | 11:3 | 12: | -:5');
});

test('tier edits', () => {
  const b = board();
  assert.equal(updateTier(b, 11, { label: 'Hola', color: '#000000' }).tiers[1].label, 'Hola');
  assert.equal(updateTier(b, 11, { label: 'Hola' }).tiers[1].color, '#ffbf7f');
  assert.equal(b.tiers[1].label, 'Me ha gustado mucho');
  assert.deepEqual(insertTier(b, { id: 20, label: 'N', color: '#fff' }, 1).tiers.map((t) => t.id), [10, 20, 11, 12]);
  assert.deepEqual(insertTier(b, { id: 20, label: 'N', color: '#fff' }).tiers.map((t) => t.id), [10, 11, 12, 20]);
  assert.deepEqual(insertTier(b, { id: 20, label: 'N', color: '#fff' }).tiers[3].games, []);
  assert.deepEqual(moveTier(b, 10, 2).tiers.map((t) => t.id), [11, 12, 10]);
  assert.equal(moveTier(b, 10, 5), b);
  assert.equal(moveTier(b, 10, 0), b);
  assert.deepEqual(duplicateTier(b, 10, 30).tiers.map((t) => `${t.id}${t.label}${t.games.length}`), ['10S2', '30S0', '11Me ha gustado mucho1', '12Vaya mierda0']);
});

test('removing a tier sends its games to the pool and can be undone', () => {
  const b = board();
  const { state, removed } = removeTier(b, 10);
  assert.equal(shape(state), '11:3 | 12: | -:4,5,1,2');
  assert.deepEqual(removed, { tier: { id: 10, label: 'S', color: '#ff7f7f' }, index: 0, games: [1, 2] });
  const back = restoreTier(state, removed, 77);
  assert.equal(shape(back), '77:1,2 | 11:3 | 12: | -:4,5');
  assert.equal(back.tiers[0].label, 'S');
  // a game that left the pool meanwhile is not brought back
  const moved = place(state, 1, 12);
  assert.equal(shape(restoreTier(moved, removed, 77)), '77:2 | 11:3 | 12:1 | -:4,5');
  assert.equal(removeTier(b, 99).removed, null);
});

test('dropIndex counts the tiles before the pointer, row by row', () => {
  // two rows of three 10x10 tiles, 2 px apart
  const rects = [0, 1, 2, 3, 4, 5].map((i) => {
    const left = (i % 3) * 12, top = Math.floor(i / 3) * 12;
    return { left, top, right: left + 10, bottom: top + 10 };
  });
  assert.equal(dropIndex(rects, 1, 1), 0);   // left of the first tile's centre
  assert.equal(dropIndex(rects, 8, 1), 1);   // right of the first centre
  assert.equal(dropIndex(rects, 35, 1), 3);  // end of the first row
  assert.equal(dropIndex(rects, 1, 13), 3);  // start of the second row
  assert.equal(dropIndex(rects, 20, 11), 3); // in the gap between the rows: after the first row only
  assert.equal(dropIndex(rects, 100, 100), 6);
  assert.equal(dropIndex([], 5, 5), 0);
});

test('tile sizes', () => {
  assert.equal(tileWidth('M', 390), 64);
  assert.equal(tileWidth('M', 1280), 90);
  assert.equal(tileWidth('S', 390) < tileWidth('L', 390), true);
  assert.equal(tileWidth('?', 1280), 90);
  assert.equal(tileHeight(90), 135);
  assert.equal(tileHeight(64), 96);
});

test('wrapText wraps long labels by words and cuts a word that cannot fit', () => {
  const m = (s) => s.length * 10;
  assert.deepEqual(wrapText(m, 'Me ha cambiado la vida', 100), ['Me ha', 'cambiado', 'la vida']);
  assert.deepEqual(wrapText(m, 'S', 100), ['S']);
  assert.deepEqual(wrapText(m, '', 100), ['']);
  assert.deepEqual(wrapText(m, 'Supercalifragilistico', 100), ['Supercalif', 'ragilistic', 'o']);
  assert.deepEqual(wrapText(m, '  a   b  ', 100), ['a b']);
});

test('exportLayout stacks tiers with wrapped tile rows', () => {
  const o = { perRow: 4, tileW: 90, tileH: 135, gap: 6, labelW: 140, pad: 16, headH: 60 };
  const l = exportLayout({ counts: [0, 4, 9], ...o });
  assert.equal(l.width, 16 * 2 + 140 + 4 * 96 + 6);
  assert.deepEqual(l.rows.map((r) => r.lines), [1, 1, 3]);
  assert.deepEqual(l.rows.map((r) => r.h), [147, 147, 3 * 141 + 6]);
  assert.equal(l.rows[0].y, 60);
  assert.equal(l.rows[1].y, 60 + 147 + 6);
  assert.deepEqual(l.rows[2].tile(5), { x: 16 + 140 + 6 + 96, y: l.rows[2].y + 6 + 141 });
  const last = l.rows[2];
  assert.equal(l.height, last.y + last.h + 16);
});

test('textOn picks a readable text colour', async () => {
  const { textOn } = await import('../static/js/rankboard.js');
  assert.equal(textOn('#ffffff'), '#14161a');
  assert.equal(textOn('#ffdf7f'), '#14161a');
  assert.equal(textOn('#000000'), '#ffffff');
  assert.equal(textOn('#6e6e6e'), '#ffffff');
});

test('fitExportLayout adds tiles per row until the picture fits a phone canvas', async () => {
  const { fitExportLayout } = await import('../static/js/rankboard.js');
  const o = { perRow: 10, tileW: 90, tileH: 135, gap: 6, labelW: 150, pad: 16, headH: 64 };
  const small = fitExportLayout({ counts: [3, 8, 20], ...o });
  assert.equal(small.perRow, 10); // fits as asked
  const big = fitExportLayout({ counts: [1000], ...o });
  assert.ok(big.perRow > 10 && big.layout.width * big.layout.height <= 16_000_000 && big.layout.height <= 14000, `perRow ${big.perRow}`);
  assert.equal(fitExportLayout({ counts: [50000], ...o }).layout, null);
});
