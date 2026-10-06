// Library view: instant search, faceted filters (URL-backed), sortable cover grid.
import { h, norm, debounce, icon, gradientFor, initials } from './util.js';
import { store, coverURL } from './api.js';
import { openDrawer } from './drawer.js';

const GROUPS = [
  { key: 'status', label: 'Status', get: (g) => [g.status] },
  { key: 'platform', label: 'Platform', get: (g) => g.platforms },
  { key: 'genre', label: 'Genre', get: (g) => g.genres },
  { key: 'theme', label: 'Theme', get: (g) => g.themes },
  { key: 'year', label: 'Year played', get: (g) => [...g.years.map(String), ...g.periods] },
  { key: 'ownership', label: 'Ownership', get: (g) => (g.ownership ? [g.ownership] : []) },
  { key: 'player', label: 'Played with', get: (g) => g.players },
];
const FLAGS = [
  { key: 'favorite', label: '★ Favorite' },
  { key: 'vr', label: 'VR' },
  { key: 'completed', label: '100% completed' },
];
const SORTS = {
  default: { label: 'Default order', cmp: null },
  title: { label: 'Title A–Z', cmp: (a, b) => a._sort.localeCompare(b._sort) },
  recent: { label: 'Recently played', cmp: (a, b) => lastYear(b) - lastYear(a) || a._sort.localeCompare(b._sort) },
};
const lastYear = (g) => (g.years.length ? g.years[g.years.length - 1] : 0);

function readState() {
  const qs = new URLSearchParams(location.hash.split('?')[1] || '');
  const sel = Object.fromEntries(GROUPS.map((g) => [g.key, new Set(qs.getAll(g.key))]));
  const flags = new Set(FLAGS.map((f) => f.key).filter((k) => qs.get(k) === '1'));
  const sort = SORTS[qs.get('sort')] ? qs.get('sort') : 'default';
  return { q: qs.get('q') || '', sel, flags, sort };
}

function writeState(s) {
  const qs = new URLSearchParams();
  if (s.q) qs.set('q', s.q);
  for (const g of GROUPS) for (const v of s.sel[g.key]) qs.append(g.key, v);
  for (const f of s.flags) qs.set(f, '1');
  if (s.sort !== 'default') qs.set('sort', s.sort);
  const str = qs.toString();
  history.replaceState(null, '', '#/library' + (str ? '?' + str : ''));
}

