// The edit sheet of one expansion (a bottom sheet; a card from 900px; Back closes it): status, ownership, years played, notes,
// time to beat, Hide and "Refresh from IGDB". Every control saves at once (like the drawer's quick actions); nothing is
// guessed before the server answers. The rules live in expansions.js (pure).
import { h, gradientFor, initials, lazyImg, toast, toastAction, toastError, glyphLabel } from './util.js';
import { store, findExpansion, patchExpansion, refreshExpansions } from './api.js';
import { createSheet } from './filtershell.js';
import { pickMany } from './picker.js';
import { ownershipLabel } from './ownership.js';
import { yearsOf, yearOptions, normalizeYearToken, libraryYears } from './editpatch.js';
import { ttbSection } from './ttbview.js';
import { expansionCoverURL, withYear, expansionsOf, isDLC, isAdded } from './expansions.js';
import { openDrawer } from './drawer.js';

/** Ownership values in use across games and expansions, most used first (plus the two everyone needs). */
function ownershipNames(current) {
  const tally = new Map([['Owned', 0], ['Nope', 0]]);
  const add = (n) => { if (n) tally.set(n, (tally.get(n) || 0) + 1); };
  for (const g of store.games) { add(g.ownership); for (const x of expansionsOf(g)) add(x.ownership); }
  add(current);
  return [...tally.keys()].sort((a, b) => tally.get(b) - tally.get(a) || a.localeCompare(b));
}

let openSheet = null; // one sheet at a time

/** Close the open expansion sheet (a page change). */
export function closeExpansionSheet() { openSheet?.close(); }

