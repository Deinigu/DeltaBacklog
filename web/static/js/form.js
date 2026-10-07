// Form controls shared by the add sheet and the edit form. Each control reads and writes a plain state object `f`.
import { h } from './util.js';
import { store } from './api.js';
import { pickMany } from './picker.js';
import { ownershipLabel } from './ownership.js';
import { yearOptions, normalizeYearToken } from './editpatch.js';

/** Values of a multi-valued field across the library, most used first. */
export const unique = (get) => {
  const tally = new Map();
  for (const g of store.games) for (const v of get(g)) tally.set(v, (tally.get(v) || 0) + 1);
  return [...tally.keys()].sort((a, b) => tally.get(b) - tally.get(a) || a.localeCompare(b));
};

export const field = (label, control) => h('label.frow', {}, h('span.flabel', {}, label), control);

/** A text/number input bound to f[key]. */
export function text(f, key, attrs = {}) {
  const i = h('input', { type: 'text', value: f[key], ...attrs });
  i.addEventListener('input', () => { f[key] = i.value; });
  return i;
}

/** A <datalist> with the given suggestions, created once and refreshed on every call. */
export function ensureDatalist(id, options) {
  let dl = document.getElementById(id);
  if (!dl) { dl = h('datalist', { id }); document.body.append(dl); }
  dl.replaceChildren(...options.map((o) => h('option', { value: o })));
  return id;
}

/**
 * A row showing a list-valued field (f[key]) that opens the multi-select sheet on tap.
 * opts: { suggestions: string[] one-tap chips under the row, hint, validate(text) for new values }
 */
export function listRow(f, label, key, options, { suggestions = [], hint = 'Also on IGDB:', validate } = {}) {
  const val = h('span.fval');
  const chips = suggestions.map((name) => h('button.chip', { type: 'button', onclick: () => {
    f[key] = f[key].includes(name) ? f[key].filter((v) => v !== name) : [...f[key], name];
    draw();
  } }, name));
  const draw = () => {
    val.textContent = f[key].length ? f[key].join(', ') : 'none';
    val.classList.toggle('none', !f[key].length);
    chips.forEach((c) => { const on = f[key].includes(c.textContent); c.dataset.state = on ? 'in' : 'off'; c.setAttribute('aria-pressed', String(on)); });
  };
  draw();
  const edit = async () => {
    const next = await pickMany({ title: label, options, selected: f[key], ...(validate ? { validate } : {}) });
    if (next) { f[key] = next; draw(); }
  };
  const row = h('div.frow', {}, h('span.flabel', {}, label), h('button.listbtn', { type: 'button', onclick: edit }, val, h('span.edit', {}, '✎')));
  if (!chips.length) return row;
  return h('div.rowgroup', {}, row, h('div.suggest', {}, h('span.hint', {}, hint), ...chips));
}

/** "Played in": calendar years and the named periods. */
export const yearsRow = (f) => listRow(f, 'Played in', 'years', yearOptions(), { validate: normalizeYearToken });

export function statusSelect(f) {
  const sel = h('select', { 'aria-label': 'Status' }, store.statuses.map((s) => h('option', { value: s.key, selected: s.key === f.status }, s.label)));
  sel.addEventListener('change', () => { f.status = sel.value; });
  return sel;
}

export function ownershipSelect(f) {
  const names = unique((g) => (g.ownership ? [g.ownership] : []));
  if (f.ownership && !names.includes(f.ownership)) names.push(f.ownership);
  const sel = h('select', { 'aria-label': 'Ownership' }, [h('option', { value: '' }, '—'), ...names.map((o) => h('option', { value: o, selected: o === f.ownership }, ownershipLabel(o)))]);
  sel.addEventListener('change', () => { f.ownership = sel.value; });
  return sel;
}

/** A multi-line box bound to f[key]. Grows with its content. */
export function notesField(f, key = 'notes') {
  const t = h('textarea', { rows: '3', 'aria-label': 'Notes', placeholder: 'Anything worth remembering…' }, f[key]);
  const fit = () => { t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight + 2, 320) + 'px'; };
  t.addEventListener('input', () => { f[key] = t.value; fit(); });
  requestAnimationFrame(fit);
  return t;
}

/** A labelled on/off switch bound to f[key]. */
export function toggleRow(f, key, label) {
  const box = h('input', { type: 'checkbox', checked: f[key] || null, 'aria-label': label });
  box.addEventListener('change', () => { f[key] = box.checked; });
  return h('label.frow.toggle', {}, h('span.flabel', {}, label), box);
}
