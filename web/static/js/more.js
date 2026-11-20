// The phone navigation's "More" sheet: the pages that do not fit in the bottom bar (see main.js).
import { h, icon } from './util.js';
import { openLayer } from './layers.js';

/** Pages in the sheet, in order. `path` is the route (`data-path`), `icon` a util.js icon name; a page may carry a `badge` count (main.js adds one to Deals for new price alerts). */
export const MORE_PAGES = [
  { path: 'series', label: 'Series', icon: 'series' },
  { path: 'upcoming', label: 'Upcoming', icon: 'upcoming' },
  { path: 'similar', label: 'Similar games', icon: 'similar' },
  { path: 'deals', label: 'Deals', icon: 'deals' },
  { path: 'stats', label: 'Stats', icon: 'stats' },
  { path: 'years', label: 'Year in review', icon: 'years' },
  { path: 'rankings', label: 'Rankings', icon: 'rankings' },
  { path: 'screenshots', label: 'Screenshots', icon: 'screenshots' },
  { path: 'views', label: 'Views', icon: 'views' },
  { path: 'hidden', label: 'Hidden', icon: 'hidden' },
  { path: 'attention', label: 'Needs attention', icon: 'attention' },
];
export const isMorePath = (path, pages = MORE_PAGES) => pages.some((p) => p.path === path);

const OTHER_PAGES = { library: 'Library', link: 'Link IGDB' };
/** The tab title for a route (`resolve(...).path`): "Stats · DeltaBacklog". `views` are the saved views, for `view/<id>`. */
export function pageTitle(path, views = []) {
  const m = /^view\/(\d+)$/.exec(path);
  const name = m ? views.find((v) => v.id === Number(m[1]))?.name : MORE_PAGES.find((p) => p.path === path)?.label ?? OTHER_PAGES[path];
  return name ? `${name} · DeltaBacklog` : 'DeltaBacklog';
}

let open = null;

export function closeMore(refocus = true) {
  if (!open) return;
  const { wrap, opener, release } = open;
  open = null;
  wrap.remove();
  document.removeEventListener('keydown', onKey, true);
  release(false);
  if (refocus && opener?.isConnected) opener.focus();
}

function onKey(e) {
  if (!open) return;
  if (e.key === 'Escape') { e.preventDefault(); closeMore(); }
}

/** Open the sheet; `current` is the active route, `opener` gets the focus back when it closes. */
export function openMore(opener, current, pages = MORE_PAGES) {
  closeMore(false);
  const link = (p) => h('a.more-link', { href: `#/${p.path}`, 'aria-current': p.path === current ? 'page' : null, onclick: () => closeMore(false) },
    icon(p.icon), h('span', {}, p.label), p.badge ? h('span.nav-badge', { 'aria-label': `${p.badge} new` }, p.badge > 9 ? '9+' : String(p.badge)) : null);
  const sheet = h('div.more-sheet', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'More pages' },
    h('div.more-links', {}, pages.map(link)),
    h('button.btn.more-close', { type: 'button', onclick: () => closeMore() }, 'Close'));
  const wrap = h('div.more-wrap', {}, h('div.scrim', { onclick: () => closeMore() }), sheet);
  document.body.append(wrap);
  open = { wrap, opener, release: openLayer(wrap, { requestClose: () => closeMore() }) }; // Tab stays inside, Back closes it
  document.addEventListener('keydown', onKey, true);
  sheet.querySelector('a')?.focus();
}
