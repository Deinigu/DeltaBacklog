// Game detail drawer: facts at a glance, quick status/favorite changes, and an Edit mode for every field.
import { h, gradientFor, initials, toast, confirmDialog } from './util.js';
import { store, coverURL, patchGame, deleteGame } from './api.js';
import { openCoverPicker, pickCandidate } from './add.js';
import { ownershipLabel } from './ownership.js';
import { badgeStyle } from './colors.js';
import { formFromGame, validateForm, buildPatch, applyCandidate, yearsOf } from './editpatch.js';
import { seriesPosition } from './series.js';
import { ttbParts } from './ttb.js';
import { unique, field, text, ensureDatalist, listRow, yearsRow, ownershipSelect, notesField, toggleRow } from './form.js';

let open = null;

/** "3 of 5 in Name" by release year, or "Name (5 games)" while some game of the series has no year. */
function seriesFact(game) {
  const p = seriesPosition(game, store.games);
  if (!p) return game.series;
  return p.position ? `${p.position} of ${p.total} in ${p.name}` : `${p.name} (${p.total} games)`;
}

/** Status picker and favorite toggle; changes apply immediately and refresh every open view. */
function quickActions(game) {
  const sel = h('select.btn', { 'aria-label': 'Status', onchange: () => change({ status: sel.value }) },
    store.statuses.map((s) => h('option', { value: s.key, selected: s.key === game.status }, s.label)));
  const fav = h('button.btn', { type: 'button', 'aria-pressed': String(game.favorite), onclick: () => change({ favorite: !game.favorite }) }, game.favorite ? '★ Favorite' : '☆ Favorite');
  async function change(patch) {
    try { await patchGame(game.id, patch); } catch { toast('Could not save the change'); }
    sel.value = game.status;
    fav.textContent = game.favorite ? '★ Favorite' : '☆ Favorite';
    fav.setAttribute('aria-pressed', String(game.favorite));
    const st = store.statusByCode.get(game.status);
    const badge = open?.drawer.querySelector('.cover .st');
    if (badge && st) { badge.textContent = st.label; badge.setAttribute('style', badgeStyle(st.color)); }
  }
  return h('div.quick', {}, sel, fav);
}

const STORE_LABELS = { steam: 'Steam', gog: 'GOG', epic: 'Epic Games', official: 'Official site' };

/** "Quick 8 h, Usual 12 h, Complete 40 h" from IGDB, or null when IGDB has no time. */
const ttbText = (game) => ttbParts(game).map((p) => `${p.label} ${p.text}`).join(' · ') || null;

/** Buttons to the game's store pages and official site (links come from IGDB; only http(s) is ever opened). */
function storeLinks(game) {
  const order = Object.keys(STORE_LABELS);
  const links = (game.links || []).filter((l) => STORE_LABELS[l.kind] && /^https?:\/\//.test(l.url))
    .sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind));
  if (!links.length) return null;
  return h('div.store-links', {}, links.map((l) => h('a.btn', { href: l.url, target: '_blank', rel: 'noopener noreferrer' }, STORE_LABELS[l.kind], ' ↗')));
}

export function closeDrawer() {
  if (!open) return;
  open.drawer.remove();
  open.scrim.remove();
  document.removeEventListener('keydown', open.onKey);
  open = null;
}

/** Close because the user asked to: when there are unsaved edits, ask first. */
async function requestClose() {
  if (open?.isDirty?.()) {
    const ok = await confirmDialog({ title: 'Discard your changes?', message: 'You have edits that are not saved.', confirm: 'Discard' });
    if (!ok) return;
  }
  closeDrawer();
}

