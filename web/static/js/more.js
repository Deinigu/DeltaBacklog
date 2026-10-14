// The phone navigation's "More" sheet: the pages that do not fit in the bottom bar (see main.js).
import { h, icon } from './util.js';

/** Pages in the sheet, in order. `path` is the route (`data-path`), `icon` a util.js icon name. */
export const MORE_PAGES = [
  { path: 'series', label: 'Series', icon: 'series' },
  { path: 'stats', label: 'Stats', icon: 'stats' },
  { path: 'years', label: 'Year in review', icon: 'years' },
  { path: 'rankings', label: 'Rankings', icon: 'rankings' },
  { path: 'views', label: 'Views', icon: 'views' },
];
export const isMorePath = (path, pages = MORE_PAGES) => pages.some((p) => p.path === path);

let open = null;

export function closeMore(refocus = true) {
  if (!open) return;
  const { wrap, opener } = open;
  open = null;
  wrap.remove();
  document.removeEventListener('keydown', onKey, true);
  if (refocus && opener?.isConnected) opener.focus();
}

function onKey(e) {
  if (!open) return;
  if (e.key === 'Escape') { e.preventDefault(); closeMore(); return; }
  if (e.key !== 'Tab') return;
  const items = [...open.wrap.querySelectorAll('a, button')];
  const first = items[0], last = items[items.length - 1];
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
}

/** Open the sheet; `current` is the active route, `opener` gets the focus back when it closes. */
export function openMore(opener, current, pages = MORE_PAGES) {
  closeMore(false);
  const link = (p) => h('a.more-link', { href: `#/${p.path}`, 'aria-current': p.path === current ? 'page' : null, onclick: () => closeMore(false) },
    icon(p.icon), h('span', {}, p.label));
  const sheet = h('div.more-sheet', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'More pages' },
    h('div.more-links', {}, pages.map(link)),
    h('button.btn.more-close', { type: 'button', onclick: () => closeMore() }, 'Close'));
  const wrap = h('div.more-wrap', {}, h('div.scrim', { onclick: () => closeMore() }), sheet);
  document.body.append(wrap);
  open = { wrap, opener };
  document.addEventListener('keydown', onKey, true);
  sheet.querySelector('a')?.focus();
}
