import test from 'node:test';
import assert from 'node:assert/strict';
import { gameIdFromHash, hashWithGame } from '../static/js/gameurl.js';

test('reads the game id from a hash', () => {
  assert.equal(gameIdFromHash('#/library?game=123'), 123);
  assert.equal(gameIdFromHash('#/library?status=backlog&game=7&sort=title'), 7);
  assert.equal(gameIdFromHash('#/stats'), null);
  assert.equal(gameIdFromHash('#/library?game=abc'), null);
  assert.equal(gameIdFromHash('#/library?game=0'), null);
  assert.equal(gameIdFromHash('#/library?mygame=5'), null);
  assert.equal(gameIdFromHash(''), null);
});

test('sets and removes the game, keeping the other parameters byte for byte', () => {
  assert.equal(hashWithGame('#/library', 5), '#/library?game=5');
  assert.equal(hashWithGame('#/library?q=a%20b&sort=title', 5), '#/library?q=a%20b&sort=title&game=5');
  assert.equal(hashWithGame('#/library?game=5', 9), '#/library?game=9');
  assert.equal(hashWithGame('#/library?game=5', null), '#/library');
  assert.equal(hashWithGame('#/library?q=x&game=5&sort=title', null), '#/library?q=x&sort=title');
  assert.equal(hashWithGame('#/stats', null), '#/stats');
  assert.equal(hashWithGame('#/view/3', 12), '#/view/3?game=12');
});
