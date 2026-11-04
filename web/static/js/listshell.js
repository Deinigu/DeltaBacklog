// The toolbar and filter panel of the list pages (Series, Similar games, Upcoming, Hidden): the Library's search box,
// Filters button, sort button, count and active-filter pills, and the panel (a bottom sheet on a phone, a side panel from
// 900px) with its groups of chips. It is the shared parts of filtershell.js put together once, the way the Deals page does
// it by hand; a page only says what its filters are. The filter logic itself is pure and lives in the page's own module.
import { h, icon, debounce, plural } from './util.js';
import { replaceUrl } from './layers.js';
import { hashWithGame, gameIdFromHash } from './gameurl.js';
import { createSearchBox, createFilterButton, createFilterShell, filterGroup, createSheet, pill } from './filtershell.js';

/**
 * o: { label (of the search box), placeholder, noun ('series' → "Show 3 series"; a [one, many] pair for irregular plurals),
 *      getQ(), setQ(text), onChange() (redraw: the page calls paint() from it), onReset() (clear every filter, not the sort),
 *      sorts: [{key, label}] (omit for a page without a sort), getSort(), setSort(key), defaultSort }
 * Returns {toolbar, nodes(results), chips, range, paint, writeUrl, focusSearch, destroy, input}.
 */
export function createListShell(o) {
  const { onChange } = o;
  const groups = [];
  const searchBox = createSearchBox({ value: o.getQ(), placeholder: o.placeholder, label: o.label, onClear: () => { o.setQ(''); onChange(); } });
  const input = searchBox.input;
  input.addEventListener('input', debounce(() => { o.setQ(input.value.trim()); onChange(); }, 90));
  const filterBtn = createFilterButton(() => shell.setSheet(true));
  const sortBtn = o.sorts ? h('button.btn.sort-btn', { type: 'button', 'aria-label': 'Sort', 'aria-haspopup': 'dialog', onclick: () => { paintSort(); sortSheet.toggle(true); } }, icon('sort'), h('span.sort-label')) : null;
  const count = h('span.count', { role: 'status' }); // one live region: "3 of 80 series" is announced as the filters change
  const meta = h('div.meta-row');
  const toolbar = h('div.toolbar', {}, h('div.toolbar-row', {}, searchBox.el, filterBtn.btn, sortBtn), meta);

  // ---- the sort sheet ----
  const sortList = h('div.chips');
  const sortSheet = o.sorts ? createSheet({ label: 'Sort', title: 'Sort by', body: [sortList],
    foot: (close) => [h('button.btn', { type: 'button', onclick: () => { o.setSort(o.defaultSort); onChange(); paintSort(); } }, 'Reset'), h('button.btn.primary', { type: 'button', onclick: close }, 'Done')] }) : null;
  function paintSort() {
    if (!sortList) return;
    sortList.replaceChildren(...o.sorts.map((s) => h('button.chip', { type: 'button', 'data-key': s.key, 'aria-pressed': String(s.key === o.getSort()),
      onclick: () => { o.setSort(s.key); onChange(); paintSort(); sortList.querySelector(`[data-key="${s.key}"]`)?.focus(); } }, s.label)));
  }

  // ---- the groups of the panel ----
  /**
   * A group of chips. c: { options() → [{value, text, title?}] (called on every paint: the options may change),
   * has(value), toggle(value), count(value) → how many results the chip would give, selected() → how many of the group are on,
   * open (the group starts open), lazy (counts are only worked out while the group is open: for long lists), hidden() }
   */
  function chips(label, c) {
    const box = h('div.chips');
    const g = { ...filterGroup(label, { open: !!c.open, chips: box }), box, c, refs: [], sig: null };
    groups.push(g);
    if (c.lazy) g.el.addEventListener('toggle', () => paintGroup(g));
    return g;
  }
  function paintGroup(g) {
    const c = g.c;
    const opts = c.options();
    const sig = opts.map((x) => `${x.value}\u0002${x.text}`).join('\u0001');
    if (sig !== g.sig) { // rebuilt only when the options change, so the chip that has the focus is not lost on a redraw
      g.sig = sig;
      g.refs = opts.map((opt) => {
        const n = h('span.c');
        const btn = h('button.chip', { type: 'button', 'aria-pressed': 'false', title: opt.title || null, onclick: () => { c.toggle(opt.value); onChange(); } }, h('span', {}, opt.text), n);
        return { btn, n, value: opt.value };
      });
      g.box.replaceChildren(...g.refs.map((r) => r.btn));
    }
    g.el.hidden = !opts.length || !!c.hidden?.();
    g.n.textContent = c.selected() || '';
    const counts = !c.lazy || g.el.open;
    for (const r of g.refs) {
      const on = c.has(r.value);
      r.btn.setAttribute('aria-pressed', String(on));
      if (counts) { const k = c.count(r.value); r.n.textContent = k; r.btn.classList.toggle('zero', k === 0 && !on); }
    }
  }

  /** A group with two number boxes (from, to): r: { get() → {from, to}, set(from, to), min, max }. */
  function range(label, r) {
    const box = (name, key) => h('input', { type: 'number', inputmode: 'numeric', min: r.min, max: r.max, step: 1, placeholder: name, 'aria-label': `${label}, ${name.toLowerCase()}`, 'data-key': key });
    const from = box('From', 'from'), to = box('To', 'to');
    const read = (el) => { const v = el.value.trim(); return v === '' ? 0 : Number(v); };
    const commit = debounce(() => { r.set(read(from), read(to)); onChange(); }, 300);
    for (const el of [from, to]) el.addEventListener('input', commit);
    const g = { ...filterGroup(label, { open: true, chips: h('div.range', {}, from, h('span', { 'aria-hidden': 'true' }, '–'), to) }), range: true };
    g.sync = () => {
      const v = r.get();
      if (document.activeElement !== from) from.value = v.from || '';
      if (document.activeElement !== to) to.value = v.to || '';
      g.n.textContent = v.from || v.to ? 1 : '';
    };
    groups.push(g);
    return g;
  }

  const resetBtn = h('button.btn', { type: 'button', onclick: reset }, 'Reset');
  const showBtn = h('button.btn.primary', { type: 'button', onclick: () => shell.setSheet(false) });
  let shell = null;
  function reset() { o.onReset(); input.value = ''; onChange(); }

  /** Builds the panel from the groups made so far; the page calls it once, then puts nodes(results) in the page. */
  function nodes(results) {
    shell = createFilterShell({ body: groups.map((g) => g.el), foot: [resetBtn, showBtn] });
    return [toolbar, h('div.library', {}, shell.panel, shell.scrim, results), ...(sortSheet ? sortSheet.el : [])];
  }

  /**
   * Brings the toolbar and the panel in line with the page state. p: { filters (how many filters are on), shown, of (how many
   * there are without the filters), pills: [{name, kids: [...nodes..., label], remove()}] }
   */
  function paint(p) {
    for (const g of groups) { if (g.range) g.sync(); else paintGroup(g); }
    filterBtn.setCount(p.filters);
    const [one, many] = [].concat(o.noun);
    showBtn.textContent = `Show ${plural(p.shown, one, many)}`;
    if (sortBtn) sortBtn.querySelector('.sort-label').textContent = o.sorts.find((s) => s.key === o.getSort())?.label || '';
    if (sortSheet?.sheet.classList.contains('open')) paintSort();
    const pills = p.pills.map((x) => pill('', x.name, () => { x.remove(); onChange(); }, ...x.kids));
    const revert = (pills.length || o.getQ()) && h('button.pill.reset', { type: 'button', onclick: reset }, 'Reset');
    meta.replaceChildren(...[count, ...pills, revert].filter(Boolean)); // replaceChildren would print a null as the text "null"
    count.textContent = p.of ? `${p.shown} of ${p.of}` : '';
  }

  /** Puts the page state in the address (an open game stays in it) without adding a history entry. */
  function writeUrl(path, qs) {
    replaceUrl(hashWithGame(`#/${path}${qs ? '?' + qs : ''}`, gameIdFromHash(location.hash)));
  }

  // The toolbar is pinned to the top of the screen once the title has scrolled away; the panel sits below it.
  const syncToolbar = () => {
    document.documentElement.style.setProperty('--toolbar-h', toolbar.offsetHeight + 'px');
    document.documentElement.style.setProperty('--head-h', '0px');
  };
  const syncStuck = () => toolbar.classList.toggle('stuck', toolbar.getBoundingClientRect().top <= 1);
  window.addEventListener('scroll', syncStuck, { passive: true });
  const ro = new ResizeObserver(syncToolbar);
  ro.observe(toolbar);
  const onKey = (e) => {
    if (e.key === '/' && document.activeElement !== input && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName) && !document.querySelector('dialog[open]')) { e.preventDefault(); input.focus(); }
    if (e.key === 'Escape') shell?.setSheet(false);
  };
  document.addEventListener('keydown', onKey);
  syncToolbar();

  return {
    toolbar, input, chips, range, nodes, paint, writeUrl,
    destroy: () => { window.removeEventListener('scroll', syncStuck); document.removeEventListener('keydown', onKey); ro.disconnect(); shell?.destroy(); },
  };
}
