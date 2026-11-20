// Library view (and saved views): instant search, tri-state faceted filters, multi-level sort, cover grid.
// With opts.view it shows a saved view and tracks unsaved changes; without, it is the scratch Library (URL-backed).
import { createTtbFilter } from './ttbfilter.js';
import { createSearchBox, createFilterButton, createFilterShell, filterGroup, pill } from './filtershell.js';
import { ttbRangeActive, ttbRangeText, ttbKindLabel } from './ttb.js';
import { h, emptyBox, norm, debounce, icon, gradientFor, initials, toast, askNameDialog, lazyImg, plural, toastError } from './util.js';
import { store, coverURL, createView, updateView, reorderGames } from './api.js';
import { openDrawer } from './drawer.js';
import { createReorderList } from './reorder.js';
import { renderTable } from './table.js';
import { playedBadges, playedSummary } from './years.js';
import { badgeStyle } from './colors.js';
import { openSurprise } from './surprise.js';
import { ownershipIcon, ownershipLabel, isUsualOwnership } from './ownership.js';
import { replaceUrl } from './layers.js';
import { hashWithGame, gameIdFromHash } from './gameurl.js';
import { buildComparator, sortSummary, groupGames, yearOrder, createSortSheet, GROUP_FIELDS } from './sort.js';
import {
  GROUPS, FLAGS, matches, cycle, chipState, activeCount, isPristine, emptyState, facetSignature,
  filterToJSON, stateFromView, stateFromQuery, stateToQuery, signature,
} from './filters.js';

/** A button's content: a symbol that always shows, and a text label that narrow screens hide (see .lbl in the CSS). */
function setLabel(btn, symbol, label) {
  btn.replaceChildren(h('span', { 'aria-hidden': 'true' }, symbol), h('span.lbl', {}, ' ' + label));
  btn.setAttribute('aria-label', label);
}

