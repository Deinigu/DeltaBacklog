// Series progress page: every series with a progress bar and its games in release order.
import { h, gradientFor, initials, lazyImg } from './util.js';
import { store, coverURL } from './api.js';
import { openDrawer } from './drawer.js';
import { badgeStyle } from './colors.js';
import { buildSeries, withLeft, barShares, progressText, isFinished, isPlayed } from './series.js';

export function renderSeries(root) {
  let onlyLeft = false;
  const open = new Set(); // series keys that are expanded; survives redraws
  const list = h('div.series-list');
  const count = h('p.hint');
  const leftBtn = h('button.chip', { type: 'button', 'aria-pressed': 'false', onclick: () => {
    onlyLeft = !onlyLeft;
    leftBtn.setAttribute('aria-pressed', String(onlyLeft));
    draw();
  } }, 'Games left to play');
  root.replaceChildren(h('div.manage.series-page', {},
    h('h1', {}, 'Series'),
    h('p.hint', {}, 'How far you are through each series. Games are listed by release year.'),
    h('div.chips', {}, leftBtn),
    count, list));

  function gameRow(g) {
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    const mark = isFinished(g) ? '✓' : isPlayed(g) ? '▶' : '';
    return h('button.srow', { type: 'button', onclick: () => openDrawer(g) },
      h('span.sthumb', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title))),
      h('span.sname', {}, g.title, h('small', {}, g.release_year ?? 'Year unknown')),
      mark && h('span.smark', { 'aria-hidden': 'true' }, mark),
      st && h('span.st-badge', { style: badgeStyle(st.color) }, st.label));
  }

  function seriesCard(s) {
    const sh = barShares(s);
    const det = h('details.scard', { open: open.has(s.key) || null, ontoggle: () => (det.open ? open.add(s.key) : open.delete(s.key)) },
      h('summary', {},
        h('span.shead', {}, h('span.stitle', {}, s.name), h('small', {}, progressText(s) + (s.left ? ` · ${s.left} left to play` : ''))),
        h('span.sbar', { role: 'img', 'aria-label': progressText(s) },
          h('i.fin', { style: `width:${sh.finished}%` }), h('i.pl', { style: `width:${sh.played}%` }))),
      h('div.sgames', {}, s.games.map(gameRow)));
    return det;
  }

  function draw() {
    const all = buildSeries(store.games);
    const shown = onlyLeft ? withLeft(all) : all;
    count.textContent = all.length ? `${shown.length} of ${all.length} series` : '';
    list.replaceChildren(...(shown.length ? shown.map(seriesCard)
      : [h('div.empty', {}, all.length ? 'Nothing left to play in any series.' : 'No game has a series yet. Set one in a game’s details.')]));
  }
  draw();
  window.addEventListener('games-changed', draw);
  return () => window.removeEventListener('games-changed', draw);
}
