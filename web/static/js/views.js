// Manage saved views: open, rename, pin, reorder, duplicate, delete (with undo).
import { h, toast, askNameDialog } from './util.js';
import { store, createView, updateView, deleteView, reorderViews } from './api.js';

export function renderViews(root) {
  const list = h('div');
  root.replaceChildren(h('div.manage', {},
    h('h1', {}, 'My views'),
    h('p.hint', {}, 'Saved filters with their own sort order. Pinned views appear in the navigation.'),
    h('a.btn.primary', { href: '#/library' }, 'Create one from the Library'),
    list));

  async function guard(fn, msg) {
    try { await fn(); } catch { toast(msg || 'Something went wrong'); }
    draw();
  }

  async function remove(v) {
    const copy = { name: v.name, icon: v.icon, filter: v.filter, sort: v.sort, group_by: v.group_by, layout: v.layout, pinned: v.pinned };
    await guard(() => deleteView(v.id), 'Could not delete the view');
    const t = h('div.toast', {}, `Deleted “${v.name}”  `, h('button.pill', { type: 'button', onclick: () => guard(async () => { await createView(copy); t.remove(); }, 'Could not restore') }, 'Undo'));
    document.getElementById('toasts').append(t);
    setTimeout(() => t.remove(), 6000);
  }

  function move(i, d) {
    const ids = store.views.map((v) => v.id);
    const [x] = ids.splice(i, 1);
    ids.splice(i + d, 0, x);
    return guard(() => reorderViews(ids), 'Could not reorder');
  }

  function draw() {
    if (!store.views.length) { list.replaceChildren(h('div.empty', {}, 'No saved views yet.')); return; }
    list.replaceChildren(...store.views.map((v, i) => h('div.vrow', {},
      h('span.emoji', {}, v.icon || '⭐'),
      h('a.name', { href: `#/view/${v.id}`, style: 'text-decoration:none;color:inherit' }, v.name, h('small', {}, summary(v))),
      h('div.tools', {},
        h('button.btn.pin', { type: 'button', 'aria-label': v.pinned ? 'Unpin from navigation' : 'Pin to navigation', 'aria-pressed': String(v.pinned), title: 'Pin to navigation', onclick: () => guard(() => updateView(v.id, { pinned: !v.pinned })) }, '📌'),
        h('button.btn', { type: 'button', 'aria-label': 'Move up', disabled: i === 0 || null, onclick: () => move(i, -1) }, '▲'),
        h('button.btn', { type: 'button', 'aria-label': 'Move down', disabled: i === store.views.length - 1 || null, onclick: () => move(i, 1) }, '▼'),
        h('button.btn', { type: 'button', 'aria-label': 'Rename', onclick: async () => {
          const r = await askNameDialog({ title: 'Rename view', name: v.name, icon: v.icon || '⭐' });
          if (r) guard(() => updateView(v.id, r));
        } }, '✎'),
        h('button.btn', { type: 'button', 'aria-label': 'Duplicate', onclick: () => guard(() => createView({ name: `${v.name} copy`, icon: v.icon, filter: v.filter, sort: v.sort, group_by: v.group_by, layout: v.layout, pinned: v.pinned }), 'Could not duplicate') }, '⧉'),
        h('button.btn', { type: 'button', 'aria-label': 'Delete', onclick: () => remove(v) }, '🗑')))));
  }

  /** One-line description of what a view filters on. */
  function summary(v) {
    const f = v.filter || {};
    const parts = [];
    for (const [k, vals] of Object.entries(f.in || {})) parts.push(`${k}: ${vals.map(nice(k)).join(f.all?.includes(k) ? ' + ' : ' / ')}`);
    for (const [k, vals] of Object.entries(f.ex || {})) parts.push(`not ${k}: ${vals.map(nice(k)).join(', ')}`);
    for (const fl of f.flags || []) parts.push(fl);
    if (f.q) parts.push(`“${f.q}”`);
    return parts.join(' · ') || 'All games';
  }
  const nice = (k) => (x) => (k === 'status' ? store.statusByCode.get(x)?.label ?? x : x);

  draw();
}
