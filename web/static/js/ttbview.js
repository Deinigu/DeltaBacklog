// The "Time to beat" block of the game drawer: three tiles, a source note, typed edits and the HowLongToBeat picker.
// The formatting and parsing live in ttb.js (pure, unit-tested); this file only builds the DOM.
import { h, toast } from './util.js';
import { patchGame, hltbSearch } from './api.js';
import { TTB_KINDS, ttbTiles, hasTimes, ttbSourceNote, parseTime, timeInputValue, hltbPatch, formatTime, playedOfText } from './ttb.js';

const SITE_DOWN = "HowLongToBeat can't be reached right now. Try again later, or type the times yourself.";

/** The block for one game. It redraws itself after a save; `game` is the live object from the store. */
export function ttbSection(game) {
  const root = h('section.ttb', { 'aria-label': 'Time to beat' });
  let mode = 'view'; // 'view' | 'edit' | 'hltb'
  let dirty = null; // set while the edit boxes are open: are there typed changes?
  let search = 0; // counts HowLongToBeat searches, so an answer that arrives late is ignored

  const tiles = () => h('div.ttb-tiles', {}, ttbTiles(game).map((t) =>
    h('div.ttb-tile' + (t.minutes ? '' : '.none'), {},
      h('span.ttb-label', {}, t.label),
      h('span.ttb-value', { title: t.minutes ? `${t.minutes} min` : null }, t.text || '–'),
      h('span.ttb-bar', { 'aria-hidden': 'true' }, h('i', { style: `width:${Math.round(t.share * 100)}%` })))));

  const heading = (note) => h('div.ttb-head', {}, h('h3', {}, 'Time to beat'), note && h('span.ttb-src', {}, note));

  function viewMode() {
    const known = hasTimes(game);
    const actions = h('div.ttb-actions', {},
      h('button.btn', { type: 'button', onclick: () => go('edit') }, known ? '✎ Edit times' : '✎ Add times'),
      h('button.btn', { type: 'button', onclick: () => go('hltb') }, 'Get from HowLongToBeat'));
    const played = playedOfText(game);
    return [heading(ttbSourceNote(game)), known ? tiles() : h('p.ttb-empty', {}, 'No time yet'),
      played ? h('p.ttb-played', {}, h('b', {}, played), ' played on Steam') : null, actions].filter(Boolean);
  }

  function editMode() {
    const inputs = TTB_KINDS.map((k, i) => {
      const input = h('input', { type: 'text', inputmode: 'text', autocomplete: 'off', value: timeInputValue(game[k.key]), id: `ttb-in-${game.id}-${i}`, placeholder: 'e.g. 7.5' });
      const err = h('span.ttb-err', { role: 'alert' });
      const unit = h('em', {}, 'h');
      // the "h" suffix only fits a plain number of hours: "45m" or "7h30" already say their unit
      const showUnit = () => { unit.textContent = /[a-z:]/i.test(input.value) ? '' : 'h'; };
      input.addEventListener('input', showUnit);
      showUnit();
      return { k, input, err, unit };
    });
    function save() {
      const patch = {};
      let bad = false;
      for (const { k, input, err } of inputs) {
        const v = parseTime(input.value);
        err.textContent = '';
        input.removeAttribute('aria-invalid');
        if (v === undefined) {
          bad = true;
          err.textContent = 'Use hours like 7.5 or 7h30, or minutes like 45m.';
          input.setAttribute('aria-invalid', 'true');
        } else patch[k.key] = v;
      }
      if (bad) { inputs.find((x) => x.input.getAttribute('aria-invalid'))?.input.focus(); return; }
      const unchanged = TTB_KINDS.every((k) => (patch[k.key] ?? null) === (game[k.key] ?? null));
      if (unchanged) { go('view'); return; }
      patch.ttb_source = 'manual';
      commit(patch, 'Times saved');
    }
    dirty = () => inputs.some(({ k, input }) => input.value.trim() !== timeInputValue(game[k.key]));
    return [heading(null),
      h('form.ttb-form', { onsubmit: (e) => { e.preventDefault(); save(); } },
        inputs.map(({ k, input, err, unit }) => h('label.ttb-field', { for: input.id }, h('span', {}, k.label), h('span.ttb-in', {}, input, unit), err)),
        h('p.hint', {}, 'Hours (7.5, 7,5 or 7h30) or minutes (45m). Leave a field empty for no time.'),
        h('div.ttb-actions', {},
          h('button.btn', { type: 'button', onclick: () => go('view') }, 'Cancel'),
          h('button.btn.primary', { type: 'submit' }, 'Save')))];
  }

  function hltbMode() {
    const queryIn = h('input', { type: 'search', value: game.title, 'aria-label': 'Title to search on HowLongToBeat', autocomplete: 'off', enterkeyhint: 'search' });
    const out = h('div.ttb-results', { 'aria-live': 'polite' });
    const run = async () => {
      const q = queryIn.value.trim();
      if (q.length < 2) { out.replaceChildren(h('p.hint', {}, 'Type at least two letters.')); return; }
      const mine = ++search;
      out.replaceChildren(h('p.hint', {}, 'Searching HowLongToBeat…'));
      try {
        const data = await hltbSearch(q, q === game.title ? game.release_year || 0 : 0);
        if (mine !== search) return;
        if (!data.results.length) { out.replaceChildren(h('p.hint', {}, `Nothing found for “${q}”. Try a shorter title.`)); return; }
        out.replaceChildren(h('div.ttb-cands', {}, data.results.map((c) => candidate(c))));
        out.querySelector('button')?.focus({ preventScroll: true });
      } catch (e) {
        if (mine !== search) return;
        const msg = e.status === 502 && e.message ? `${e.message}. Try again later, or type the times yourself.` : SITE_DOWN;
        out.replaceChildren(h('p.ttb-fail', { role: 'alert' }, msg), h('button.btn', { type: 'button', onclick: run }, 'Try again'));
      }
    };
    const candidate = (c) => h('button.ttb-cand', { type: 'button', onclick: () => commit(hltbPatch(c), 'Times saved from HowLongToBeat') },
      h('span.ttb-cand-name', {}, c.name, c.year ? h('small', {}, ` (${c.year})`) : null),
      h('span.ttb-cand-times', {}, [[TTB_KINDS[0].label, c.hastily], [TTB_KINDS[1].label, c.normally], [TTB_KINDS[2].label, c.completely]]
        .map(([label, m]) => h('span', {}, h('b', {}, formatTime(m) || '–'), ' ', label))));
    queueMicrotask(run);
    return [heading(null),
      h('form.ttb-search', { onsubmit: (e) => { e.preventDefault(); run(); } }, queryIn, h('button.btn', { type: 'submit' }, 'Search')),
      out,
      h('div.ttb-actions', {}, h('button.btn', { type: 'button', onclick: () => go('view') }, 'Cancel'))];
  }

  async function commit(patch, message) {
    root.setAttribute('aria-busy', 'true');
    try {
      await patchGame(game.id, patch);
      toast(message);
      go('view');
    } catch {
      toast('Could not save the times');
    } finally {
      root.removeAttribute('aria-busy');
    }
  }

  function go(next) {
    search++;
    mode = next;
    dirty = null;
    root.replaceChildren(...{ view: viewMode, edit: editMode, hltb: hltbMode }[mode]());
    if (mode === 'edit') root.querySelector('input')?.focus({ preventScroll: true });
    if (mode === 'hltb') root.querySelector('.ttb-search input')?.focus({ preventScroll: true });
    if (mode === 'view' && root.dataset.touched) root.querySelector('.ttb-actions .btn')?.focus({ preventScroll: true });
    root.dataset.touched = '1';
  }
  // Escape leaves the edit boxes or the picker; it must not also close the drawer (and lose what was typed)
  root.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && mode !== 'view') { e.stopPropagation(); go('view'); }
  });
  root.isDirty = () => !!dirty?.();
  go('view');
  return root;
}
