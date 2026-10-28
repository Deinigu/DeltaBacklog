// Excel-like table: sticky header and title column, resizable and hideable columns, sortable headers,
// inline editing, windowed rendering (only the visible rows exist in the DOM), CSV export.
import { h, toast } from './util.js';
import { store, patchGame } from './api.js';
import { pickMany } from './picker.js';
import { toCSV } from './csv.js';
import { ownershipIcon } from './ownership.js';
import { formatTime, ttbMinutes } from './ttb.js';
import { yearOptions, normalizeYearToken, libraryYears } from './editpatch.js';

const ROW_H = 48; // keep in step with .tr and .thead in styles.css
const OVERSCAN = 6;

const uniq = (get) => () => {
  const tally = new Map();
  for (const g of store.games) for (const v of get(g)) tally.set(v, (tally.get(v) || 0) + 1);
  return [...tally.keys()].sort((a, b) => tally.get(b) - tally.get(a) || a.localeCompare(b));
};
const yearsOf = (g) => [...g.years.map(String), ...g.periods.map((p) => p[0].toUpperCase() + p.slice(1))];

// kind: text | number | lookup | select | bool | multi | years | readonly
// sort: the sort field (see sort.js) a header click applies. patch(value) builds the API patch.
export const COLUMNS = [
  { key: 'title', label: 'Title', w: innerWidth < 600 ? 170 : 260, kind: 'text', sort: 'title', text: (g) => g.title, patch: (v) => ({ title: v }) },
  { key: 'status', label: 'Status', w: 150, kind: 'select', sort: 'status', text: (g) => store.statusByCode.get(g.status)?.label ?? g.status, options: () => store.statuses.map((s) => ({ value: s.key, label: s.label })), value: (g) => g.status, patch: (v) => ({ status: v }) },
  { key: 'platforms', label: 'Platforms', w: 170, kind: 'multi', sort: 'platform', text: (g) => g.platforms.join('; '), values: (g) => g.platforms, options: uniq((g) => g.platforms), patch: (v) => ({ platforms: v }) },
  { key: 'genres', label: 'Genres', w: 220, kind: 'multi', text: (g) => g.genres.join('; '), values: (g) => g.genres, options: uniq((g) => g.genres), patch: (v) => ({ genres: v }) },
  { key: 'themes', label: 'Themes', w: 200, kind: 'multi', text: (g) => g.themes.join('; '), values: (g) => g.themes, options: uniq((g) => g.themes), patch: (v) => ({ themes: v }) },
  { key: 'years', label: 'Years played', w: 150, kind: 'years', sort: 'last_year', text: (g) => yearsOf(g).join('; '), values: yearsOf, options: () => yearOptions(undefined, libraryYears(store.games)),
    patch: (v) => ({ years: v.filter((x) => /^\d{4}$/.test(x)).map(Number), periods: v.filter((x) => /^(childhood|always)$/i.test(x)).map((x) => x.toLowerCase()) }) },
  { key: 'series', label: 'Series', w: 170, kind: 'lookup', sort: 'series', text: (g) => g.series ?? '', options: uniq((g) => (g.series ? [g.series] : [])), patch: (v) => ({ series: v }) },
  { key: 'developer', label: 'Developer', w: 160, kind: 'lookup', text: (g) => g.developer ?? '', options: uniq((g) => (g.developer ? [g.developer] : [])), patch: (v) => ({ developer: v }) },
  { key: 'release_year', label: 'Released', w: 100, kind: 'number', sort: 'release', text: (g) => g.release_year ?? '', patch: (v) => ({ release_year: v }) },
  { key: 'ttb', label: 'Time to beat', w: 120, kind: 'readonly', sort: 'ttb', text: (g) => formatTime(ttbMinutes(g)) },
  { key: 'ownership', label: 'Ownership', w: 130, kind: 'lookup', text: (g) => g.ownership ?? '', options: uniq((g) => (g.ownership ? [g.ownership] : [])), patch: (v) => ({ ownership: v }) },
  { key: 'players', label: 'Played with', w: 170, kind: 'multi', text: (g) => g.players.join('; '), values: (g) => g.players, options: uniq((g) => g.players), patch: (v) => ({ players: v }) },
  { key: 'favorite', label: 'Favorite', w: 90, kind: 'bool', sort: 'favorite', text: (g) => (g.favorite ? 'yes' : ''), patch: (v) => ({ favorite: v }) },
  { key: 'vr', label: 'VR', w: 64, kind: 'bool', text: (g) => (g.vr ? 'yes' : ''), patch: (v) => ({ vr: v }) },
  { key: 'completed', label: '100%', w: 74, kind: 'bool', sort: 'completed', text: (g) => (g.completed ? 'yes' : ''), patch: (v) => ({ completed: v }) },
  { key: 'notes', label: 'Notes', w: 280, kind: 'longtext', text: (g) => g.notes, patch: (v) => ({ notes: v }) },
  { key: 'added_at', label: 'Added', w: 110, kind: 'readonly', sort: 'added', text: (g) => (g.added_at || '').slice(0, 10) },
];
const DEFAULT_HIDDEN = new Set(['themes', 'developer', 'ownership', 'players', 'vr', 'completed', 'notes', 'added_at', 'release_year']);