export function renderLibrary(root, opts = {}) {
  const view = opts.view || null;
  const state = view ? stateFromView(view) : stateFromQuery(location.hash.split('?')[1]);
  let baseline = view ? signature(state) : null;
  const dirty = () => view && signature(state) !== baseline;

  // Chip order is fixed from the whole library (by popularity) so chips don't jump around while filtering.
  const order = {};
  for (const grp of GROUPS) {
    if (grp.key === 'status') { order.status = store.statuses.map((s) => s.key); continue; }
    const tally = new Map();
    for (const g of store.games) for (const v of grp.get(g)) tally.set(v, (tally.get(v) || 0) + 1);
    // Values used by a saved filter but absent from the library stay selectable, so a view never silently loses a condition.
    for (const v of [...state.in[grp.key], ...state.ex[grp.key]]) if (!tally.has(v)) tally.set(v, 0);
    order[grp.key] = [...tally.keys()].sort((a, b) => {
      if (grp.key === 'year') return yearOrder(a, b);
      return tally.get(b) - tally.get(a) || a.localeCompare(b);
    });
  }
  const label = (grp, v) => (grp.key === 'ownership' ? ownershipLabel(v) : null) ?? (grp.key === 'status' ? store.statusByCode.get(v)?.label : null) ?? (grp.key === 'year' && isNaN(v) ? v[0].toUpperCase() + v.slice(1) : v);

  // ---------- header + toolbar ----------
  const searchBox = createSearchBox({ value: state.q, onClear: () => { state.q = ''; update(); } });
  const input = searchBox.input;
  const filter = createFilterButton(() => setSheet(true));
  const filterBtn = filter.btn;
  const sortSheet = createSortSheet(state, () => update());
  const sortBtn = h('button.btn.sort-btn', { type: 'button', 'aria-label': 'Sort and group', onclick: () => sortSheet.open() }, icon('sort'), h('span.sort-label'));
  const meta = h('div.meta-row');
  const count = h('span.count', { role: 'status' }); // one live region for the whole page: "12 of 600 games" is announced as the filters change

  let reordering = false;
  let tableInst = null, tableBox = null;
  const tableScroll = { top: 0, left: 0 };
  const layoutBtn = h('button.btn', { type: 'button', 'aria-label': 'Switch between cards and table', onclick: () => { state.layout = state.layout === 'table' ? 'cards' : 'table'; reordering = false; update(); } });
  const surpriseBtn = h('button.btn.surprise-btn', { type: 'button', onclick: () => openSurprise(state) });
  setLabel(surpriseBtn, '🎲', 'Surprise me');
  const reorderBtn = h('button.btn', { type: 'button', 'aria-pressed': 'false', onclick: () => { reordering = !reordering; update(); } }, '⇅ Reorder');
  const saveBtn = h('button.btn.primary', { type: 'button', onclick: saveChanges }, 'Save');
  const saveAsBtn = h('button.btn', { type: 'button', onclick: saveAs }, view ? 'Save as…' : 'Save as view');
  const title = view
    ? h('h1', {}, h('span', {}, view.icon ? view.icon + ' ' : ''), view.name)
    : h('h1', {}, 'Library');
  const manageBtn = view && h('a.btn', { href: '#/views', 'aria-label': 'Manage views' }, icon('views'), h('span.lbl', {}, 'Views'));
  const head = h('div.view-head', {}, title, surpriseBtn, layoutBtn, reorderBtn, manageBtn);
  // Below 600px Save / Save as… sit in the meta row (next to Revert), so the head stays one row while typing in a view
  const narrow = window.matchMedia('(max-width: 599px)');
  const onNarrow = () => update();
  narrow.addEventListener('change', onNarrow);

  const toolbar = h('div.toolbar', {}, head,
    h('div.toolbar-row', {}, searchBox.el, filterBtn, sortBtn), meta);

  input.addEventListener('input', debounce(() => { state.q = norm(input.value.trim()); update(); }, 90));

  // ---------- filter panel ----------
  const chipRefs = []; // {grp, v, btn, count}
  const groupRefs = {};
  const modeRefs = {};
  const groupEls = GROUPS.map((grp) => {
    const chips = h('div.chips', {}, order[grp.key].map((v) => {
      const count = h('span.c');
      const dot = grp.key === 'status' ? h('span.dot', { style: `background:${store.statusByCode.get(v)?.color}` }) : null;
      const btn = h('button.chip', { type: 'button', onclick: () => { cycle(state, grp.key, v); update(); } }, dot, label(grp, v), count);
      chipRefs.push({ grp, v, btn, count });
      return btn;
    }));
    let mode = null;
    if (grp.multi) {
      const any = h('button', { type: 'button', onclick: () => { state.all.delete(grp.key); update(); } }, 'Any');
      const all = h('button', { type: 'button', onclick: () => { state.all.add(grp.key); update(); } }, 'All');
      mode = h('div.mode', { role: 'group', 'aria-label': `${grp.label}: match any or all` }, any, all);
      modeRefs[grp.key] = { el: mode, any, all };
    }
    const open = grp.key === 'status' || state.in[grp.key].size > 0 || state.ex[grp.key].size > 0;
    // Groups that only IGDB fills (game mode, perspective) stay hidden until some game has a value.
    const group = filterGroup(grp.label, { open, hidden: order[grp.key].length === 0, lead: mode, chips });
    groupRefs[grp.key] = group.n;
    return group.el;
  });
  const flagRefs = FLAGS.map((f) => {
    const btn = h('button.chip', { type: 'button', hidden: !!f.test && !state.flags.has(f.key) && !store.games.some(f.test), onclick: () => { state.flags.has(f.key) ? state.flags.delete(f.key) : state.flags.add(f.key); update(); } }, f.label);
    return { f, btn };
  });
  const ttbFilter = createTtbFilter(state, () => update());
  const showBtn = h('button.btn.primary', { type: 'button', onclick: () => setSheet(false) });
  const resetBtn = h('button.btn', { type: 'button', onclick: reset }, view ? 'Revert' : 'Reset');
  // The panel is a bottom sheet below 900px and a sidebar from there (see filtershell.js).
  const shell = createFilterShell({ body: [
    h('p.chip-hint', {}, 'Tap a chip to include it, tap again to exclude it.'),
    h('div.chips', {}, flagRefs.map((r) => r.btn)), groupEls, ttbFilter.el], foot: [resetBtn, showBtn] });
  const { panel, scrim, setSheet } = shell;

  // ---------- results ----------
  const results = h('section', { 'aria-label': 'Games' });
  root.replaceChildren(toolbar, h('div.library', {}, panel, scrim, results), ...sortSheet.el);
  input.value = state.q;

  // The filter panel sticks below the toolbar, whose height varies (view title row, wrapped pills).
  // --head-h: on phones the title row (plus the 6px above the search row) scrolls away and only the search row stays
  // pinned (see .toolbar in the CSS). Once pinned the toolbar is "stuck" and its title row is hidden, so no strip of it
  // shows above the search row (with a safe-area inset that strip would be tall).
  const syncStuck = () => toolbar.classList.toggle('stuck', narrow.matches && toolbar.getBoundingClientRect().top <= 1 - parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--head-h') || '0'));
  const syncToolbar = () => {
    document.documentElement.style.setProperty('--toolbar-h', toolbar.offsetHeight + 'px');
    document.documentElement.style.setProperty('--head-h', head.offsetHeight + 6 + 'px');
    syncStuck();
  };
  window.addEventListener('scroll', syncStuck, { passive: true });
  const ro = new ResizeObserver(syncToolbar);
  ro.observe(toolbar);
  syncToolbar();

  const cards = new Map();
  function card(g) {
    let el = cards.get(g.id);
    if (el) return el;
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    const played = playedBadges(g, innerWidth < 600 ? 2 : 3); // two years fit on one line on a phone
    const hasPlayed = played.years.length || played.periods.length;
    const platforms = g.platforms.slice(0, 2);
    el = h('button.card', { type: 'button', onclick: () => openDrawer(g), style: st ? `--st:${st.color}` : null },
      h('div.cover', {},
        url ? lazyImg(url) : h('div.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)),
        g.favorite && h('span.fav', { 'aria-label': 'Favorite' }, '★'),
        g.ownership && !isUsualOwnership(g.ownership) && h('span.own', { title: g.ownership, role: 'img', 'aria-label': g.ownership }, ownershipIcon(g.ownership)),
        st && h('span.st', { style: badgeStyle(st.color) }, st.label)),
      h('div.card-body', {},
        h('div.card-title', {}, g.title),
        platforms.length ? h('div.tags-row', {}, ...platforms.map((p) => h('span.pf', {}, p)), g.platforms.length > 2 ? h('span.pf.more', { title: g.platforms.slice(2).join(', ') }, `+${g.platforms.length - 2}`) : null) : null,
        hasPlayed ? h('div.years-row', { title: playedSummary(g), role: 'img', 'aria-label': playedSummary(g) },
          ...played.periods.map((p) => h('span.yr.period', { title: p.label }, p.icon)),
          played.earlier ? h('span.yr.earlier', { title: `${played.earlier} earlier year${played.earlier > 1 ? 's' : ''}` }, `+${played.earlier}`) : null,
          ...played.years.map((y) => h('span.yr', {}, String(y.year)))) : null));
    cards.set(g.id, el);
    return el;
  }

  function reset() {
    const fresh = view ? stateFromView(view) : emptyState();
    state.q = fresh.q; state.in = fresh.in; state.ex = fresh.ex; state.all = fresh.all; state.flags = fresh.flags; state.ttb = fresh.ttb;
    ttbFilter.sync();
    if (view) { state.sorts = fresh.sorts; state.group = fresh.group; sortSheet.render(); }
    input.value = state.q;
    update();
  }
  const payload = () => ({ filter: filterToJSON(state), sort: state.sorts, group_by: state.group, layout: state.layout });

  async function saveChanges() {
    try {
      await updateView(view.id, payload());
      Object.assign(view, store.views.find((v) => v.id === view.id));
      baseline = signature(state);
      toast('View saved');
      update();
    } catch { toastError('save the view'); }
  }
  async function saveAs() {
    const r = await askNameDialog({ title: 'Save as a new view', name: view ? `${view.name} copy` : '', icon: view?.icon || '⭐' });
    if (!r) return;
    try {
      const created = await createView({ ...r, ...payload() });
      toast(`Saved “${created.name}”`);
      location.hash = `#/view/${created.id}`;
    } catch { toastError('save the view'); }
  }
  title.addEventListener('click', async () => {
    if (!view) return;
    const r = await askNameDialog({ title: 'Rename view', name: view.name, icon: view.icon || '⭐' });
    if (!r) return;
    try {
      Object.assign(view, await updateView(view.id, r));
      title.replaceChildren(h('span', {}, view.icon ? view.icon + ' ' : ''), view.name);
    } catch { toastError('rename the view'); }
  });

  function update() {
    if (!view) replaceUrl(hashWithGame('#/library' + (stateToQuery(state) ? '?' + stateToQuery(state) : ''), gameIdFromHash(location.hash))); // an open game stays in the address
    const list = store.games.filter((g) => matches(state, g));
    list.sort(buildComparator(state.sorts));
    sortBtn.querySelector('.sort-label').textContent = sortSummary(state.sorts) + (state.group ? ` · by ${GROUP_FIELDS[state.group].label}` : '');

    reorderBtn.setAttribute('aria-pressed', String(reordering));
    setLabel(reorderBtn, reordering ? '✓' : '⇅', reordering ? 'Done' : 'Reorder');
    setLabel(layoutBtn, state.layout === 'table' ? '▦' : '☰', state.layout === 'table' ? 'Cards' : 'Table');
    reorderBtn.hidden = state.layout === 'table';
    if (state.layout === 'table') {
      if (!tableInst) {
        tableInst = renderTable({
          list, sorts: state.sorts, scroll: tableScroll, onOpen: openDrawer,
          setSort: (field, dir) => { state.sorts = [{ field, dir }]; sortSheet.render(); update(); },
        });
        tableBox = h('div.tablebox', {},
          h('div.table-tools', {},
            h('button.btn', { type: 'button', onclick: () => tableInst.columnsSheet() }, 'Columns'),
            h('button.btn', { type: 'button', onclick: () => tableInst.exportCSV() }, 'Export CSV'),
            h('span.hint', {}, 'Tap a cell to edit it. Tab moves right, Enter moves down.')),
          tableInst);
      }
      if (results.firstChild !== tableBox) { results.replaceChildren(tableBox); tableInst.mounted(); }
      tableInst.setList(list, state.sorts);
    } else if (!list.length) results.replaceChildren(emptyBox('No games match these filters.', (activeCount(state) || state.q) && h('button.btn', { type: 'button', onclick: reset }, view ? 'Revert' : 'Clear filters')));
    else if (reordering) {
      // Reordering always works on the manual order of exactly the games shown, whatever the sort is.
      const byPos = [...list].sort((a, b) => (a.queue_pos ?? 1e9) - (b.queue_pos ?? 1e9) || a.id - b.id);
      results.replaceChildren(
        h('p.hint', {}, `Drag ⠿ (or use ▲▼) to reorder ${list.length === 1 ? 'this' : 'these'} ${plural(list.length, 'game')}. The order is shared by every view that sorts by Manual order.`),
        createReorderList(byPos, async (ids) => { try { await reorderGames(ids); } catch { toastError('save the order'); } }, openDrawer));
    } else results.replaceChildren(...groupGames(list, state.group).map((grp) => h('div.group-block', {},
      state.group && h('h3.group-title', {}, grp.label, h('span.count', {}, grp.games.length)),
      h('div.grid', {}, grp.games.map(card)))));

    // facet counts: each group is counted against all *other* active filters
    const counts = {};
    for (const grp of GROUPS) {
      const m = new Map();
      for (const g of store.games) if (matches(state, g, grp.key)) for (const v of new Set(grp.get(g))) m.set(v, (m.get(v) || 0) + 1);
      counts[grp.key] = m;
    }
    for (const r of chipRefs) {
      const n = counts[r.grp.key].get(r.v) || 0;
      const st = chipState(state, r.grp.key, r.v);
      r.count.textContent = n;
      r.btn.dataset.state = st;
      r.btn.setAttribute('aria-pressed', st === 'in' ? 'true' : st === 'ex' ? 'mixed' : 'false');
      r.btn.classList.toggle('zero', n === 0 && st === 'off');
    }
    for (const { f, btn } of flagRefs) {
      const on = state.flags.has(f.key);
      btn.dataset.state = on ? 'in' : 'off';
      btn.setAttribute('aria-pressed', on);
    }
    for (const grp of GROUPS) {
      const n = state.in[grp.key].size + state.ex[grp.key].size;
      groupRefs[grp.key].textContent = n || '';
      const m = modeRefs[grp.key];
      if (m) {
        m.el.hidden = state.in[grp.key].size < 2;
        const all = state.all.has(grp.key);
        m.any.setAttribute('aria-pressed', !all);
        m.all.setAttribute('aria-pressed', all);
      }
    }

    ttbFilter.sync();

    // active filter pills
    const active = [];
    for (const grp of GROUPS) {
      for (const v of state.in[grp.key]) active.push(pill('', `${grp.label}: ${label(grp, v)}`, () => { state.in[grp.key].delete(v); if (state.in[grp.key].size < 2) state.all.delete(grp.key); update(); }, h('b', {}, grp.label), label(grp, v)));
      for (const v of state.ex[grp.key]) active.push(pill('.ex', `${grp.label}: not ${label(grp, v)}`, () => { state.ex[grp.key].delete(v); update(); }, h('b', {}, grp.label), `not ${label(grp, v)}`));
    }
    for (const f of state.flags) active.push(pill('', FLAGS.find((x) => x.key === f).label, () => { state.flags.delete(f); update(); }, FLAGS.find((x) => x.key === f).label));
    if (ttbRangeActive(state.ttb)) active.push(pill('', `${ttbKindLabel(state.ttb.kind)}: ${ttbRangeText(state.ttb)}`, () => { state.ttb.min = state.ttb.max = null; update(); }, h('b', {}, ttbKindLabel(state.ttb.kind)), ttbRangeText(state.ttb)));
    if (state.ttb.unknown) active.push(pill(state.ttb.unknown === 'ex' ? '.ex' : '', `${ttbKindLabel(state.ttb.kind)}: ${state.ttb.unknown === 'ex' ? 'not unknown' : 'unknown'}`, () => { state.ttb.unknown = ''; update(); }, h('b', {}, ttbKindLabel(state.ttb.kind)), state.ttb.unknown === 'ex' ? 'not unknown' : 'unknown'));
    const nFilters = activeCount(state);
    saveBtn.hidden = !(view && dirty());
    saveAsBtn.hidden = view ? false : isPristine(state);
    const revert = (nFilters || state.q) && h('button.pill.reset', { type: 'button', onclick: reset }, view ? 'Revert' : 'Reset');
    if (narrow.matches) {
      meta.replaceChildren(count, revert, saveBtn, saveAsBtn, ...active);
    } else {
      meta.replaceChildren(count, ...active, revert);
      head.insertBefore(saveBtn, manageBtn || null);
      head.insertBefore(saveAsBtn, manageBtn || null);
    }
    filter.setCount(nFilters);
    count.textContent = `${list.length} of ${plural(store.games.length, 'game')}`;
    showBtn.textContent = `Show ${plural(list.length, 'game')}`;

  }
  update();

  const onKey = (e) => {
    if (e.key === '/' && document.activeElement !== input && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName)) { e.preventDefault(); input.focus(); }
    if (e.key === 'Escape') setSheet(false);
  };
  // A game changed elsewhere (drawer, reorder): rebuild the cards and refresh counts.
  // While dragging we must not rebuild the list under the user's finger.
  let facets = facetSignature(store.games);
  const onGames = (e) => {
    if (reordering) return;
    const d = e.detail || {};
    if (!d.reorder) {
      // A new genre, platform or year has no filter chip yet: rebuild the whole page once so it appears.
      const now = facetSignature(store.games);
      if (now !== facets) { facets = now; window.dispatchEvent(new Event('games-reloaded')); return; }
    }
    if (d.id) cards.delete(d.id); else if (!d.reorder) cards.clear(); // only the card that changed is rebuilt
    update();
  };
  window.addEventListener('games-changed', onGames);
  document.addEventListener('keydown', onKey);
  return () => {
    window.removeEventListener('games-changed', onGames); document.removeEventListener('keydown', onKey); narrow.removeEventListener('change', onNarrow); shell.destroy(); window.removeEventListener('scroll', syncStuck); ro.disconnect(); };
}
