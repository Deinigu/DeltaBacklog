// Modal layers (the game drawer, sheets, dialogs, the screenshot viewer): one place for what every layer needs.
//  - History: opening a layer pushes a history entry (same URL), so the Back button / swipe-back closes the top layer
//    instead of leaving the page. A layer closed another way (✕, Escape, Save) pops its entry again.
//  - Tab is trapped inside the top layer; focus goes into it on open and back to the opener on close.
//  - The page behind does not scroll (body scroll lock).
//  - A beforeunload warning while something registered with guardUnload() has unsaved edits.
// A layer's own code still does its DOM work; it calls openLayer(el, {requestClose}) after adding `el` to the page
// and the returned release() when it takes `el` away. requestClose() is what Back calls: it may ask "Discard your
// changes?" first and simply not close.

import { popAction, rewindBy, pushAction, trapTarget, carryUrl } from './layerlogic.js';

// ---------------------------------------------------------------- the stack

const stack = []; // {el, requestClose, trap, opener}
const layerOf = () => history.state?.layer ?? 0;
let rewindTimer = 0;
let lastHref = typeof location !== 'undefined' ? location.href : ''; // the page's URL now, as the layers last saw it
let carry = null; // the URL to restore on the entry a rewind lands on

/** Change the URL of the current history entry (a page's own query). Use this instead of history.replaceState: it keeps the layer state and lets closing layers carry the new URL to the entry below. */
export function replaceUrl(url) {
  history.replaceState(history.state, '', url);
  lastHref = location.href;
}

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])';
const visible = (n) => n.getClientRects().length > 0 && !n.closest('[inert]');
export const focusables = (el) => [...el.querySelectorAll(FOCUSABLE)].filter(visible);

// Scroll lock: counted, so two layers on top of each other release it only when both are gone.
let locks = 0;
function lockScroll() {
  if (locks++ > 0) return;
  const gap = window.innerWidth - document.documentElement.clientWidth; // the scrollbar's width: keep the page from shifting
  if (gap > 0) document.body.style.paddingRight = `${gap}px`;
  document.body.classList.add('no-scroll');
}
function unlockScroll() {
  if (--locks > 0) return;
  locks = 0;
  document.body.classList.remove('no-scroll');
  document.body.style.paddingRight = '';
}

/** Drop layers whose element has left the page without release() (a page change replaced it). */
export function pruneLayers() {
  for (let i = stack.length - 1; i >= 0; i--) if (!stack[i].el.isConnected) detach(stack[i]);
}

function detach(layer) {
  const i = stack.indexOf(layer);
  if (i < 0) return;
  stack.splice(i, 1);
  unlockScroll();
  clearTimeout(rewindTimer);
  rewindTimer = setTimeout(rewind, 0); // after the current work: a layer closed and another opened at once keep one entry
}

function rewind() {
  const by = rewindBy(layerOf(), stack.length);
  if (!by) return;
  carry = location.href;
  history.go(by);
}

/**
 * Register `el` (already in the page) as the top layer.
 * opts.requestClose: what Back does (may ask first). opts.trap: keep Tab inside (default true; a native <dialog> does it itself).
 * Returns release(restoreFocus = true): call it when `el` is closed. It gives focus back to the opener and pops the history entry.
 */
export function openLayer(el, { requestClose, trap = true } = {}) {
  pruneLayers();
  const opener = document.activeElement;
  const layer = { el, requestClose, trap, opener };
  stack.push(layer);
  lockScroll();
  const state = { ...history.state, layer: stack.length };
  if (pushAction(layerOf(), stack.length) === 'push') history.pushState(state, '');
  else history.replaceState(state, '');
  if (trap && !el.contains(document.activeElement)) {
    if (!el.hasAttribute('tabindex')) el.tabIndex = -1;
    el.focus({ preventScroll: true });
  }
  return (restoreFocus = true) => {
    if (!stack.includes(layer)) return;
    detach(layer);
    if (!restoreFocus) return;
    // Back to where the layer was opened, unless focus already went somewhere on purpose. Never into a text field:
    // on a phone that would bring the keyboard back up.
    const a = document.activeElement;
    const lost = !a || a === document.body || el.contains(a) || !a.isConnected;
    if (!lost) return;
    const usable = (n) => n?.isConnected && typeof n.focus === 'function' && !n.closest('[inert]');
    if (usable(opener) && !opener.matches('input, textarea, select')) opener.focus({ preventScroll: true });
    else {
      // The opener was a text field (a dialog opened by Back while typing): the layer below, if any, takes the focus
      const below = stack[stack.length - 1];
      if (below && usable(below.el)) below.el.focus({ preventScroll: true });
    }
  };
}

/** True while any layer is open. */
export const hasLayer = () => { pruneLayers(); return stack.length > 0; };

// ---------------------------------------------------------------- unsaved edits and reloads

const unsaved = new Set();
/** Warn before the page is closed or reloaded while `isDirty()` is true. Returns the function that stops warning. */
export function guardUnload(isDirty) {
  unsaved.add(isDirty);
  return () => unsaved.delete(isDirty);
}
// The listeners below need a browser; util.js imports this file, and the node tests import util.js.
if (typeof window !== 'undefined' && typeof history !== 'undefined') {
  // Back / swipe-back: close the top layer (asking first when it has unsaved edits).
  window.addEventListener('popstate', () => {
    pruneLayers();
    const keep = carryUrl(carry, location.href);
    carry = null;
    if (keep) history.replaceState(history.state, '', keep); // a layer closed: the entry it landed on gets the page's current URL
    const act = popAction(layerOf(), stack.length);
    if (act === 'skip') { history.back(); return; }
    if (act !== 'close') { lastHref = location.href; return; }
    const top = stack[stack.length - 1];
    // Put the entry back first (with the URL the page had before Back): the layer may stay open (a "Discard your
    // changes?" answered with Cancel), and a dialog opened by the question pushes its own entry on top of it.
    history.pushState({ ...history.state, layer: stack.length }, '', carryUrl(lastHref, location.href) ?? location.href);
    lastHref = location.href;
    top.requestClose?.();
  });
  window.addEventListener('hashchange', () => { lastHref = location.href; });

  // A reload keeps history.state: do not let a stale layer index from the last session confuse the first push.
  if (history.state?.layer) history.replaceState({ ...history.state, layer: 0 }, '');

  // Tab stays inside the top layer.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Tab' || e.defaultPrevented) return;
    pruneLayers();
    const top = stack[stack.length - 1];
    if (!top || !top.trap) return;
    const items = focusables(top.el);
    const t = trapTarget(items.indexOf(document.activeElement), items.length, e.shiftKey);
    if (t === -1) return;
    e.preventDefault();
    (t === -2 ? top.el : items[t]).focus({ preventScroll: true });
  }, true);

  window.addEventListener('beforeunload', (e) => {
    if (![...unsaved].some((f) => f())) return;
    e.preventDefault();
    e.returnValue = ''; // some browsers only warn when this is set
  });
}