export function openDrawer(game) {
  closeDrawer();
  const st = store.statusByCode.get(game.status);
  const url = coverURL(game);

  const fact = (label, value) => (value == null || value === '' || (Array.isArray(value) && !value.length)) ? null : [
    h('dt', {}, label),
    h('dd', {}, Array.isArray(value) ? h('div.tags', {}, value.map((v) => h('span.tag', {}, v))) : String(value)),
  ];

  const title = h('h2', {}, game.title);
  const readView = h('div.readview', {},
    h('dl.facts', {},
      fact('Platforms', game.platforms),
      fact('Genres', game.genres),
      fact('Themes', game.themes),
      fact('Game modes', game.modes),
      fact('Perspective', game.perspectives),
      fact('Time to beat', ttbText(game)),
      fact('Series', seriesFact(game)),
      fact('Developer', game.developer),
      fact('Released', game.release_year),
      fact('Played in', yearsOf(game)),
      fact('Played with', game.players),
      fact('Ownership', game.ownership && ownershipLabel(game.ownership)),
      fact('VR', game.vr ? 'Yes' : null),
      fact('100% completed', game.completed ? 'Yes' : null)),
    game.notes && h('div.notes', {}, game.notes),
    storeLinks(game),
    h('div.drawer-actions', {},
      h('button.btn.primary', { type: 'button', onclick: startEdit }, '✎ Edit'),
      h('button.btn', { type: 'button', onclick: () => openCoverPicker(game) }, 'Change cover'),
      h('button.btn.danger-text', { type: 'button', onclick: remove }, 'Delete game')));
  const editView = h('div.editview', { hidden: true });

  const scrim = h('div.scrim.show', { onclick: requestClose });
  scrim.style.zIndex = 55;
  const quick = quickActions(game);
  const drawer = h('div.drawer', { role: 'dialog', 'aria-modal': 'true', 'aria-label': game.title },
    h('button.close', { onclick: requestClose, 'aria-label': 'Close' }, '✕'),
    h('div.cover', {},
      url ? h('img', { src: url, alt: '' }) : h('div.ph', { style: `background:${gradientFor(game.title)}` }, initials(game.title)),
      st && h('span.st', { style: badgeStyle(st.color) }, st.label)),
    h('div.drawer-body', {}, title, quick, readView, editView));

  async function remove() {
    const ok = await confirmDialog({ title: `Delete “${game.title}”?`, message: 'This removes the game, its years played and its cover. It cannot be undone.', confirm: 'Delete' });
    if (!ok) return;
    try {
      await deleteGame(game.id);
      closeDrawer();
      toast(`Deleted “${game.title}”`);
    } catch { toast('Could not delete the game'); }
  }

  // ---------------------------------------------------------------- edit mode
  function startEdit() {
    let f = formFromGame(game);
    const isDirty = () => Object.keys(buildPatch(game, f)).length > 0;
    if (open) open.isDirty = isDirty;

    const formMount = h('div'); // the part that is redrawn; the Save/Cancel bar below it stays put
    function render() {
      const seriesIn = text(f, 'series', { list: ensureDatalist('dl-edit-series', unique((g) => (g.series ? [g.series] : []))), 'aria-label': 'Series' });
      const devIn = text(f, 'developer', { list: ensureDatalist('dl-edit-dev', unique((g) => (g.developer ? [g.developer] : []))), 'aria-label': 'Developer' });
      const yearIn = text(f, 'release_year', { type: 'number', inputmode: 'numeric', placeholder: 'e.g. 2025', 'aria-label': 'Release year' });
      const titleIn = text(f, 'title', { 'aria-label': 'Title', required: true });
      const linked = f.igdb_id
        ? h('div.dupe.ok', {}, h('span', {}, `Linked to IGDB (#${f.igdb_id}).`), h('button.pill', { type: 'button', onclick: () => { f.igdb_id = null; render(); } }, 'Unlink'))
        : null;

      formMount.replaceChildren(h('div.addform', {},
        field('Title', titleIn),
        listRow(f, 'Platforms', 'platforms', unique((g) => g.platforms)),
        listRow(f, 'Genres', 'genres', unique((g) => g.genres)),
        listRow(f, 'Themes', 'themes', unique((g) => g.themes)),
        field('Series', seriesIn),
        field('Developer', devIn),
        field('Released', yearIn),
        field('Ownership', ownershipSelect(f)),
        yearsRow(f),
        listRow(f, 'Played with', 'players', unique((g) => g.players)),
        toggleRow(f, 'vr', 'VR'),
        toggleRow(f, 'completed', '100% completed'),
        h('div.frow.col', {}, h('span.flabel', {}, 'Notes'), notesField(f)),
        linked,
        h('div.frow.col', {},
          h('button.btn', { type: 'button', onclick: fill }, 'Fill from IGDB…'),
          h('p.hint', {}, 'Looks the game up on IGDB and replaces its genres, themes, developer, series and release year. Platforms, notes and ownership are kept.'))));
    }

    async function fill() {
      const c = await pickCandidate({ query: f.title, excludeId: game.id });
      if (!c) return;
      f = applyCandidate(f, c);
      render();
      toast('Filled from IGDB. Review it, then save.');
    }

    async function save() {
      const problem = validateForm(f);
      if (problem) { toast(problem); return; }
      const patch = buildPatch(game, f);
      if (!Object.keys(patch).length) { stopEdit(); return; }
      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving…';
      try {
        await patchGame(game.id, patch);
        toast('Saved');
        openDrawer(game); // redraw with the new values
      } catch (e) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';
        toast(e.status === 409 ? 'Another game in your library is already linked to that IGDB match.' : 'Could not save the changes.');
      }
    }

    async function cancel() {
      if (isDirty()) {
        const ok = await confirmDialog({ title: 'Discard your changes?', message: 'You have edits that are not saved.', confirm: 'Discard' });
        if (!ok) return;
      }
      stopEdit();
    }
    function stopEdit() {
      if (open) open.isDirty = null;
      editView.hidden = true;
      readView.hidden = false;
      title.hidden = false;
      quick.hidden = false;
      drawer.classList.remove('editing');
    }

    const saveBtn = h('button.btn.primary', { type: 'button', onclick: save }, 'Save');
    editView.replaceChildren(formMount, h('div.editbar', {}, h('button.btn', { type: 'button', onclick: cancel }, 'Cancel'), saveBtn));
    render();
    readView.hidden = true;
    title.hidden = true;
    quick.hidden = true;
    editView.hidden = false;
    drawer.classList.add('editing');
    drawer.querySelector('.drawer-body').scrollTop = 0;
    editView.querySelector('input')?.focus({ preventScroll: true });
  }

  const onKey = (e) => {
    if (e.key === 'Escape' && !document.querySelector('.dialog, .sheet.open')) requestClose();
  };
  document.addEventListener('keydown', onKey);
  document.body.append(scrim, drawer);
  open = { drawer, scrim, onKey, isDirty: null };
}
