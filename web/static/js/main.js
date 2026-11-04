// App shell: hash router, navigation (bottom bar / sidebar), theme toggle, service worker.
import { h, icon } from './util.js';
import { pruneLayers } from './layers.js';
import { load, store, loadLinkPending, loadDealsUnseen } from './api.js';
import { renderLibrary } from './library.js';
import { renderViews } from './views.js';
import { renderStats } from './statspage.js';
import { renderSeries } from './seriesview.js';
import { renderUpcoming } from './upcomingview.js';
import { renderSimilar } from './similarview.js';
import { renderDeals } from './dealsview.js';
import { renderHidden } from './hiddenview.js';
import { closeDrawer, drawerDirty } from './drawer.js';
import { openAdd } from './add.js';
import { renderLink } from './link.js';
import { renderAttention } from './attentionview.js';
import { renderYear, defaultYear } from './yearpage.js';
import { parseYearPath } from './yearreview.js';
import { renderGallery } from './gallerypage.js';
import { renderRankings } from './rankingspage.js';
import { renderRanking } from './rankingedit.js';
import { MORE_PAGES, isMorePath, openMore, closeMore, pageTitle } from './more.js';

const nav = document.getElementById('nav');
const view = document.getElementById('view');
let cleanup = null;

const safeDecode = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
const currentPath = () => location.hash.replace(/^#\/?/, '').split('?')[0] || 'library';

// Later feature branches add their routes here (table, years, add).
function resolve(path) {
  if (path === 'views') return { path, render: renderViews };
  if (path === 'link') return { path, render: renderLink };
  if (path === 'attention') return { path, render: renderAttention };
  const year = parseYearPath(path);
  if (year !== undefined) return { path: 'years', render: (root) => renderYear(root, year ?? defaultYear()) };
  const rk = /^rankings(?:\/(\d+))?$/.exec(path);
  if (rk) return { path: 'rankings', render: rk[1] ? (root) => renderRanking(root, Number(rk[1])) : renderRankings };
  if (path === 'screenshots') return { path, render: renderGallery };
  if (path === 'stats') return { path, render: renderStats };
  if (path === 'upcoming') return { path, render: renderUpcoming };
  if (path === 'similar') return { path, render: renderSimilar };
  if (path === 'deals') return { path, render: renderDeals };
  if (path === 'hidden') return { path, render: renderHidden };
  const sr = /^series(?:\/(.+))?$/.exec(path); // series/<key>: one series opened (the key is URL-encoded)
  if (sr) return { path: 'series', render: (root) => renderSeries(root, sr[1] ? safeDecode(sr[1]) : null) };
  const m = /^view\/(\d+)$/.exec(path);
  if (m) {
    const v = store.views.find((x) => x.id === Number(m[1]));
    if (v) return { path, render: (root) => renderLibrary(root, { view: structuredClone(v) }) };
  }
  return { path: 'library', render: renderLibrary };
}

const SIDEBAR_ORDER = ['years', 'rankings', 'screenshots', 'series', 'upcoming', 'similar', 'deals', 'stats', 'views', 'hidden', 'attention', 'link']; // the insight pages together, then Views

// The sheet / sidebar pages; "Link IGDB" joins them only while the IGDB review queue is not empty.
const morePages = () => {
  const pages = MORE_PAGES.map((p) => (p.path === 'deals' && store.dealsUnseen > 0 ? { ...p, badge: store.dealsUnseen } : p)); // new price alerts
  return store.linkPending > 0 ? [...pages, { path: 'link', label: 'Link IGDB', icon: 'link' }] : pages;
};
const badge = (n) => (n > 0 ? h('span.nav-badge', { 'aria-hidden': 'true' }, n > 9 ? '9+' : String(n)) : null);

function buildNav() {
  const pinned = store.views.filter((v) => v.pinned);
  nav.replaceChildren(
    h('div.brand', {}, 'DeltaBacklog'),
    h('a', { href: '#/library', 'data-path': 'library' }, icon('library'), h('span', {}, 'Library')),
    pinned.length > 0 && h('div.nav-title', {}, 'My views'),
    // Phones: Library, the first two pinned views and "More" (at most 4 of the 5 slots); the sidebar lists everything.
    ...pinned.map((v, i) => h('a.nav-view', { href: `#/view/${v.id}`, 'data-path': `view/${v.id}`, class: i >= 2 ? 'extra' : '' },
      h('span.emoji', {}, v.icon || '⭐'), h('span', {}, v.name))),
    ...[...morePages()].sort((a, b) => SIDEBAR_ORDER.indexOf(a.path) - SIDEBAR_ORDER.indexOf(b.path)).map((p) =>
      h('a.more-item', { href: `#/${p.path}`, 'data-path': p.path, 'aria-label': p.badge ? `${p.label}, ${p.badge} new price ${p.badge === 1 ? 'alert' : 'alerts'}` : null }, icon(p.icon), h('span', {}, p.label), badge(p.badge))),
    h('button.more-btn', { type: 'button', 'aria-haspopup': 'dialog', 'aria-label': store.dealsUnseen > 0 ? 'More, new price alerts on Deals' : null, onclick: (e) => openMore(e.currentTarget, resolve(currentPath()).path, morePages()) }, icon('more'), h('span', {}, 'More'), badge(store.dealsUnseen)),
    h('div.spacer'),
    h('button.theme-toggle', { type: 'button', onclick: toggleTheme }, icon('sun'), h('span', {}, 'Theme')));
  markActive();
  syncTheme();
}

function markActive() {
  const path = resolve(currentPath()).path;
  for (const a of nav.querySelectorAll('a')) {
    if (a.dataset.path === path) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  const more = nav.querySelector('.more-btn'); // highlighted while inside one of the pages it holds
  if (more) { if (isMorePath(path, morePages())) more.setAttribute('aria-current', 'page'); else more.removeAttribute('aria-current'); }
}

const THEME_COLOR = { dark: '#0f1115', light: '#f5f6f8' }; // the page backgrounds (--bg in styles.css)
const systemDark = matchMedia('(prefers-color-scheme: dark)');
const currentTheme = () => document.documentElement.dataset.theme || (systemDark.matches ? 'dark' : 'light');
/** The browser bar colour and the Theme button's name follow the theme that is really on, forced or from the system. */
function syncTheme() {
  const cur = currentTheme();
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_COLOR[cur]);
  nav.querySelector('.theme-toggle')?.setAttribute('aria-label', `Theme: ${cur}, switch to ${cur === 'dark' ? 'light' : 'dark'}`);
}
function toggleTheme() {
  const next = currentTheme() === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try { localStorage.setItem('theme', next); } catch { /* private mode */ }
  syncTheme();
}
try {
  const t = localStorage.getItem('theme');
  if (t) document.documentElement.dataset.theme = t;
} catch { /* ignore */ }
systemDark.addEventListener('change', syncTheme);

let stopFocusWatch = null;
/**
 * Keyboard and screen reader users start at the top of the new page, not on the link they used. Pages that draw
 * after a fetch (Deals, Upcoming) replace what was focused, so for a moment the focus follows: while it is lost
 * (on the body or the page itself) it goes to the h1 as it appears. Any key press or tap ends that.
 */
function focusPage() {
  stopFocusWatch?.();
  const go = () => {
    const head = view.querySelector('h1');
    if (head) head.tabIndex = -1;
    (head || view).focus({ preventScroll: true });
  };
  const lost = () => { const a = document.activeElement; return !a || a === document.body || a === view; };
  go();
  const ctl = new AbortController();
  const mo = new MutationObserver(() => { if (lost()) go(); });
  const stop = () => { mo.disconnect(); clearTimeout(timer); ctl.abort(); stopFocusWatch = null; };
  const timer = setTimeout(stop, 2000);
  mo.observe(view, { childList: true, subtree: true });
  for (const ev of ['keydown', 'pointerdown']) addEventListener(ev, stop, { capture: true, signal: ctl.signal });
  stopFocusWatch = stop;
}

/** Render the page for the current route. `moved`: the route really changed (not a rebuild of the same page). */
async function route({ moved = false } = {}) {
  if (!store.loaded) {
    view.replaceChildren(h('div.grid', {}, Array.from({ length: 8 }, () => h('div.skeleton'))));
    try { await load(); } catch {
      view.replaceChildren(h('div.empty', {}, 'Could not load games. ', h('button.btn', { onclick: () => route({ moved }) }, 'Retry')));
      return;
    }
    buildNav();
    loadLinkPending().then(() => { if (store.linkPending > 0) buildNav(); });
    loadDealsUnseen().then(() => { if (store.dealsUnseen > 0) buildNav(); });
  }
  const r = resolve(currentPath());
  markActive();
  syncFab(r.path);
  if (cleanup) { cleanup(); cleanup = null; }
  // A drawer with unsaved edits is never closed behind the owner's back; Back, ✕ and the scrim ask first. A rebuild of
  // the same page (a game was added) leaves the drawer alone.
  if (moved && !drawerDirty()) closeDrawer();
  closeMore(false);
  cleanup = r.render(view) || null;
  pruneLayers(); // sheets that lived in the old page are gone now
  document.title = pageTitle(r.path, store.views);
  window.scrollTo(0, 0);
  if (moved) focusPage();
}

buildNav();
// Re-render only when the route (not just the query string) changes: the library rewrites its own query.
let lastPath = currentPath();
window.addEventListener('hashchange', () => {
  const p = currentPath();
  if (p !== lastPath) { lastPath = p; route({ moved: true }); }
});
// Views changed (saved, pinned, renamed...): refresh navigation without re-rendering the open page.
window.addEventListener('views-changed', buildNav);
// Price alerts seen (Deals opened) or new ones read: redraw the badge.
window.addEventListener('deals-unseen', buildNav);
// A game was added or deleted: rebuild the open page (new genres, platforms... must show up as filter chips).
window.addEventListener('games-reloaded', () => route());

// "+" button: above the bottom bar on phones, only where the list of games is (Library, saved views and shelves);
// the other pages have no use for it and it would cover their last row. The "n" key still adds a game everywhere.
const fab = h('button.fab', { type: 'button', 'aria-label': 'Add a game', title: 'Add a game (n)', onclick: () => openAdd() }, icon('add'));
document.body.append(fab);
const showsFab = (path) => path === 'library' || path.startsWith('view/');
function syncFab(path) {
  fab.hidden = !showsFab(path);
  document.body.classList.toggle('has-fab', !fab.hidden);
}
document.addEventListener('keydown', (e) => {
  if (e.key === 'n' && !e.ctrlKey && !e.metaKey && !e.altKey && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName) && !document.querySelector('.dialog, .sheet.open, .more-wrap')) {
    e.preventDefault();
    openAdd();
  }
});
route();

if ('serviceWorker' in navigator) {
  // relative, so under a sub-path (/backlog/sw.js) the worker's scope is that sub-path
  addEventListener('load', () => navigator.serviceWorker.register('sw.js').catch(() => {}));
}
