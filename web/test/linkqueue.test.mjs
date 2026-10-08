import test from 'node:test';
import assert from 'node:assert/strict';
import { scoreTone, candidateLabel, remaining, without, progressText, gameMeta } from '../static/js/linkqueue.js';

const it = (id, extra = {}) => ({ game_id: id, title: 'G' + id, year: null, platforms: [], candidates: [], ...extra });

test('score tone thresholds', () => {
  assert.equal(scoreTone(100), 'high');
  assert.equal(scoreTone(90), 'high');
  assert.equal(scoreTone(89), 'mid');
  assert.equal(scoreTone(75), 'mid');
  assert.equal(scoreTone(74), 'low');
});

test('candidate label with and without a year', () => {
  assert.equal(candidateLabel({ name: 'Hades', year: 2020 }), 'Hades (2020)');
  assert.equal(candidateLabel({ name: 'Hades', year: null }), 'Hades');
});

test('skipped games are left out, order kept', () => {
  const items = [it(1), it(2), it(3)];
  assert.deepEqual(remaining(items, new Set([2])).map((x) => x.game_id), [1, 3]);
  assert.deepEqual(remaining(items, new Set()).map((x) => x.game_id), [1, 2, 3]);
});

test('without removes only the decided game and does not mutate', () => {
  const items = [it(1), it(2)];
  assert.deepEqual(without(items, 1).map((x) => x.game_id), [2]);
  assert.equal(items.length, 2);
});

test('progress text', () => {
  assert.equal(progressText([], new Set()), 'All linked');
  assert.equal(progressText([it(1), it(2)], new Set()), '2 to review');
  assert.equal(progressText([it(1), it(2)], new Set([1])), '1 to review · 1 skipped');
  assert.equal(progressText([it(1)], new Set([1])), '1 skipped for now');
});

test('game meta line', () => {
  assert.equal(gameMeta(it(1, { year: 2019, platforms: ['PC', 'Switch'] })), '2019 · PC, Switch');
  assert.equal(gameMeta(it(1)), '');
  assert.equal(gameMeta(it(1, { platforms: ['PC'] })), 'PC');
});
