// The rankings list (#/rankings): every ranking with a strip of its best games, and the way to make a new one.
import { h, toast, gradientFor, initials, promptDialog, confirmDialog, icon } from './util.js';
import { store, posterURL, coverURL, listRankings, createRanking, patchRanking, deleteRanking, duplicateRanking, reorderRankings, refreshPosters } from './api.js';
import { moveItem } from './order.js';

function previewTile(gid) {
  const g = store.byId.get(gid);
  if (!g) return null;
  const url = posterURL(g) || coverURL(g);
  return h('span.rkc-poster', { title: g.title }, url ? h('img', { src: url, alt: '', loading: 'lazy', decoding: 'async' }) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)));
}

/** Ask for the name and the starting tiers of a new ranking. Resolves {name, preset} or null. */
function newRankingDialog() {
  let preset = 'sabcdf';
  const choice = (value, label, hint) => {
    const b = h('button.rk-choice', { type: 'button', 'aria-pressed': String(value === preset), onclick: () => {
      preset = value;
      for (const x of group.children) x.setAttribute('aria-pressed', String(x === b));
    } }, h('b', {}, label), h('span', {}, hint));
    return b;
  };
  const group = h('div.rk-choices', { role: 'group', 'aria-label': 'Starting tiers' },
    choice('sabcdf', 'S to F', 'Six classic tiers'), choice('empty', 'Empty', 'Make your own tiers'));
  return promptDialog({ title: 'New ranking', placeholder: 'e.g. Best of 2025', confirm: 'Create', extra: group }).then((name) => (name ? { name, preset } : null));
}

export function renderRankings(root) {
  let alive = true;
  let list = [];
  const box = h('div.rk-list');
  root.replaceChildren(h('div.rk-page', {},
    h('header.rk-listhead', {}, h('h1', {}, 'Rankings'),
      h('button.btn.primary', { type: 'button', onclick: create }, icon('add'), 'New ranking')),
    h('p.hint', {}, 'Rank any set of games your way: your own tiers, your own labels, big posters. Export the result as a picture.'),
    box));

  async function load() {
    try { list = await listRankings(); } catch {
      if (alive) box.replaceChildren(h('div.empty', {}, 'Could not load the rankings. ', h('button.btn', { type: 'button', onclick: load }, 'Retry')));
      return;
    }
    if (alive) draw();
    refreshPosters().then((changed) => { if (changed && alive) draw(); }).catch(() => {});
  }

  function draw() {
    if (!list.length) {
      box.replaceChildren(h('div.empty', {}, 'No rankings yet.', h('br'), 'Make one, then add games from your library, or open “Rank this year” on a Year in review page.'));
      return;
    }
    box.replaceChildren(...list.map((r, i) => {
      const strip = r.preview.map(previewTile).filter(Boolean);
      const stripes = h('span.rkc-tiers', { 'aria-hidden': 'true' }, r.tiers.map((t) => h('i', { style: `background:${t.color}`, title: t.label })));
      const card = h('a.rkc-main', { href: `#/rankings/${r.id}` },
        h('span.rkc-strip', {}, strip.length ? strip : h('span.rkc-none', {}, r.games ? 'Nothing ranked yet' : 'No games yet')),
        h('span.rkc-info', {}, h('b', {}, r.name),
          h('span.rkc-sub', {}, `${r.games} game${r.games === 1 ? '' : 's'} · ${r.ranked} ranked · ${r.tiers.length} tier${r.tiers.length === 1 ? '' : 's'}`),
          r.description && h('span.rkc-desc', {}, r.description),
          stripes));
      const btn = (label, glyph, fn, off, cls = '') => h('button.btn' + cls, { type: 'button', 'aria-label': `${label}: ${r.name}`, title: label, disabled: off || null, onclick: fn }, glyph);
      // The arrows only exist when there is something to reorder; at the ends the missing one leaves its place empty.
      const arrows = list.length > 1 ? [btn('Move up', '▲', () => move(i, -1), i === 0, '.arrow'), btn('Move down', '▼', () => move(i, 1), i === list.length - 1, '.arrow')] : [];
      return h('article.rkc', {}, card,
        h('div.rkc-actions', {},
          ...arrows,
          btn('Rename', 'Rename', () => rename(r)), btn('Duplicate', 'Duplicate', () => duplicate(r)), btn('Delete', 'Delete', () => remove(r), false, '.danger-btn')));
    }));
  }

  async function create() {
    const v = await newRankingDialog();
    if (!v || !alive) return;
    try { const r = await createRanking({ name: v.name, preset: v.preset }); location.hash = `#/rankings/${r.id}`; } catch { toast('Could not create the ranking'); }
  }
  async function rename(r) {
    const name = await promptDialog({ title: 'Rename the ranking', value: r.name });
    if (!name || name === r.name || !alive) return;
    try { await patchRanking(r.id, { name }); r.name = name; draw(); } catch { toast('Could not rename'); }
  }
  async function duplicate(r) {
    try { await duplicateRanking(r.id); toast('Duplicated'); await load(); } catch { toast('Could not duplicate'); }
  }
  async function remove(r) {
    if (!await confirmDialog({ title: `Delete “${r.name}”?`, message: 'The games stay in your library; only this ranking is deleted.' })) return;
    try { await deleteRanking(r.id); list = list.filter((x) => x.id !== r.id); draw(); } catch { toast('Could not delete'); }
  }
  async function move(i, delta) {
    const next = moveItem(list, i, i + delta);
    if (next.every((x, k) => x === list[k])) return;
    const before = list;
    list = next; draw();
    try { await reorderRankings(next.map((x) => x.id)); } catch { list = before; draw(); toast('Could not change the order'); }
  }

  load();
  return () => { alive = false; };
}
