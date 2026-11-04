import test from 'node:test';
import assert from 'node:assert/strict';
import { errorText, couldNot } from '../static/js/util.js';

test('couldNot says what failed and what to do next', () => {
  assert.equal(couldNot('save the change'), 'Could not save the change. Check that the server is reachable, then try again.');
});

test('errorText keeps a message the server sent', () => {
  const err = Object.assign(new Error('POST api/x: 409'), { status: 409, data: { error: 'That game already exists' } });
  assert.equal(errorText('add the game', err), 'That game already exists');
});

test('errorText hides transport errors and bare status messages', () => {
  assert.equal(errorText('save', new TypeError('Failed to fetch')), couldNot('save'));
  assert.equal(errorText('save', Object.assign(new Error('POST api/x: 500'), { status: 500, data: null })), couldNot('save'));
  assert.equal(errorText('save', undefined), couldNot('save'));
});
