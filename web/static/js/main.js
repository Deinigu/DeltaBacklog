// App shell: hash router, navigation (bottom bar / sidebar), theme toggle, service worker.
import { h, icon } from './util.js';
import { load, store, loadLinkPending } from './api.js';
import { renderLibrary } from './library.js';
import { renderViews } from './views.js';
import { renderStats } from './statspage.js';
import { renderSeries } from './seriesview.js';
import { closeDrawer } from './drawer.js';
import { openAdd } from './add.js';
import { renderLink } from './link.js';
import { renderYear, defaultYear } from './yearpage.js';
import { parseYearPath } from './yearreview.js';
import { renderGallery } from './gallerypage.js';
import { renderRankings } from './rankingspage.js';
import { renderRanking } from './rankingedit.js';
import { MORE_PAGES, isMorePath, openMore, closeMore } from './more.js';

const nav = document.getElementById('nav');
const view = document.getElementById('view');
let cleanup = null;

const currentPath = () => location.hash.replace(/^#\/?/, '').split('?')[0] || 'library';

// Later feature branches add their routes here (table, years, add).
function resolve(path) {
  if (path === 'views') return { path, render: renderViews };
  if (path === 'link') return { path, render: renderLink };
  const year = parseYearPath(path);
  if (year !== undefined) return { path: 'years', render: (root) => renderYear(root, year ?? defaultYear()) };
  const rk = /^rankings(?:\/(\d+))?$/.exec(path);
  if (rk) return { path: 'rankings', render: rk[1] ? (root) => renderRanking(root, Number(rk[1])) : renderRankings };
  if (path === 'screenshots') return { path, render: renderGallery };
  if (path === 'stats') return { path, render: renderStats };
  if (path === 'series') return { path, render: renderSeries };
  const m = /^view\/(\d+)$/.exec(path);
  if (m) {
    const v = store.views.find((x) => x.id === Number(m[1]));
    if (v) return { path, render: (root) => renderLibrary(root, { view: structuredClone(v) }) };
  }
  return { path: 'library', render: renderLibrary };
}

const SIDEBAR_ORDER = ['years', 'rankings', 'screenshots', 'series', 'stats', 'views', 'link']; // the insight pages together, then Views

// The sheet / sidebar pages; "Link IGDB" joins them only while the IGDB review queue is not empty.
const morePages = () => (store.linkPending > 0 ? [...MORE_PAGES, { path: 'link', label: 'Link IGDB', icon: 'link' }] : MORE_PAGES);

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
      h('a.more-item', { href: `#/${p.path}`, 'data-path': p.path }, icon(p.icon), h('span', {}, p.label))),
    h('button.more-btn', { type: 'button', 'aria-haspopup': 'dialog', onclick: (e) => openMore(e.currentTarget, resolve(currentPath()).path, morePages()) }, icon('more'), h('span', {}, 'More')),
    h('div.spacer'),
    h('button.theme-toggle', { type: 'button', onclick: toggleTheme }, icon('sun'), h('span', {}, 'Theme')));
  markActive();
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

function toggleTheme() {
  const dark = matchMedia('(prefers-color-scheme: dark)').matches;
  const cur = document.documentElement.dataset.theme || (dark ? 'dark' : 'light');
  const next = cur === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try { localStorage.setItem('theme', next); } catch { /* private mode */ }
}
try {
  const t = localStorage.getItem('theme');
  if (t) document.documentElement.dataset.theme = t;
} catch { /* ignore */ }

async function route() {
  if (!store.loaded) {
    view.replaceChildren(h('div.grid', {}, Array.from({ length: 8 }, () => h('div.skeleton'))));
    try { await load(); } catch {
      view.replaceChildren(h('div.empty', {}, 'Could not load games. ', h('button.btn', { onclick: route }, 'Retry')));
      return;
    }
    buildNav();
    loadLinkPending().then(() => { if (store.linkPending > 0) buildNav(); });
  }
  const r = resolve(currentPath());
  markActive();
  if (cleanup) { cleanup(); cleanup = null; }
  closeDrawer();
  closeMore(false);
  cleanup = r.render(view) || null;
  window.scrollTo(0, 0);
}

buildNav();
// Re-render only when the route (not just the query string) changes: the library rewrites its own query.
let lastPath = currentPath();
window.addEventListener('hashchange', () => {
  const p = currentPath();
  if (p !== lastPath) { lastPath = p; route(); }
});
// Views changed (saved, pinned, renamed...): refresh navigation without re-rendering the open page.
window.addEventListener('views-changed', buildNav);
// A game was added or deleted: rebuild the open page (new genres, platforms... must show up as filter chips).
window.addEventListener('games-reloaded', () => route());

// "+" button: always reachable, above the bottom bar on phones.
document.body.append(h('button.fab', { type: 'button', 'aria-label': 'Add a game', title: 'Add a game (n)', onclick: () => openAdd() }, icon('add')));
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
