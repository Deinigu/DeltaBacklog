// The "Similar games" row of the game drawer: for a finished or favourite game, the suggestions the server has cached
// (never IGDB). It stays out of the way: nothing is drawn until the cache answers, or when it has nothing for the game.
import { h, gradientFor, initials, lazyImg } from './util.js';
import { store, similarList, missingPosterURL } from './api.js';
import { isSource, similarFor } from './similar.js';
import { closeDrawer } from './drawer.js';

export function similarSection(game) {
  const box = h('div.sim-drawer', { hidden: true });
  if (!isSource(game)) return box;
  similarList().then((resp) => {
    const list = similarFor(resp, store.games, game);
    if (!list.length || !box.isConnected) return;
    box.append(
      h('h3', {}, 'Similar games'),
      h('ul.sim-strip', {}, list.map((s) => {
        const url = missingPosterURL(s.cover, s.v);
        return h('li', {}, h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(s.title)}` }, initials(s.title))), s.title);
      })),
      h('a', { href: '#/similar', onclick: () => closeDrawer() }, 'See all similar games'));
    box.hidden = false;
  }).catch(() => { /* offline or an old server: no row */ });
  return box;
}
