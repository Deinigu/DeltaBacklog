// "Add games" sheet for the rankings: search and filter the whole library (the same filters as the Library page), tick
// games one by one or all the matches at once, and add them. Resolves to the picked game ids, or null when cancelled.
import { h, norm, icon, debounce, gradientFor, initials, lazyImg } from './util.js';
import { store, coverURL, posterURL } from './api.js';
import { GROUPS, FLAGS, emptyState, matches, cycle, chipState, activeCount } from './filters.js';
import { ownershipLabel } from './ownership.js';
import { yearOrder } from './sort.js';

/**
 * opts: { title, existing: Set of game ids that are already in the ranking (shown, not selectable), confirm }.
 */
export function openGamePicker({ title = 'Add games', existing = new Set(), confirm = 'Add' } = {}) {
  return new Promise((resolve) => {
    const state = emptyState();
    const picked = new Set();
    const games = store.games.slice().sort((a, b) => a.title.localeCompare(b.title));

    // chips of each group, most used first (years: newest first)
    const order = {};
    for (const grp of GROUPS) {
      if (grp.key === 'status') { order.status = store.statuses.map((s) => s.key); continue; }
      const tally = new Map();
      for (const g of games) for (const v of grp.get(g)) tally.set(v, (tally.get(v) || 0) + 1);
      order[grp.key] = [...tally.keys()].sort((a, b) => (grp.key === 'year' ? yearOrder(a, b) : tally.get(b) - tally.get(a) || a.localeCompare(b)));
    }
    const label = (grp, v) => (grp.key === 'ownership' ? ownershipLabel(v) : null) ?? (grp.key === 'status' ? store.statusByCode.get(v)?.label : null)
      ?? (grp.key === 'year' && isNaN(v) ? v[0].toUpperCase() + v.slice(1) : v);

    const input = h('input', { type: 'search', placeholder: 'Search games…', autocomplete: 'off', enterkeyhint: 'search', 'aria-label': 'Search games' });
    const chipRefs = [];
    const groupEls = GROUPS.map((grp) => {
      const nSel = h('span.n');
      const chips = h('div.chips', {}, order[grp.key].map((v) => {
        const btn = h('button.chip', { type: 'button', onclick: () => { cycle(state, grp.key, v); update(); } }, grp.key === 'status' && h('span.dot', { style: `background:${store.statusByCode.get(v)?.color}` }), label(grp, v));
        chipRefs.push({ grp, v, btn });
        return btn;
      }));
      const mode = grp.multi && h('div.mode', { role: 'group', 'aria-label': `${grp.label}: match any or all` },
        h('button', { type: 'button', 'data-mode': 'any', onclick: () => { state.all.delete(grp.key); update(); } }, 'Any'),
        h('button', { type: 'button', 'data-mode': 'all', onclick: () => { state.all.add(grp.key); update(); } }, 'All'));
      return { grp, nSel, mode, el: h('details.group', { open: grp.key === 'year' || null, hidden: order[grp.key].length === 0 }, h('summary', {}, h('span', {}, grp.label), nSel), mode, chips) };
    });
    const flagBtns = FLAGS.map((f) => ({ f, btn: h('button.chip', { type: 'button', hidden: !!f.test && !games.some(f.test), onclick: () => { state.flags.has(f.key) ? state.flags.delete(f.key) : state.flags.add(f.key); update(); } }, f.label) }));

    const count = h('div.count.pk-count');
    const list = h('div.pk-list', { role: 'list' });
    const filtersBox = h('div.pk-filters', {}, h('div.chips', {}, flagBtns.map((r) => r.btn)), groupEls.map((g) => g.el));
    const filtersBtn = h('button.btn.pk-toggle', { type: 'button', 'aria-expanded': 'false', onclick: () => {
      const on = filtersBox.classList.toggle('open');
      filtersBtn.setAttribute('aria-expanded', String(on));
    } }, icon('filter'), h('span', {}, 'Filters'));
    const filtersBadge = h('span.badge', { hidden: true });
    filtersBtn.append(filtersBadge);
    const selectAll = h('button.btn', { type: 'button' });
    const clearAll = h('button.btn', { type: 'button', onclick: () => { picked.clear(); draw(); } }, 'Clear');
    const addBtn = h('button.btn.primary', { type: 'button', onclick: () => finish([...picked]) });

    let shown = [];
    const rowEls = new Map();
    function rowFor(g) {
      const url = posterURL(g) || coverURL(g);
      const inRanking = existing.has(g.id);
      const box = h('span.box', {});
      const row = h('button.pk-row', { type: 'button', role: 'listitem', 'aria-pressed': 'false', disabled: inRanking || null, 'data-id': g.id, onclick: () => {
        if (inRanking) return;
        picked.has(g.id) ? picked.delete(g.id) : picked.add(g.id);
        paintRow(g.id); paintFoot();
      } },
      box,
      h('span.pk-thumb', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title))),
      h('span.pk-info', {}, h('span.pk-name', {}, g.title), h('span.pk-sub', {}, [g.platforms.slice(0, 2).join(', '), g.years.length ? g.years.slice(-1)[0] : ''].filter(Boolean).join(' · '))),
      inRanking && h('span.pk-in', {}, 'In ranking'));
      rowEls.set(g.id, { row, box });
      return row;
    }
    function paintRow(id) {
      const r = rowEls.get(id);
      if (!r) return;
      const on = picked.has(id);
      r.row.setAttribute('aria-pressed', String(on));
      r.box.textContent = on ? '✓' : '';
    }
    function paintFoot() {
      const free = shown.filter((g) => !existing.has(g.id));
      const allOn = free.length > 0 && free.every((g) => picked.has(g.id));
      selectAll.textContent = allOn ? `Deselect these ${free.length}` : `Select these ${free.length}`;
      selectAll.disabled = free.length === 0;
      selectAll.onclick = () => { for (const g of free) (allOn ? picked.delete(g.id) : picked.add(g.id)); for (const g of free) paintRow(g.id); paintFoot(); };
      clearAll.disabled = picked.size === 0;
      addBtn.textContent = picked.size ? `${confirm} ${picked.size} game${picked.size === 1 ? '' : 's'}` : confirm;
      addBtn.disabled = picked.size === 0;
    }
    function draw() {
      rowEls.clear();
      list.replaceChildren(...(shown.length ? shown.map(rowFor) : [h('div.empty', {}, 'No games match.')]));
      for (const id of picked) paintRow(id);
      count.textContent = `${shown.length} of ${games.length} game${games.length === 1 ? '' : 's'}`;
      paintFoot();
    }
    function update() {
      for (const r of chipRefs) {
        const st = chipState(state, r.grp.key, r.v);
        r.btn.dataset.state = st;
        r.btn.setAttribute('aria-pressed', String(st === 'in'));
      }
      for (const g of groupEls) {
        const n = state.in[g.grp.key].size + state.ex[g.grp.key].size;
        g.nSel.textContent = n ? String(n) : '';
        if (g.mode) {
          const all = state.all.has(g.grp.key);
          g.mode.querySelector('[data-mode="any"]').setAttribute('aria-pressed', String(!all));
          g.mode.querySelector('[data-mode="all"]').setAttribute('aria-pressed', String(all));
          g.mode.hidden = state.in[g.grp.key].size < 2;
        }
      }
      for (const r of flagBtns) r.btn.setAttribute('aria-pressed', String(state.flags.has(r.f.key)));
      const n = activeCount(state);
      filtersBadge.hidden = n === 0;
      filtersBadge.textContent = String(n);
      shown = games.filter((g) => matches(state, g));
      draw();
    }
    input.addEventListener('input', debounce(() => { state.q = norm(input.value.trim()); update(); }, 90));

    const finish = (value) => { scrim.remove(); sheet.remove(); document.removeEventListener('keydown', onKey); resolve(value); };
    const scrim = h('div.scrim.show', { onclick: () => finish(null) });
    scrim.style.zIndex = 95;
    const sheet = h('div.sheet.open.picker', { role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div.filters-head', {}, h('span', {}, title), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: () => finish(null) }, '✕')),
      h('div.pk-top', {}, h('div.search', {}, icon('search'), input), filtersBtn),
      filtersBox,
      h('div.pk-bar', {}, count, selectAll, clearAll),
      h('div.filters-body.pk-body', {}, list),
      h('div.filters-foot', {}, h('button.btn', { type: 'button', onclick: () => finish(null) }, 'Cancel'), addBtn));
    sheet.style.zIndex = 96;
    const onKey = (e) => { if (e.key === 'Escape') finish(null); };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, sheet);
    update();
    input.focus({ preventScroll: true });
  });
}
