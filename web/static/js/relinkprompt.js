// "Linked to <IGDB title>. Update details from it?": offered after a game's IGDB link changes (the drawer's save, the
// Link with IGDB screen). Fill empty fields is the default; Replace all shows what would change first. One Undo toast.
import { h, toast, toastAction, toastError } from './util.js';
import { igdbDetails, patchGame } from './api.js';
import { changeText, appliedText, errorText, hasUndo, undoText } from './relink.js';

let floating = null;

/** Remove the floating prompt (a new link replaces it). */
export function closeRelinkPrompt() {
  floating?.remove();
  floating = null;
}

/**
 * The prompt for `game`, linked to the IGDB game called `name`. With `mount` (an element) it is placed there; without,
 * it floats above the page until it is answered or dismissed. Returns its element.
 */
export function relinkPrompt(game, name, mount = null) {
  closeRelinkPrompt();
  let busy = false;
  const body = h('div.relink-body');
  const box = h('div.relink-prompt', { role: 'group', 'aria-label': 'Update details from IGDB' }, body);
  const dismiss = () => { if (box === floating) floating = null; box.remove(); };

  function ask() {
    body.replaceChildren(
      h('p.relink-q', {}, 'Linked to ', h('b', {}, `“${name}”`), '. Update details from it?'),
      h('p.hint', {}, 'Genres, themes, developer, series, release date, game modes and times. Status, notes, ownership, years played and your covers stay as they are.'),
      h('div.relink-actions', {},
        h('button.btn.primary', { type: 'button', onclick: () => run('empty') }, 'Fill empty fields'),
        h('button.btn', { type: 'button', onclick: preview }, 'Replace all…'),
        h('button.btn', { type: 'button', onclick: dismiss }, 'Not now')));
  }

  async function guard(fn) {
    if (busy) return;
    busy = true;
    box.setAttribute('aria-busy', 'true');
    try { await fn(); } catch (e) { toast(errorText(e)); ask(); } finally { busy = false; box.removeAttribute('aria-busy'); }
  }

  const run = (mode) => guard(async () => {
    const data = await igdbDetails(game.id, mode, true);
    dismiss();
    const text = appliedText(data.changes, mode);
    if (data.changes.length && hasUndo(data.undo)) {
      toastAction(text, 'Undo', async () => {
        try { await patchGame(game.id, data.undo); toast(undoText(data.undo)); } catch { toastError('undo the update'); }
      }, 8000);
    } else toast(text);
  });

  const preview = () => guard(async () => {
    body.replaceChildren(h('p.hint', {}, 'Reading IGDB…'));
    const { changes } = await igdbDetails(game.id, 'replace', false);
    if (!changes.length) {
      body.replaceChildren(h('p.relink-q', {}, 'IGDB has nothing different to put in.'),
        h('div.relink-actions', {}, h('button.btn', { type: 'button', onclick: dismiss }, 'Close')));
      return;
    }
    body.replaceChildren(
      h('p.relink-q', {}, 'Replace these with what IGDB has?'),
      h('ul.relink-changes', {}, changes.map((c) => h('li', {}, changeText(c)))),
      h('div.relink-actions', {},
        h('button.btn.primary', { type: 'button', onclick: () => run('replace') }, 'Replace'),
        h('button.btn', { type: 'button', onclick: ask }, 'Back')));
  });

  ask();
  if (mount) mount.replaceChildren(box);
  else {
    box.classList.add('floating');
    document.body.append(box);
    floating = box;
  }
  return box;
}
