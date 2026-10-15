// Multi-select sheet: search, tick existing values, optionally add a new one. Used for the table's list cells.
import { h, norm, icon } from './util.js';

/**
 * opts: { title, options: string[], selected: string[], allowNew, label?: (v) => string, validate?: (text) => string|null }
 * Resolves to the new selection (array) or null when dismissed without changes.
 */
export function pickMany({ title, options, selected, allowNew = true, label = (v) => v, validate = (t) => t, hint = '', invalidHint = '' }) {
  return new Promise((resolve) => {
    const chosen = new Set(selected);
    const all = [...new Set([...selected, ...options])];
    const search = h('input', { type: 'search', placeholder: allowNew ? 'Search or add…' : 'Search…', autocomplete: 'off', enterkeyhint: 'done', 'aria-label': 'Search' });
    const list = h('div.pick-list');

    const finish = (value) => { scrim.remove(); sheet.remove(); document.removeEventListener('keydown', onKey); resolve(value); };
    const done = () => {
      const next = [...chosen];
      const same = next.length === selected.length && next.every((v) => selected.includes(v));
      finish(same ? null : next);
    };

    function draw() {
      const q = norm(search.value.trim());
      const shown = all.filter((v) => !q || norm(label(v)).includes(q));
      // ticked values first, so what a game has is always visible at the top
      shown.sort((a, b) => (chosen.has(b) - chosen.has(a)));
      const rows = shown.map((v) => h('button.pick-row', { type: 'button', 'aria-pressed': String(chosen.has(v)), onclick: () => { chosen.has(v) ? chosen.delete(v) : chosen.add(v); draw(); } },
        h('span.box', {}, chosen.has(v) ? '✓' : ''), label(v)));
      const typed = search.value.trim();
      const clean = typed ? validate(typed) : null;
      if (allowNew && clean && !all.some((v) => norm(v) === norm(clean))) {
        rows.unshift(h('button.pick-row.add', { type: 'button', onclick: () => { all.unshift(clean); chosen.add(clean); search.value = ''; draw(); search.focus(); } }, h('span.box', {}, '+'), `Add “${clean}”`));
      }
      if (allowNew && typed && !clean && invalidHint && /^\d+$/.test(typed)) rows.unshift(h('div.empty', {}, invalidHint));
      list.replaceChildren(...(rows.length ? rows : [h('div.empty', {}, 'Nothing matches.')]));
    }
    search.addEventListener('input', draw);
    search.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); list.querySelector('.pick-row.add')?.click(); }
    });

    const scrim = h('div.scrim.show', { onclick: done });
    scrim.style.zIndex = 95;
    const sheet = h('div.sheet.open', { role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div.filters-head', {}, h('span', {}, title), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: done }, '✕')),
      h('div.filters-body', {}, h('div.search.pick-search', {}, icon('search'), search), ...(hint ? [h('p.hint', {}, hint)] : []), list),
      h('div.filters-foot', {}, h('button.btn', { type: 'button', onclick: () => finish(null) }, 'Cancel'), h('button.btn.primary', { type: 'button', onclick: done }, 'Done')));
    sheet.style.zIndex = 96;
    const onKey = (e) => { if (e.key === 'Escape') finish(null); };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, sheet);
    draw();
    search.focus({ preventScroll: true });
  });
}
