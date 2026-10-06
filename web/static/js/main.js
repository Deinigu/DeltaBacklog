// App shell: hash router, navigation (bottom bar / sidebar), theme toggle, service worker.
import { h, icon } from './util.js';
import { load, store } from './api.js';
import { renderLibrary } from './library.js';
import { closeDrawer } from './drawer.js';

// Later feature branches add their views here (shelves, years, table, add).
const ROUTES = [
  { path: 'library', label: 'Library', icon: 'library', render: renderLibrary },
];

const nav = document.getElementById('nav');
const view = document.getElementById('view');
let cleanup = null;

function buildNav() {
  nav.replaceChildren(
    h('div.brand', {}, 'DeltaBacklog'),
    ...ROUTES.map((r) => h('a', { href: `#/${r.path}`, 'data-path': r.path }, icon(r.icon), h('span', {}, r.label))),
    h('div.spacer'),
    h('button.theme-toggle', { type: 'button', onclick: toggleTheme }, icon('sun'), h('span', {}, 'Theme')));
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
  const path = (location.hash.replace(/^#\/?/, '').split('?')[0]) || 'library';
  const r = ROUTES.find((x) => x.path === path) || ROUTES[0];
  for (const a of nav.querySelectorAll('a')) {
    if (a.dataset.path === r.path) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  if (cleanup) { cleanup(); cleanup = null; }
  closeDrawer();
  if (!store.loaded) {
    view.replaceChildren(h('div.grid', {}, Array.from({ length: 8 }, () => h('div.skeleton'))));
    try { await load(); } catch (e) {
      view.replaceChildren(h('div.empty', {}, 'Could not load games. ', h('button.btn', { onclick: route }, 'Retry')));
      return;
    }
  }
  cleanup = r.render(view) || null;
  window.scrollTo(0, 0);
}

buildNav();
// Re-render only when the route (not just the query string) changes: library filters rewrite the hash themselves.
let lastPath = null;
window.addEventListener('hashchange', () => {
  const p = location.hash.replace(/^#\/?/, '').split('?')[0] || 'library';
  if (p !== lastPath) { lastPath = p; route(); }
});
lastPath = location.hash.replace(/^#\/?/, '').split('?')[0] || 'library';
route();

if ('serviceWorker' in navigator) {
  addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => {}));
}