export function renderLibrary(root) {
  const state = readState();
  for (const g of store.games) g._sort ??= norm(g.title);

  const matches = (g, skip) => {
    if (state.q && !state.q.split(/\s+/).every((t) => g._search.includes(t))) return false;
    for (const f of state.flags) if (!g[f]) return false;
    for (const grp of GROUPS) {
      const sel = state.sel[grp.key];
      if (grp.key === skip || !sel.size) continue;
      if (!grp.get(g).some((v) => sel.has(v))) return false;
    }
    return true;
  };

  // Chip order is fixed from the whole library (by popularity) so chips don't jump around while filtering.
  const order = {};
  for (const grp of GROUPS) {
    if (grp.key === 'status') { order.status = store.statuses.map((s) => s.key); continue; }
    const tally = new Map();
    for (const g of store.games) for (const v of grp.get(g)) tally.set(v, (tally.get(v) || 0) + 1);
    order[grp.key] = [...tally.keys()].sort((a, b) => {
      if (grp.key === 'year') return b.localeCompare(a); // newest first, "childhood" last
      return tally.get(b) - tally.get(a) || a.localeCompare(b);
    });
  }
  const label = (grp, v) => (grp.key === 'status' ? store.statusByCode.get(v)?.label : null) ?? (grp.key === 'year' && isNaN(v) ? v[0].toUpperCase() + v.slice(1) : v);

  // ---------- toolbar ----------
  const input = h('input', { type: 'search', placeholder: 'Search games…', autocomplete: 'off', enterkeyhint: 'search', 'aria-label': 'Search', value: state.q });
  const clearBtn = h('button.clear', { type: 'button', 'aria-label': 'Clear search', onclick: () => { input.value = ''; state.q = ''; input.focus(); update(); } }, '✕');
  const filterBadge = h('span.badge', { hidden: true });
  const filterBtn = h('button.btn.open-filters', { type: 'button', 'aria-label': 'Filters', onclick: () => setSheet(true) }, icon('filter'), filterBadge);
  const sortSel = h('select.btn', { 'aria-label': 'Sort', onchange: () => { state.sort = sortSel.value; update(); } },
    Object.entries(SORTS).map(([k, v]) => h('option', { value: k, selected: k === state.sort }, v.label)));
  const meta = h('div.meta-row');
  const toolbar = h('div.toolbar', {},
    h('div.toolbar-row', {}, h('div.search', {}, icon('search'), input, clearBtn), filterBtn, sortSel), meta);

  input.addEventListener('input', debounce(() => { state.q = norm(input.value.trim()); update(); }, 90));
  if (state.q) input.value = state.q;
  state.q = norm(state.q);

  // ---------- filter panel ----------
  const chipRefs = []; // {grp, v, btn, count}
  const groupRefs = {};
  const groupEls = GROUPS.map((grp) => {
    const nSel = h('span.n');
    const chips = h('div.chips', {}, order[grp.key].map((v) => {
      const count = h('span.c');
      const dot = grp.key === 'status' ? h('span.dot', { style: `background:${store.statusByCode.get(v)?.color}` }) : null;
      const btn = h('button.chip', { type: 'button', 'aria-pressed': 'false', onclick: () => toggle(grp.key, v) }, dot, label(grp, v), count);
      chipRefs.push({ grp, v, btn, count });
      return btn;
    }));
    groupRefs[grp.key] = nSel;
    return h('details.group', { open: grp.key === 'status' || state.sel[grp.key].size > 0 || null }, h('summary', {}, h('span', {}, grp.label), nSel), chips);
  });
  const flagRefs = FLAGS.map((f) => {
    const btn = h('button.chip', { type: 'button', 'aria-pressed': 'false', onclick: () => { state.flags.has(f.key) ? state.flags.delete(f.key) : state.flags.add(f.key); update(); } }, f.label);
    return { f, btn };
  });
  const showBtn = h('button.btn.primary', { type: 'button', onclick: () => setSheet(false) });
  const resetBtn = h('button.btn', { type: 'button', onclick: reset }, 'Reset');
  const panel = h('aside.filters', { 'aria-label': 'Filters' },
    h('div.filters-head', {}, h('span', {}, 'Filters'), h('button.btn.close-sheet', { type: 'button', onclick: () => setSheet(false), 'aria-label': 'Close filters' }, '✕')),
    h('div.filters-body', {}, h('div.chips', {}, flagRefs.map((r) => r.btn)), groupEls),
    h('div.filters-foot', {}, resetBtn, showBtn));
  const scrim = h('div.scrim', { onclick: () => setSheet(false) });
  function setSheet(on) { panel.classList.toggle('open', on); scrim.classList.toggle('show', on); }

  // ---------- results ----------
  const grid = h('div.grid');
  const results = h('section', { 'aria-label': 'Games' }, grid);

  root.replaceChildren(toolbar, h('div.library', {}, panel, scrim, results));

  const cards = new Map();
  function card(g) {
    let el = cards.get(g.id);
    if (el) return el;
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    const sub = [g.platforms.join(', '), g.years.length ? g.years[g.years.length - 1] : ''].filter(Boolean).join(' · ');
    el = h('button.card', { type: 'button', onclick: () => openDrawer(g) },
      h('div.cover', {},
        url ? h('img', { src: url, alt: '', loading: 'lazy', decoding: 'async' }) : h('div.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)),
        g.favorite && h('span.fav', { 'aria-label': 'Favorite' }, '★'),
        st && h('span.st', { style: `background:${st.color}` }, st.label)),
      h('div.card-body', {}, h('div.card-title', {}, g.title), sub && h('div.card-sub', {}, sub)));
    cards.set(g.id, el);
    return el;
  }

  function toggle(key, v) {
    const s = state.sel[key];
    s.has(v) ? s.delete(v) : s.add(v);
    update();
  }
  function reset() {
    state.q = ''; input.value = '';
    for (const k in state.sel) state.sel[k].clear();
    state.flags.clear();
    update();
  }

  function update() {
    writeState(state);
    const list = store.games.filter((g) => matches(g));
    const cmp = SORTS[state.sort].cmp;
    if (cmp) list.sort(cmp);

    grid.replaceChildren(...list.map(card));
    if (!list.length) grid.replaceChildren(h('div.empty', { style: 'grid-column:1/-1' }, 'No games match these filters.'));

    // facet counts: each group is counted against all *other* active filters
    const counts = {};
    for (const grp of GROUPS) {
      const m = new Map();
      for (const g of store.games) if (matches(g, grp.key)) for (const v of new Set(grp.get(g))) m.set(v, (m.get(v) || 0) + 1);
      counts[grp.key] = m;
    }
    for (const r of chipRefs) {
      const n = counts[r.grp.key].get(r.v) || 0;
      r.count.textContent = n;
      r.btn.classList.toggle('zero', n === 0 && !state.sel[r.grp.key].has(r.v));
      r.btn.setAttribute('aria-pressed', state.sel[r.grp.key].has(r.v));
    }
    for (const { f, btn } of flagRefs) btn.setAttribute('aria-pressed', state.flags.has(f.key));
    for (const grp of GROUPS) groupRefs[grp.key].textContent = state.sel[grp.key].size || '';

    // active filter pills
    const active = [];
    for (const grp of GROUPS) for (const v of state.sel[grp.key]) active.push(h('button.pill', { type: 'button', onclick: () => toggle(grp.key, v) }, h('b', {}, grp.label), label(grp, v), ' ✕'));
    for (const f of state.flags) active.push(h('button.pill', { type: 'button', onclick: () => { state.flags.delete(f); update(); } }, FLAGS.find((x) => x.key === f).label, ' ✕'));
    const nFilters = active.length;
    meta.replaceChildren(
      h('span.count', {}, `${list.length} of ${store.games.length} games`), ...active,
      (nFilters || state.q) && h('button.pill.reset', { type: 'button', onclick: reset }, 'Reset'));
    filterBadge.hidden = !nFilters;
    filterBadge.textContent = nFilters;
    showBtn.textContent = `Show ${list.length} games`;
  }
  update();

  const onKey = (e) => {
    if (e.key === '/' && document.activeElement !== input && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName)) { e.preventDefault(); input.focus(); }
    if (e.key === 'Escape') setSheet(false);
  };
  document.addEventListener('keydown', onKey);
  return () => document.removeEventListener('keydown', onKey);
}
