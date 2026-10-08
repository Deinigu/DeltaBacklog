// App shell: hash router, navigation (bottom bar / sidebar), theme toggle, service worker.
import { h, icon } from './util.js';
import { load, store } from './api.js';
import { renderLibrary } from './library.js';
import { renderViews } from './views.js';
import { renderStats } from './statspage.js';
import { renderSeries } from './seriesview.js';
import { closeDrawer } from './drawer.js';
import { openAdd } from './add.js';

const nav = document.getElementById('nav');
const view = document.getElementById('view');
let cleanup = null;

const currentPath = () => location.hash.replace(/^#\/?/, '').split('?')[0] || 'library';

// Later feature branches add their routes here (table, years, add).
function resolve(path) {
  if (path === 'views') return { path, render: renderViews };
  if (path === 'stats') return { path, render: renderStats };
  if (path === 'series') return { path, render: renderSeries };
  const m = /^view\/(\d+)$/.exec(path);
  if (m) {
    const v = store.views.find((x) => x.id === Number(m[1]));
    if (v) return { path, render: (root) => renderLibrary(root, { view: structuredClone(v) }) };
  }
  return { path: 'library', render: renderLibrary };
}

function buildNav() {
  const pinned = store.views.filter((v) => v.pinned);
  nav.replaceChildren(
    h('div.brand', {}, 'DeltaBacklog'),
    h('a', { href: '#/library', 'data-path': 'library' }, icon('library'), h('span', {}, 'Library')),
    pinned.length > 0 && h('div.nav-title', {}, 'My views'),
    ...pinned.map((v, i) => h('a.nav-view', { href: `#/view/${v.id}`, 'data-path': `view/${v.id}`, class: i >= 3 ? 'extra' : '' },
      h('span.emoji', {}, v.icon || '⭐'), h('span', {}, v.name))),
    h('a', { href: '#/series', 'data-path': 'series' }, icon('series'), h('span', {}, 'Series')),
    h('a', { href: '#/views', 'data-path': 'views' }, icon('views'), h('span', {}, 'Views')),
    h('a', { href: '#/stats', 'data-path': 'stats' }, icon('stats'), h('span', {}, 'Stats')),
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
  }
  const r = resolve(currentPath());
  markActive();
  if (cleanup) { cleanup(); cleanup = null; }
  closeDrawer();
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
  if (e.key === 'n' && !e.ctrlKey && !e.metaKey && !e.altKey && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName) && !document.querySelector('.dialog, .sheet.open')) {
    e.preventDefault();
    openAdd();
  }
});
route();

if ('serviceWorker' in navigator) {
  addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => {}));
}
