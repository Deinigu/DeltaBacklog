// The "Time to beat" block of the filter panel (Library, saved views, the rankings' game picker): quick ranges, a
// from/under range in hours, "no time known" and which estimate to use. The state lives in state.ttb (see ttb.js).
import { h } from './util.js';
import { store } from './api.js';
import { TTB_KINDS, TTB_PRESETS, emptyTtbFilter, ttbRangeActive, parseTime, timeInputValue } from './ttb.js';

const KIND_OPTIONS = [{ key: '', label: 'Shown time' }, ...TTB_KINDS];

/** Returns {el, sync}: put `el` in the panel, call `sync()` after the state changed (it repaints the controls). */
export function createTtbFilter(state, onChange) {
  const f = () => state.ttb;
  const change = () => { sync(); onChange(); };
  const nSel = h('span.n');

  const presets = TTB_PRESETS.map((p) => ({
    p, btn: h('button.chip', { type: 'button', onclick: () => {
      const on = f().min === p.min && f().max === p.max;
      f().min = on ? null : p.min; f().max = on ? null : p.max;
      change();
    } }, p.label),
  }));
  // "No time known" cycles like the other chips: off -> only those games -> hide them -> off.
  const unknownBtn = h('button.chip', { type: 'button', onclick: () => { f().unknown = { '': 'in', in: 'ex', ex: '' }[f().unknown]; change(); } }, 'No time known');

  // Typed as hours ("7.5", "7h30", "45m"); a value that cannot be read leaves the filter as it was and marks the box.
  const box = (key, label) => {
    const input = h('input', { type: 'text', inputmode: 'decimal', autocomplete: 'off', placeholder: 'any', 'aria-label': label,
      oninput: () => {
        const v = parseTime(input.value);
        input.setAttribute('aria-invalid', v === undefined ? 'true' : 'false');
        if (v === undefined) return;
        f()[key] = v;
        if (f().min !== null && f().max !== null && f().max <= f().min) return; // an empty range: wait for the other box
        paint(); onChange();
      },
      onchange: () => { input.value = timeInputValue(f()[key]); input.setAttribute('aria-invalid', 'false'); } });
    return { key, input, el: h('label.ttb-box', {}, h('span', {}, label), input) };
  };
  const from = box('min', 'From (h)'), under = box('max', 'Under (h)');

  const kindSel = h('select.btn', { 'aria-label': 'Which time to beat', onchange: () => { f().kind = kindSel.value; change(); } },
    KIND_OPTIONS.map((k) => h('option', { value: k.key }, k.label)));

  const el = h('details.group.ttb-filter', { hidden: true },
    h('summary', {}, h('span', {}, 'Time to beat'), nSel),
    h('div.chips', {}, presets.map((r) => r.btn), unknownBtn),
    h('div.ttb-range', {}, from.el, under.el, h('label.ttb-box', {}, h('span', {}, 'Use'), kindSel)));

  function paint() {
    const t = f();
    for (const { p, btn } of presets) btn.setAttribute('aria-pressed', String(t.min === p.min && t.max === p.max));
    unknownBtn.dataset.state = t.unknown || 'off';
    unknownBtn.setAttribute('aria-pressed', t.unknown === 'in' ? 'true' : t.unknown === 'ex' ? 'mixed' : 'false');
    nSel.textContent = (ttbRangeActive(t) ? 1 : 0) + (t.unknown ? 1 : 0) || '';
  }
  function sync() {
    if (!state.ttb) state.ttb = emptyTtbFilter();
    kindSel.value = f().kind;
    for (const b of [from, under]) if (document.activeElement !== b.input) b.input.value = timeInputValue(f()[b.key]);
    // Nothing to filter on until some game has a time (the chips would only ever show an empty list).
    const any = store.games.some((g) => TTB_KINDS.some((k) => g[k.key] > 0));
    el.hidden = !any && !ttbRangeActive(f()) && !f().unknown;
    paint();
  }
  sync();
  return { el, sync };
}
