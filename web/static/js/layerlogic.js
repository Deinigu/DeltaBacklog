// The decisions behind layers.js, pure so they can be tested without a browser (web/test/layers.test.mjs).

/** What a popstate means. `target` is the layer index of the entry now shown, `depth` how many layers are open. */
export function popAction(target, depth) {
  if (target < depth) return 'close'; // Back: the top layer's entry was left
  if (target > depth) return 'skip'; // a stale layer entry (a closed layer, or one left behind by a page change): step over it
  return 'none';
}

/** How far to go back in history so that it holds exactly `depth` layer entries. `cur` is the current entry's layer index. */
export const rewindBy = (cur, depth) => (cur > depth ? depth - cur : 0);

/** Opening layer number `depth` (1-based) from the entry `cur`: push a new entry, or reuse one a just-closed layer left. */
export const pushAction = (cur, depth) => (cur >= depth ? 'replace' : 'push');

/**
 * The URL to put on the history entry a closing layer lands on. A page may rewrite its own query while a layer is open
 * (the Library's filters and sort); that rewrite lives on the layer's entry, and the entry below still has the old
 * query. `saved` is the URL the page had; null when `now` already is it, or when the page is not the same one.
 */
export function carryUrl(saved, now) {
  if (!saved || saved === now) return null;
  return saved.split('?')[0] === now.split('?')[0] ? saved : null;
}

/** The index Tab should go to inside a trap of `count` focusable elements: -1 means "let the browser decide". */
export function trapTarget(index, count, shift) {
  if (count === 0) return -2; // nothing focusable: keep focus on the layer itself
  if (index < 0) return shift ? count - 1 : 0; // focus is outside (or on the layer): come in at the matching end
  if (shift && index === 0) return count - 1;
  if (!shift && index === count - 1) return 0;
  return -1;
}
