import test from 'node:test';
import assert from 'node:assert/strict';
import { popAction, rewindBy, pushAction, trapTarget } from '../static/js/layerlogic.js';
import { guardUnload, hasLayer } from '../static/js/layers.js';
import { plural, toastMs, NO_AUTOCORRECT } from '../static/js/util.js';
import { pageTitle, MORE_PAGES } from '../static/js/more.js';

test('Back closes a layer; stale layer entries are stepped over', () => {
  assert.equal(popAction(0, 1), 'close'); // the drawer's entry was left
  assert.equal(popAction(1, 2), 'close'); // a sheet over the drawer
  assert.equal(popAction(0, 0), 'none'); // plain navigation
  assert.equal(popAction(1, 1), 'none'); // a layer closed its own entry
  assert.equal(popAction(2, 0), 'skip'); // Forward onto an entry whose layer is gone
});

test('closing layers rewinds history to the number of open layers', () => {
  assert.equal(rewindBy(1, 0), -1);
  assert.equal(rewindBy(2, 0), -2);
  assert.equal(rewindBy(2, 1), -1);
  assert.equal(rewindBy(1, 1), 0);
  assert.equal(rewindBy(0, 0), 0);
  assert.equal(rewindBy(0, 1), 0); // never goes forward
});

test('a layer opened right after another closed reuses its history entry', () => {
  assert.equal(pushAction(0, 1), 'push'); // first layer
  assert.equal(pushAction(1, 2), 'push'); // a sheet on the drawer
  assert.equal(pushAction(1, 1), 'replace'); // the drawer was redrawn after a save
  assert.equal(pushAction(2, 1), 'replace');
});

test('Tab wraps inside a layer', () => {
  assert.equal(trapTarget(2, 3, false), 0); // last -> first
  assert.equal(trapTarget(0, 3, true), 2); // first -> last
  assert.equal(trapTarget(1, 3, false), -1); // inside: the browser moves on
  assert.equal(trapTarget(1, 3, true), -1);
  assert.equal(trapTarget(-1, 3, false), 0); // focus is outside (or on the layer itself): come in
  assert.equal(trapTarget(-1, 3, true), 2);
  assert.equal(trapTarget(-1, 0, false), -2); // nothing to focus: stay on the layer
});

test('layers.js loads without a browser and guardUnload hands back its off switch', () => {
  assert.equal(typeof guardUnload(() => false), 'function');
  assert.equal(typeof hasLayer, 'function');
});

test('plural counts one game and many games', () => {
  assert.equal(plural(1, 'game'), '1 game');
  assert.equal(plural(0, 'game'), '0 games');
  assert.equal(plural(12, 'game'), '12 games');
  assert.equal(plural(1, 'game was', 'games were'), '1 game was');
  assert.equal(plural(3, 'game was', 'games were'), '3 games were');
});

test('error toasts stay at least 5 s, other toasts keep their time', () => {
  assert.equal(toastMs('Could not save the change'), 5000);
  assert.equal(toastMs('Could not save the change', 2600), 5000);
  assert.equal(toastMs('Could not delete the game', 8000), 8000);
  assert.equal(toastMs('Something went wrong'), 5000);
  assert.equal(toastMs('Saved'), 2600);
  assert.equal(toastMs('Deleted “Hades”', 6000), 6000);
});

test('title searches switch phone autocorrect off', () => {
  assert.deepEqual(NO_AUTOCORRECT, { spellcheck: 'false', autocapitalize: 'off', autocorrect: 'off' });
});

test('the tab title names the page', () => {
  assert.equal(pageTitle('library'), 'Library · DeltaBacklog');
  assert.equal(pageTitle('stats'), 'Stats · DeltaBacklog');
  assert.equal(pageTitle('years'), 'Year in review · DeltaBacklog');
  assert.equal(pageTitle('link'), 'Link IGDB · DeltaBacklog');
  assert.equal(pageTitle('view/3', [{ id: 3, name: 'Indie RPGs' }]), 'Indie RPGs · DeltaBacklog');
  assert.equal(pageTitle('view/9', []), 'DeltaBacklog');
  assert.equal(pageTitle('nowhere'), 'DeltaBacklog');
  for (const p of MORE_PAGES) assert.match(pageTitle(p.path), /^.+ · DeltaBacklog$/, p.path);
});
