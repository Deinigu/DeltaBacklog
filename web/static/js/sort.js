// Multi-level sorting and group-by, shared by every view.
import { h, icon } from './util.js';
import { store } from './api.js';
import { ownershipLabel } from './ownership.js';
import { ttbMinutes, playedMinutes } from './ttb.js';

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const lastYear = (g) => (g.years.length ? g.years[g.years.length - 1] : null);
const firstYear = (g) => (g.years.length ? g.years[0] : null);

// value(g) returns a number, a string or null. null always sorts last, whatever the direction.
export const SORT_FIELDS = {
  title:      { label: 'Title', value: (g) => g.title, defDir: 'asc' },
  status:     { label: 'Status', value: (g) => store.statusOrder.get(g.status) ?? 99, defDir: 'asc' },
  platform:   { label: 'Platform', value: (g) => g.platforms[0] ?? null, defDir: 'asc' },
  series:     { label: 'Series', value: (g) => g.series, defDir: 'asc' },
  release:    { label: 'Release year', value: (g) => g.release_year, defDir: 'desc' },
  last_year:  { label: 'Last year played', value: lastYear, defDir: 'desc' },
  first_year: { label: 'First year played', value: firstYear, defDir: 'asc' },
  years:      { label: 'Years played (count)', value: (g) => g.years.length, defDir: 'desc' },
  added:      { label: 'Date added', value: (g) => g.added_at, defDir: 'desc' },
  edited:     { label: 'Date edited', value: (g) => g.updated_at, defDir: 'desc' },
  ttb:        { label: 'Time to beat', value: ttbMinutes, defDir: 'asc' },
  playtime:   { label: 'Hours played (Steam)', value: playedMinutes, defDir: 'desc' },
  favorite:   { label: 'Favorites first', value: (g) => (g.favorite ? 1 : 0), defDir: 'desc' },
  completed:  { label: '100% completed first', value: (g) => (g.completed ? 1 : 0), defDir: 'desc' },
  manual:     { label: 'Manual order', value: (g) => g.queue_pos, defDir: 'asc' },
};

// What "no sort chosen" means: the library's natural order.
export const DEFAULT_SORT = [
  { field: 'status', dir: 'asc' },
  { field: 'title', dir: 'asc' },
];

function cmpValues(a, b) {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return collator.compare(String(a), String(b));
}

/** Builds one comparator from an ordered list of {field, dir}. Title is always the final tiebreak. */
export function buildComparator(levels) {
  const steps = (levels.length ? levels : DEFAULT_SORT).filter((l) => SORT_FIELDS[l.field]);
  return (a, b) => {
    for (const { field, dir } of steps) {
      const f = SORT_FIELDS[field];
      for (const get of [f.value, f.then]) {
        if (!get) continue;
        const va = get(a), vb = get(b);
        if (va == null && vb == null) continue;
        if (va == null) return 1; // nulls last in both directions
        if (vb == null) return -1;
        const c = cmpValues(va, vb);
        if (c) return dir === 'desc' ? -c : c;
      }
    }
    return collator.compare(a.title, b.title);
  };
}

/** Calendar years newest first, then named periods (childhood, always). */
export function yearOrder(a, b) {
  const na = !isNaN(a), nb = !isNaN(b);
  if (na && nb) return Number(b) - Number(a);
  if (na !== nb) return na ? -1 : 1;
  return a.localeCompare(b);
}

export const GROUP_FIELDS = {
  status:   { label: 'Status', keys: (g) => [g.status], label_of: (k) => store.statusByCode.get(k)?.label ?? k, order: (a, b) => (store.statusOrder.get(a) ?? 99) - (store.statusOrder.get(b) ?? 99) },
  platform: { label: 'Platform', keys: (g) => g.platforms },
  series:   { label: 'Series', keys: (g) => (g.series ? [g.series] : []) },
  genre:    { label: 'Genre', keys: (g) => g.genres },
  year:     { label: 'Year played', keys: (g) => [...g.years.map(String), ...g.periods], order: yearOrder, label_of: (k) => (isNaN(k) ? k[0].toUpperCase() + k.slice(1) : k) },
  ownership: { label: 'Ownership', keys: (g) => (g.ownership ? [g.ownership] : []), label_of: ownershipLabel },
};

/** Splits an already-sorted list into [{key, label, games}] sections. Games without a value go last under "None". */
export function groupGames(list, field) {
  const gf = GROUP_FIELDS[field];
  if (!gf) return [{ key: '', label: '', games: list }];
  const map = new Map();
  const none = [];
  for (const g of list) {
    const keys = gf.keys(g);
    if (!keys.length) { none.push(g); continue; }
    for (const k of new Set(keys)) (map.get(k) ?? map.set(k, []).get(k)).push(g);
  }
  const order = gf.order || ((a, b) => collator.compare(a, b));
  const groups = [...map.keys()].sort(order).map((k) => ({ key: k, label: gf.label_of ? gf.label_of(k) : k, games: map.get(k) }));
  if (none.length) groups.push({ key: '', label: 'None', games: none });
  return groups;
}