/** Open the sheet of expansion `id`. Resolves nothing; changes reach every view through games-changed. */
export function openExpansionSheet(id) {
  const found = findExpansion(id);
  if (!found) return;
  closeExpansionSheet();
  const { x, game } = found;
  const noun = isDLC(x) ? 'DLC' : 'Expansion';
  const body = h('div.exp-sheet');

  const status = h('select', { 'aria-label': 'Status' }, [h('option', { value: '' }, 'No status'), ...store.statuses.map((s) => h('option', { value: s.key }, s.label))]);
  const ownership = h('select', { 'aria-label': 'Ownership' });
  const yearsBtn = h('button.listbtn', { type: 'button' }, h('span.fval'), h('span.edit', { 'aria-hidden': 'true' }, '✎'));
  const notes = h('textarea', { rows: '3', 'aria-label': 'Notes', placeholder: 'Anything worth remembering…', maxlength: '20000' });
  const fit = () => { notes.style.height = 'auto'; notes.style.height = Math.min(notes.scrollHeight + 2, 320) + 'px'; };

  async function save(patch, what = 'save the change') {
    try { await patchExpansion(x.id, patch); } catch { toastError(what); }
    paint();
  }
  status.addEventListener('change', () => save({ status: status.value }));
  ownership.addEventListener('change', () => save({ ownership: ownership.value }));
  notes.addEventListener('input', fit);
  notes.addEventListener('change', () => { if (notes.value !== (x.notes || '')) save({ notes: notes.value }); }); // fires when the box loses focus: also on Close
  yearsBtn.addEventListener('click', async () => {
    const next = await pickMany({ title: 'Played in', options: yearOptions(undefined, libraryYears(store.games)), selected: yearsOf(x),
      validate: normalizeYearToken, hint: 'Older year? Type it (4 digits) in the box above.', invalidHint: 'Years go from 1970 to 2100.' });
    if (!next) return;
    await save({ years: next.filter((v) => /^\d{4}$/.test(v)).map(Number), periods: next.filter((v) => /^(childhood|always)$/i.test(v)).map((v) => v.toLowerCase()) }, 'save the years');
  });

  function paint() {
    status.value = x.status || '';
    const names = ownershipNames(x.ownership);
    ownership.replaceChildren(h('option', { value: '' }, '—'), ...names.map((n) => h('option', { value: n, selected: n === x.ownership }, ownershipLabel(n))));
    ownership.value = x.ownership || '';
    const years = yearsOf(x);
    const val = yearsBtn.querySelector('.fval');
    val.textContent = years.length ? years.join(', ') : 'none';
    val.classList.toggle('none', !years.length);
    yearsBtn.setAttribute('aria-label', `Played in: ${val.textContent}. Edit`);
    if (document.activeElement !== notes) { notes.value = x.notes || ''; fit(); }
    addOwned.hidden = isAdded(x) || !!x.ownership || !!x.hidden;
    hideBtn.textContent = x.hidden ? 'Show again' : `Hide this ${isDLC(x) ? 'DLC' : 'expansion'}`;
    hint.textContent = x.hidden ? 'Hidden: it is not counted anywhere and not listed.' : '';
  }

  const url = expansionCoverURL(x);
  const hideBtn = h('button.btn', { type: 'button', onclick: toggleHidden });
  const hint = h('p.hint');
  async function toggleHidden() {
    const hide = !x.hidden;
    try { await patchExpansion(x.id, { hidden: hide }); } catch { toastError(hide ? 'hide it' : 'show it again'); return; }
    paint();
    if (hide) { sheet.toggle(false); toastAction(`Hidden “${x.title}”`, 'Undo', () => patchExpansion(x.id, { hidden: false }).catch(() => toastError('undo that'))); }
    else toast('It is back in the list');
  }
  const refresh = h('button.btn', { type: 'button', onclick: async () => {
    refresh.disabled = true;
    try { await refreshExpansions(game.id); toast('Read again from IGDB'); } catch (e) { toast(e.status === 503 ? 'IGDB is not set up (keys are missing from .env)' : 'IGDB could not be reached. Try again later.'); }
    refresh.disabled = false;
    if (!findExpansion(x.id)) { sheet.toggle(false); return; } // IGDB dropped it
    paint();
  } }, glyphLabel('↻', 'Refresh from IGDB'));

  const head = h('div.exp-head', {},
    h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(x.title)}` }, initials(x.title))),
    h('div', {}, h('b.exp-name', {}, withYear(x)),
      h('button.exp-game', { type: 'button', onclick: () => { sheet.toggle(false); openDrawer(game); } }, `${noun} of ${game.title}`)));
  const addOwned = h('button.btn.primary.exp-add', { type: 'button', onclick: async () => {
    await save({ ownership: 'Owned' }, 'add it');
    if (addOwned.hidden) { ownership.focus({ preventScroll: true }); toast(`Added “${x.title}”`); } // the button is gone: keep the focus (and the screen reader) here
  } }, 'Add as Owned');
  const field = (label, control) => h('label.frow', {}, h('span.flabel', {}, label), control);
  body.append(head, addOwned,
    field('Status', status),
    field('Ownership', ownership),
    h('div.frow', {}, h('span.flabel', {}, 'Played in'), yearsBtn),
    h('div.frow.col', {}, h('span.flabel', {}, 'Notes'), notes),
    ttbSection(x, { patch: (p) => patchExpansion(x.id, p), year: x.year, key: `x${x.id}` }),
    h('div.exp-actions', {}, hideBtn, refresh), hint);

  const sheet = createSheet({ label: `${noun}: ${x.title}`, title: noun, body: [body], foot: (close) => [h('button.btn.primary', { type: 'button', onclick: close }, 'Done')] });
  sheet.el[0].classList.add('exp-scrim'); // above the game drawer (see styles.css)
  sheet.sheet.classList.add('exp-sh');
  document.body.append(...sheet.el);
  paint();
  const done = () => {
    obs.disconnect();
    if (openSheet?.sheet === sheet) openSheet = null;
    setTimeout(() => sheet.el.forEach((n) => n.remove()), 250);
  };
  // the sheet closes by its ✕, the scrim, Escape or Back (layers.js): all of them drop the "open" class
  const obs = new MutationObserver(() => { if (!sheet.sheet.classList.contains('open')) done(); });
  obs.observe(sheet.sheet, { attributes: true, attributeFilter: ['class'] });
  openSheet = { sheet, close: () => sheet.toggle(false) };
  requestAnimationFrame(() => { sheet.toggle(true); status.focus({ preventScroll: true }); });
}
