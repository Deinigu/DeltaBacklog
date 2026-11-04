// A small action sheet (a native modal <dialog>, the same look as the Deals sheet): a title, optional lines of detail,
// and one full-width button per action. Used behind the "⋯" buttons of Needs attention and My views.
// Escape closes it (native), Back closes it (layers.js). An action runs after the sheet has closed, so focus is back on
// the "⋯" button first and a dialog the action opens remembers that button as its opener.
import { h } from './util.js';
import { openLayer } from './layers.js';

/**
 * o.title: the heading. o.lines: small text under it. o.actions: [{label (text or node), run, danger}].
 * Returns the <dialog>.
 */
export function openActionSheet({ title, lines = [], actions }) {
  let chosen = null;
  const dlg = h('dialog.dl-sheet', { 'aria-label': `${title}: actions` });
  const close = () => dlg.close();
  const acts = actions.map((a) => h('button.dl-act' + (a.danger ? '.danger-text' : ''), { type: 'button', onclick: () => { chosen = a.run; close(); } }, a.label));
  dlg.append(
    h('div.dl-sheet-head', {}, h('div', {}, h('b.dl-sheet-title', {}, title), ...lines.filter(Boolean).map((l) => h('div.dl-sub', {}, l)))),
    h('div.dl-acts', {}, ...acts),
    h('button.dl-act.dl-close', { type: 'button', onclick: close }, 'Close'));
  dlg.addEventListener('click', (e) => { if (e.target === dlg) close(); }); // a tap on the backdrop
  document.body.append(dlg);
  const release = openLayer(dlg, { requestClose: close, trap: false });
  dlg.addEventListener('close', () => {
    dlg.remove();
    release();
    if (chosen) chosen();
  });
  if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
  return dlg;
}

/** The "⋯" button that opens an action sheet. `build()` returns the sheet's options when it is pressed. */
export const moreButton = (label, build) =>
  h('button.btn.act-more', { type: 'button', 'aria-label': label, 'aria-haspopup': 'dialog', onclick: () => openActionSheet(build()) }, h('span', { 'aria-hidden': 'true' }, '⋯'));