// ---- per-device preferences (widths, visible columns) ----
const load = (key, fallback) => { try { return JSON.parse(localStorage.getItem(key)) ?? fallback; } catch { return fallback; } };
const save = (key, v) => { try { localStorage.setItem(key, JSON.stringify(v)); } catch { /* private mode */ } };

/**
 * ctx: { list, sorts, setSort(field, dir), onOpen(game), scroll: {top, left} (kept by the caller across re-renders) }
 * Returns the element to mount.
 */
export function renderTable(ctx) {
  const widths = load('tbl-widths', {});
  const hidden = new Set(load('tbl-hidden', [...DEFAULT_HIDDEN]));
  const colW = (c) => Math.max(48, widths[c.key] ?? c.w);
  const visible = () => COLUMNS.filter((c) => !hidden.has(c.key) || c.key === 'title');

  const wrap = h('div.tablewrap', { tabindex: '0', role: 'grid', 'aria-label': 'Games table', 'aria-rowcount': ctx.list.length + 1 });
  const inner = h('div.tinner');
  const head = h('div.thead', { role: 'row' });
  const body = h('div.tbody');
  inner.append(head, body);
  wrap.append(inner);

  const rendered = new Map(); // game id -> row element
  let first = -1, last = -1;

  function totalWidth() { return visible().reduce((s, c) => s + colW(c), 0); }

  // ---------- header ----------
  function drawHeader() {
    const cur = ctx.sorts[0];
    head.replaceChildren(...visible().map((c, i) => {
      const active = c.sort && cur?.field === c.sort;
      const cell = h('div.th', { role: 'columnheader', style: `width:${colW(c)}px`, class: i === 0 ? 'sticky' : '', 'aria-sort': active ? (cur.dir === 'asc' ? 'ascending' : 'descending') : 'none' },
        c.sort
          ? h('button.hbtn', { type: 'button', onclick: () => ctx.setSort(c.sort, active ? (cur.dir === 'asc' ? 'desc' : 'asc') : 'asc') }, c.label, active ? h('span.arrow', {}, cur.dir === 'asc' ? ' ▲' : ' ▼') : null)
          : h('span.hlabel', {}, c.label),
        h('span.rz', { 'aria-hidden': 'true', onpointerdown: (e) => resize(e, c, cell) }));
      return cell;
    }));
    inner.style.width = totalWidth() + 'px';
    head.style.width = totalWidth() + 'px';
  }

  function resize(e, c, cell) {
    e.preventDefault();
    e.stopPropagation();
    const handle = e.currentTarget;
    handle.setPointerCapture(e.pointerId);
    const startX = e.clientX, startW = colW(c);
    const move = (ev) => {
      widths[c.key] = Math.max(48, Math.round(startW + ev.clientX - startX));
      cell.style.width = widths[c.key] + 'px';
      inner.style.width = head.style.width = totalWidth() + 'px';
      for (const row of rendered.values()) { // update the matching cell in each rendered row
        const idx = visible().indexOf(c);
        row.children[idx].style.width = widths[c.key] + 'px';
        row.style.width = totalWidth() + 'px';
      }
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', up);
      save('tbl-widths', widths);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', up);
  }

  // ---------- cells ----------
  async function commit(g, c, value) {
    try { await patchGame(g.id, c.patch(value)); } catch { toast('Could not save the change'); }
  }

  function cell(g, c, i) {
    const el = h('div.td', { role: 'gridcell', style: `width:${colW(c)}px`, class: i === 0 ? 'sticky' : '' });
    show(el, g, c, i);
    return el;
  }

  function show(el, g, c, i) {
    el.classList.remove('editing');
    el.classList.toggle('editable', c.kind !== 'readonly');
    if (c.kind === 'bool') {
      const box = h('input', { type: 'checkbox', checked: g[c.key] || null, 'aria-label': `${c.label}: ${g.title}`, onchange: () => commit(g, c, box.checked) });
      el.replaceChildren(box);
      el.onclick = (e) => { if (e.target !== box) { box.checked = !box.checked; commit(g, c, box.checked); } };
      return;
    }
    const text = c.text(g);
    if (c.key === 'title') {
      el.replaceChildren(h('button.open', { type: 'button', 'aria-label': `Open ${g.title}`, onclick: (e) => { e.stopPropagation(); ctx.onOpen(g); } }, '↗'), h('span.txt', { title: text }, text));
    } else if (c.key === 'ownership' && text) {
      el.replaceChildren(h('span.oicon', { 'aria-hidden': 'true' }, ownershipIcon(text)), h('span.txt', { title: text }, text));
    } else if (c.key === 'status') {
      const st = store.statusByCode.get(g.status);
      el.replaceChildren(h('span.dot', { style: `background:${st?.color}` }), h('span.txt', {}, text));
    } else {
      el.replaceChildren(h('span.txt', { title: text }, text));
    }
    if (c.kind === 'readonly') return;
    el.onclick = (e) => { if (!el.classList.contains('editing') && !e.target.closest('.open')) edit(el, g, c, i); };
  }

  function edit(el, g, c, i) {
    if (c.kind === 'multi' || c.kind === 'years') {
      pickMany({
        title: `${c.label} · ${g.title}`,
        options: c.options(),
        selected: c.values(g),
        validate: c.kind === 'years' ? normalizeYearToken : (t) => t,
        ...(c.kind === 'years' ? { hint: 'Older year? Type it (4 digits) in the box above.', invalidHint: 'Years go from 1970 to 2100.' } : {}),
      }).then((next) => next && commit(g, c, next));
      return;
    }
    el.classList.add('editing');
    let input;
    if (c.kind === 'select') {
      input = h('select', { 'aria-label': c.label }, c.options().map((o) => h('option', { value: o.value, selected: o.value === c.value(g) }, o.label)));
    } else if (c.kind === 'longtext') {
      input = h('textarea', { rows: '1', 'aria-label': c.label }, c.text(g));
    } else {
      const listId = c.kind === 'lookup' ? `dl-${c.key}` : null;
      input = h('input', { type: c.kind === 'number' ? 'number' : 'text', value: c.kind === 'number' ? (c.text(g) || '') : c.text(g), list: listId, 'aria-label': c.label, inputmode: c.kind === 'number' ? 'numeric' : null });
      if (listId) {
        let dl = document.getElementById(listId);
        if (!dl) { dl = h('datalist', { id: listId }); document.body.append(dl); }
        dl.replaceChildren(...c.options().map((o) => h('option', { value: o })));
      }
    }
    el.replaceChildren(input);
    input.focus();
    if (input.select) input.select();
    const initial = input.value; // opening and leaving a cell must never rewrite it

    let finished = false;
    const finish = (save, move) => {
      if (finished) return;
      finished = true;
      const raw = input.value;
      // Where Enter/Tab go next is decided now: the commit below can re-sort the list under us.
      const target = move === 'down' ? ctx.list[ctx.list.indexOf(g) + 1] : g;
      if (save && raw !== initial) {
        if (c.key === 'title' && !raw.trim()) toast('A title can\u2019t be empty');
        else if (c.kind === 'number') commit(g, c, raw === '' ? 0 : Number(raw));
        else if (c.kind === 'select' || c.kind === 'longtext') commit(g, c, raw);
        else commit(g, c, raw.trim());
      }
      show(el, g, c, i);
      if (move && target) moveEdit(target, i, move);
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && c.kind === 'longtext' && e.shiftKey) return; // newline inside notes
      if (e.key === 'Enter') { e.preventDefault(); finish(true, 'down'); }
      else if (e.key === 'Escape') { e.preventDefault(); finish(false); }
      else if (e.key === 'Tab') { e.preventDefault(); finish(true, e.shiftKey ? 'left' : 'right'); }
    });
    input.addEventListener('blur', () => finish(true));
    if (c.kind === 'select') input.addEventListener('change', () => finish(true));
  }

  // Excel-style navigation after Tab / Enter. `target` is the game whose row to continue in.
  function moveEdit(target, i, dir) {
    const cols = visible();
    let ci = i;
    if (dir !== 'down') {
      ci += dir === 'right' ? 1 : -1;
      while (ci >= 0 && ci < cols.length && (cols[ci].kind === 'readonly' || cols[ci].kind === 'bool')) ci += dir === 'left' ? -1 : 1;
    }
    if (ci < 0 || ci >= cols.length) return;
    // let the save's re-render settle, then open the target cell wherever its row ended up
    setTimeout(() => {
      const r = ctx.list.indexOf(target);
      if (r < 0) return;
      scrollToRow(r);
      const el = rendered.get(target.id)?.children[ci];
      if (el && el.onclick) el.click();
    }, 60);
  }
  function scrollToRow(r) {
    const top = r * ROW_H, view = wrap.clientHeight - ROW_H;
    if (top < wrap.scrollTop) wrap.scrollTop = top;
    else if (top + ROW_H > wrap.scrollTop + view) wrap.scrollTop = top + ROW_H - view;
    paint();
  }

  // ---------- windowed rows ----------
  // What a row shows. If it is unchanged after a refresh the element is kept, so a click that began on it
  // (for example the one that blurred a cell being edited) still completes.
  const signature = (g) => visible().map((c) => (c.kind === 'bool' ? g[c.key] : c.text(g))).join('\u0001') + '|' + g.status;

  function row(g, idx) {
    const el = h('div.tr', { role: 'row', 'aria-rowindex': idx + 2, style: `top:${idx * ROW_H}px;width:${totalWidth()}px` });
    el._sig = signature(g);
    visible().forEach((c, i) => el.append(cell(g, c, i)));
    return el;
  }

  function paint(force) {
    const top = wrap.scrollTop;
    const from = Math.max(0, Math.floor(top / ROW_H) - OVERSCAN);
    const to = Math.min(ctx.list.length - 1, Math.ceil((top + wrap.clientHeight) / ROW_H) + OVERSCAN);
    if (!force && from === first && to === last) return;
    first = from; last = to;
    const want = new Set();
    for (let i = from; i <= to; i++) {
      const g = ctx.list[i];
      want.add(g.id);
      if (!rendered.has(g.id)) {
        const el = row(g, i);
        rendered.set(g.id, el);
        body.append(el);
      }
    }
    for (const [id, el] of rendered) if (!want.has(id) && !el.querySelector('.editing')) { el.remove(); rendered.delete(id); }
  }

  body.style.height = ctx.list.length * ROW_H + 'px';
  drawHeader();
  wrap.addEventListener('scroll', () => {
    ctx.scroll.top = wrap.scrollTop; ctx.scroll.left = wrap.scrollLeft;
    requestAnimationFrame(() => paint());
  }, { passive: true });

  // The wrapper needs a real height before the first paint; the caller mounts us, then calls mounted().
  wrap.mounted = () => {
    wrap.scrollTop = ctx.scroll.top;
    wrap.scrollLeft = ctx.scroll.left;
    paint(true);
  };

  // New data (a game changed, filters or sort changed). Rows are rebuilt, except the one being edited,
  // so Tab/Enter navigation and typing are never interrupted by the refresh a save triggers.
  wrap.setList = (list, sorts) => {
    ctx.list = list;
    ctx.sorts = sorts;
    const at = new Map(list.map((g, i) => [g.id, i]));
    for (const [id, el] of rendered) {
      const g = store.byId.get(id);
      if (at.has(id) && (el.querySelector('.editing') || el._sig === signature(g))) {
        el.style.top = at.get(id) * ROW_H + 'px';
        el.setAttribute('aria-rowindex', at.get(id) + 2);
        continue;
      }
      el.remove();
      rendered.delete(id);
    }
    body.style.height = list.length * ROW_H + 'px';
    wrap.setAttribute('aria-rowcount', list.length + 1);
    drawHeader();
    first = last = -1;
    paint(true);
  };

  wrap.columnsSheet = () => openColumns(() => { drawHeader(); first = last = -1; rendered.forEach((e) => e.remove()); rendered.clear(); paint(true); });

  function openColumns(changed) {
    const scrim = h('div.scrim.show', { onclick: close });
    scrim.style.zIndex = 95;
    const chips = h('div.chips', {}, COLUMNS.map((c) => {
      const btn = h('button.chip', { type: 'button', 'aria-pressed': String(!hidden.has(c.key) || c.key === 'title'), disabled: c.key === 'title' || null, onclick: () => {
        hidden.has(c.key) ? hidden.delete(c.key) : hidden.add(c.key);
        btn.setAttribute('aria-pressed', String(!hidden.has(c.key)));
        btn.dataset.state = hidden.has(c.key) ? 'off' : 'in';
        save('tbl-hidden', [...hidden]);
        changed();
      } }, c.label);
      btn.dataset.state = hidden.has(c.key) && c.key !== 'title' ? 'off' : 'in';
      return btn;
    }));
    const sheet = h('div.sheet.open', { role: 'dialog', 'aria-label': 'Columns' },
      h('div.filters-head', {}, h('span', {}, 'Columns'), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕')),
      h('div.filters-body', {}, h('p.hint', {}, 'Choose which columns the table shows. Drag the edge of a column header to resize it.'), chips),
      h('div.filters-foot', {}, h('button.btn', { type: 'button', onclick: () => { hidden.clear(); DEFAULT_HIDDEN.forEach((k) => hidden.add(k)); Object.keys(widths).forEach((k) => delete widths[k]); save('tbl-hidden', [...hidden]); save('tbl-widths', {}); close(); changed(); } }, 'Defaults'), h('button.btn.primary', { type: 'button', onclick: close }, 'Done')));
    sheet.style.zIndex = 96;
    function close() { scrim.remove(); sheet.remove(); }
    document.body.append(scrim, sheet);
  }

  wrap.exportCSV = () => {
    const csv = toCSV(ctx.list, visible());
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
    const a = h('a', { href: url, download: `deltabacklog-${new Date().toISOString().slice(0, 10)}.csv` });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  };
  return wrap;
}