// ---- URL (de)serialisation: sort=field:dir,field:dir  group=field ----
export function parseSort(str) {
  return (str || '').split(',').map((p) => p.split(':')).filter(([f]) => SORT_FIELDS[f]).map(([field, dir]) => ({ field, dir: dir === 'desc' ? 'desc' : 'asc' }));
}
export const formatSort = (levels) => levels.map((l) => `${l.field}:${l.dir}`).join(',');

export function sortSummary(levels) {
  if (!levels.length) return 'Default order';
  return levels.map((l) => SORT_FIELDS[l.field].label + (l.dir === 'desc' ? ' ↓' : '')).join(', ');
}

/**
 * The sort sheet: ordered sort levels (field, direction, move, remove) plus group-by.
 * `state` has {sorts, group}; onChange is called after every edit.
 */
export function createSortSheet(state, onChange) {
  const list = h('div.sort-levels');
  const groupSel = h('select.btn', { 'aria-label': 'Group by', onchange: () => { state.group = groupSel.value; onChange(); } },
    h('option', { value: '' }, 'No grouping'),
    Object.entries(GROUP_FIELDS).map(([k, v]) => h('option', { value: k }, v.label)));

  const scrim = h('div.scrim', { onclick: () => toggle(false) });
  const sheet = h('div.sheet', { role: 'dialog', 'aria-label': 'Sort and group' },
    h('div.filters-head', {}, h('span', {}, 'Sort & group'), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: () => toggle(false) }, '✕')),
    h('div.filters-body', {},
      h('p.hint', {}, 'Games are sorted by the first level; ties fall through to the next.'),
      list,
      h('button.btn.add-level', { type: 'button', onclick: addLevel }, icon('add'), 'Add level'),
      h('div.group-by', {}, h('label', {}, 'Group by'), groupSel)),
    h('div.filters-foot', {},
      h('button.btn', { type: 'button', onclick: () => { state.sorts = []; state.group = ''; render(); onChange(); } }, 'Reset'),
      h('button.btn.primary', { type: 'button', onclick: () => toggle(false) }, 'Done')));

  function toggle(on) { sheet.classList.toggle('open', on); scrim.classList.toggle('show', on); }

  function addLevel() {
    if (!state.sorts.length) state.sorts = DEFAULT_SORT.map((l) => ({ ...l }));
    const used = new Set(state.sorts.map((l) => l.field));
    const field = Object.keys(SORT_FIELDS).find((k) => !used.has(k)) || 'title';
    state.sorts.push({ field, dir: SORT_FIELDS[field].defDir });
    render(); onChange();
  }

  function render() {
    groupSel.value = state.group || '';
    const levels = state.sorts.length ? state.sorts : DEFAULT_SORT;
    const implicit = !state.sorts.length;
    list.replaceChildren(...levels.map((lv, i) => {
      const fieldSel = h('select.btn', { 'aria-label': `Level ${i + 1} field`, onchange: () => edit(i, (l) => { l.field = fieldSel.value; l.dir = SORT_FIELDS[l.field].defDir; }) },
        Object.entries(SORT_FIELDS).map(([k, v]) => h('option', { value: k, selected: k === lv.field, disabled: (k !== lv.field && levels.some((o) => o.field === k)) || null }, v.label)));
      return h('div.sort-level', {},
        h('span.n', {}, i + 1),
        fieldSel,
        h('button.btn.dir', { type: 'button', 'aria-label': lv.dir === 'asc' ? 'Ascending' : 'Descending', onclick: () => edit(i, (l) => { l.dir = l.dir === 'asc' ? 'desc' : 'asc'; }) }, lv.dir === 'asc' ? '↑' : '↓'),
        h('button.btn', { type: 'button', 'aria-label': 'Move up', disabled: i === 0 || null, onclick: () => move(i, -1) }, '▲'),
        h('button.btn', { type: 'button', 'aria-label': 'Move down', disabled: i === levels.length - 1 || null, onclick: () => move(i, 1) }, '▼'),
        h('button.btn', { type: 'button', 'aria-label': 'Remove level', onclick: () => remove(i) }, '✕'));
    }));
    list.dataset.implicit = implicit ? '1' : '';
  }
  // Editing the implicit default materialises it, so the user's change is explicit and saved.
  const own = () => { if (!state.sorts.length) state.sorts = DEFAULT_SORT.map((l) => ({ ...l })); };
  function edit(i, fn) { own(); fn(state.sorts[i]); render(); onChange(); }
  function move(i, d) { own(); const [x] = state.sorts.splice(i, 1); state.sorts.splice(i + d, 0, x); render(); onChange(); }
  function remove(i) { own(); state.sorts.splice(i, 1); render(); onChange(); }

  render();
  return { el: [scrim, sheet], open: () => { render(); toggle(true); }, render };
}
