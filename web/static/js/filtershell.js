// The parts of the Library's toolbar that other pages share: the search box, the filter panel (a bottom sheet on a phone, a
// side panel from 900px, as a layer: Back closes it, Tab stays inside, focus returns), a collapsible group of chips and the
// small sheet used for sorting. The Library, its saved views and Deals all build their toolbar from these.
import { h, icon, NO_AUTOCORRECT } from './util.js';
import { openLayer } from './layers.js';

/**
 * The search box with its clear button. Returns {el, input}; the page listens to `input` (debounced) itself.
 * onClear runs after the box was emptied and focused again.
 */
export function createSearchBox({ value = '', onClear, placeholder = 'Search games…', label = 'Search' } = {}) {
  const input = h('input', { type: 'search', placeholder, autocomplete: 'off', ...NO_AUTOCORRECT, enterkeyhint: 'search', 'aria-label': label, value });
  const clearBtn = h('button.clear', { type: 'button', 'aria-label': 'Clear search', onclick: () => { input.value = ''; input.focus(); onClear?.(); } }, '✕');
  return { el: h('div.search', {}, icon('search'), input, clearBtn), input };
}

/** The Filters button with its count badge. Returns {btn, setCount}. */
export function createFilterButton(onclick) {
  const badge = h('span.badge', { hidden: true });
  const btn = h('button.btn.open-filters', { type: 'button', 'aria-label': 'Filters', onclick }, icon('filter'), badge);
  return { btn, setCount: (n) => { badge.hidden = !n; badge.textContent = n; btn.setAttribute('aria-label', n ? `Filters, ${n} on` : 'Filters'); } };
}

/**
 * The filter panel and its scrim. body / foot are the nodes of the scrolling part and of the buttons at the bottom.
 * Below 900px the panel is a bottom sheet that is only moved off screen when closed: `inert` keeps Tab and screen readers out of
 * it then. From 900px it is a sidebar that is always there. Returns {panel, scrim, setSheet, isOpen, destroy}.
 */
export function createFilterShell({ body, foot, title = 'Filters' }) {
  const panel = h('aside.filters', { 'aria-label': title },
    h('div.filters-head', {}, h('span', {}, title), h('button.btn.close-sheet', { type: 'button', onclick: () => setSheet(false), 'aria-label': `Close ${title.toLowerCase()}` }, '✕')),
    h('div.filters-body', {}, ...body),
    h('div.filters-foot', {}, ...foot));
  const scrim = h('div.scrim', { onclick: () => setSheet(false) });
  const sheetMode = window.matchMedia('(max-width: 899px)');
  let release = null;
  const isOpen = () => panel.classList.contains('open');
  const sync = () => {
    if (!sheetMode.matches && isOpen()) setSheet(false); // the window grew into a sidebar
    panel.inert = sheetMode.matches && !isOpen();
  };
  function setSheet(on) {
    if (on === isOpen()) return;
    if (on) panel.inert = false;
    panel.classList.toggle('open', on);
    scrim.classList.toggle('show', on);
    if (on) release = openLayer(panel, { requestClose: () => setSheet(false) }); // Back closes it, Tab stays inside
    else { release?.(); release = null; sync(); }
  }
  sheetMode.addEventListener('change', sync);
  sync();
  return { panel, scrim, setSheet, isOpen, destroy: () => sheetMode.removeEventListener('change', sync) };
}

/**
 * One collapsible group of the panel. Returns {el, n}: `n` is the span that shows how many of its filters are on.
 * `lead` (optional) goes between the heading and the chips (the Any / All switch).
 */
export function filterGroup(label, { open = false, hidden = false, lead = null, chips }) {
  const n = h('span.n');
  return { n, el: h('details.group', { open: open || null, hidden: hidden || null }, h('summary', {}, h('span', {}, label), n), lead, chips) };
}

/** An active-filter pill: tapping it removes the filter. cls is '' or '.ex'; kids are its content (the last one is followed by a ✕). */
export function pill(cls, name, onclick, ...kids) {
  return h('button.pill' + cls, { type: 'button', 'aria-label': `Remove filter: ${name}`, onclick }, ...kids.slice(0, -1), h('span', {}, kids[kids.length - 1], h('span', { 'aria-hidden': 'true' }, ' ✕'))); // the label and ✕ are one flex item
}

/**
 * A bottom sheet (a centred card from 900px) with a title, a body and buttons. `foot(close)` returns the buttons.
 * Returns {el: [scrim, sheet] (add both to the page), toggle(on), sheet}. Closed it is only moved off screen and `inert`.
 */
export function createSheet({ label, title, body, foot }) {
  const close = () => toggle(false);
  const scrim = h('div.scrim', { onclick: close });
  const sheet = h('div.sheet', { role: 'dialog', 'aria-modal': 'true', 'aria-label': label, inert: true },
    h('div.filters-head', {}, h('span', {}, title), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕')),
    h('div.filters-body', {}, ...body),
    h('div.filters-foot', {}, ...foot(close)));
  let release = null;
  const onKey = (e) => {
    if (!sheet.isConnected) { document.removeEventListener('keydown', onKey); return; }
    if (e.key === 'Escape' && !document.querySelector('.dialog')) toggle(false);
  };
  function toggle(on) {
    if (on === sheet.classList.contains('open')) return;
    sheet.classList.toggle('open', on);
    scrim.classList.toggle('show', on);
    if (on) {
      sheet.inert = false;
      document.addEventListener('keydown', onKey);
      release = openLayer(sheet, { requestClose: () => toggle(false) });
    } else {
      document.removeEventListener('keydown', onKey);
      release?.(); release = null;
      sheet.inert = true;
    }
  }
  return { el: [scrim, sheet], toggle, sheet };
}
