import test from 'node:test';
import assert from 'node:assert/strict';
import { LOCALE, noSept, formatDate, formatCalendarDate, formatNumber } from '../static/js/format.js';

test('one locale for every page', () => {
  assert.equal(LOCALE, 'en-GB');
});

test('noSept writes September as Sep', () => {
  assert.equal(noSept('4 Sept 2025'), '4 Sep 2025');
  assert.equal(noSept('Septimus'), 'Septimus');
});

test('formatDate: 3 Nov 2026, never 03/11/2026', () => {
  assert.equal(formatDate('2026-11-03T16:56:00Z'), '3 Nov 2026');
  assert.equal(formatDate(Date.UTC(2026, 0, 9, 12)), '9 Jan 2026');
  assert.equal(formatDate('2025-09-04T10:00:00Z'), '4 Sep 2025');
  assert.equal(formatDate(''), '');
  assert.equal(formatDate(null), '');
  assert.equal(formatDate('nonsense'), '');
});

test('formatCalendarDate reads the day as written, with or without a time', () => {
  assert.equal(formatCalendarDate('2026-11-04'), '4 Nov 2026');
  assert.equal(formatCalendarDate('2026-11-04 23:59:59'), '4 Nov 2026');
  assert.equal(formatCalendarDate('2023-11-14T22:13:20Z'), '14 Nov 2023');
  assert.equal(formatCalendarDate('2024-09-06T07:08:09'), '6 Sep 2024');
  assert.equal(formatCalendarDate(''), '');
  assert.equal(formatCalendarDate(undefined), '');
  assert.equal(formatCalendarDate('2024-13-01'), '');
  assert.equal(formatCalendarDate('2024-02-31'), '');
});

test('formatNumber groups with commas whatever the browser locale', () => {
  assert.equal(formatNumber(1240), '1,240');
  assert.equal(formatNumber(7), '7');
  assert.equal(formatNumber(1234567), '1,234,567');
});
