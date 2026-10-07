import test from 'node:test';
import assert from 'node:assert/strict';
import { ownershipIcon, ownershipLabel, isUsualOwnership, FALLBACK_ICON } from '../static/js/ownership.js';

test('every ownership value in the library has its own emoji', () => {
  const names = ['Owned', 'Pirated', 'Free to play', 'Family Share', 'Borrowed', 'Nope'];
  const icons = names.map(ownershipIcon);
  assert.ok(icons.every((i) => i && i !== FALLBACK_ICON), 'all mapped');
  assert.equal(new Set(icons).size, names.length, 'all different');
});

test('matching ignores case and extra spaces', () => {
  assert.equal(ownershipIcon('  FAMILY   share '), ownershipIcon('Family Share'));
  assert.equal(ownershipIcon('free to play'), ownershipIcon('Free to play'));
});

test('unknown values get the generic tag, empty values get nothing', () => {
  assert.equal(ownershipIcon('Gifted'), FALLBACK_ICON);
  assert.equal(ownershipIcon(''), '');
  assert.equal(ownershipIcon(null), '');
  assert.equal(ownershipLabel(null), '');
});

test('label is the emoji and the original spelling', () => {
  assert.equal(ownershipLabel('Pirated'), '🏴‍☠️ Pirated');
  assert.equal(ownershipLabel('Gifted'), `${FALLBACK_ICON} Gifted`);
});

test('only plain Owned counts as the usual case', () => {
  assert.equal(isUsualOwnership(' owned '), true);
  for (const n of ['Pirated', 'Free to play', 'Family Share', 'Borrowed', 'Nope', '']) assert.equal(isUsualOwnership(n), false, n);
});
