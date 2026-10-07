import test from 'node:test';
import assert from 'node:assert/strict';
import { contrast, readableTextOn, badgeStyle } from '../static/js/colors.js';

// the colours the statuses are seeded with (migration 001)
const STATUS = { playing: '#22c55e', playing_evergreen: '#14b8a6', up_next: '#3b82f6', backlog: '#a855f7', hold: '#f59e0b', evergreen: '#06b6d4', finished: '#84cc16', dropped: '#ef4444', not_interested: '#6b7280' };

test('contrast ratio matches the WCAG reference values', () => {
  assert.equal(Math.round(contrast('#000000', '#ffffff')), 21);
  assert.equal(contrast('#777777', '#777777'), 1);
  assert.ok(Math.abs(contrast('#767676', '#ffffff') - 4.54) < 0.02); // the classic "just passes AA" grey
});

test('every status badge has readable text (at least 4.5:1)', () => {
  for (const [name, hex] of Object.entries(STATUS)) {
    const text = readableTextOn(hex);
    assert.ok(contrast(hex, text) >= 4.5, `${name} ${hex} with ${text}: ${contrast(hex, text).toFixed(2)}`);
  }
});

test('light backgrounds get dark text and dark backgrounds get light text', () => {
  assert.equal(readableTextOn('#84cc16'), '#111111');
  assert.equal(readableTextOn('#f59e0b'), '#111111');
  assert.equal(readableTextOn('#6b7280'), '#ffffff');
  assert.equal(readableTextOn('#111827'), '#ffffff');
});

test('bad input does not throw and the style carries both colours', () => {
  assert.doesNotThrow(() => readableTextOn('not a colour'));
  const s = badgeStyle('#84cc16');
  assert.match(s, /background:#84cc16/);
  assert.match(s, /color:#111111/);
});
