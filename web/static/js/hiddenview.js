// Hidden page: one list of everything the owner hid (Deals, Similar games, Series), each with Show again. The per-page
// Hidden lists stay where they are; this is the overview. It reads the database only (GET api/hidden): no IGDB or
// IsThereAnyDeal call when it opens.
import { h, emptyBox, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { hiddenList, dealsHide, dealsUnhide, similarHide, similarUnhide, seriesHide, seriesUnhide, coverURL, posterURL, missingPosterURL } from './api.js';
import { groupHidden, hiddenCount, withoutItem } from './hidden.js';

const SHOW = { deals: ({ id }) => dealsUnhide(id), similar: ({ igdb_id }) => similarUnhide(igdb_id), series: ({ collection_id, igdb_id }) => seriesUnhide(collection_id, igdb_id) };
const HIDE = { deals: ({ id }) => dealsHide(id), similar: ({ igdb_id }) => similarHide(igdb_id), series: ({ collection_id, igdb_id }) => seriesHide(collection_id, igdb_id) };

function poster(it) {
  const url = it.kind === 'deals' ? (posterURL({ poster: it.poster, poster_v: it.poster_v }) || coverURL({ cover: it.cover, cover_v: it.v })) : missingPosterURL(it.cover, it.v);
  return h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(it.title)}` }, initials(it.title)));
}

export function renderHidden(root) {
  let groups = [];
  let loaded = false;
  let alive = true;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.hidden-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  async function load() {
    try { groups = groupHidden(await hiddenList()); loaded = true; } catch { /* offline or old server: the page stays empty */ }
    if (alive) draw();
  }

  async function showAgain(it) {
    try { await SHOW[it.kind](it.ref); } catch (e) { toast(e.message || 'Could not show the game again'); return; }
    groups = withoutItem(groups, it.key);
    draw();
    say(`${it.title} shown again.`);
    toastAction(`Showing “${it.title}” again`, 'Undo', async () => {
      try { await HIDE[it.kind](it.ref); } catch (e) { toast(e.message || 'Could not hide the game'); return; }
      await load();
      say(`${it.title} hidden.`);
    });
  }

  function row(it) {
    const body = h('span.up-body', {}, h('span.up-title', {}, it.title), it.year ? h('span.up-date', {}, String(it.year)) : null,
      h('span.up-from', {}, it.note), it.when ? h('span.up-date', {}, it.when) : null);
    return h('li.up-row', {}, h('div.up-open', {}, poster(it), body),
      h('span.si-actions', {}, h('button.hide-chip', { type: 'button', 'aria-label': `Show ${it.title} again`, onclick: () => showAgain(it) }, 'Show again')));
  }

  function draw() {
    const n = hiddenCount(groups);
    page.replaceChildren(...[
      h('h1', {}, 'Hidden'),
      h('p.hint', {}, 'Everything you hid, in one place. Show again puts a game back on the page it came from.'),
      ...groups.map((g) => h('section.up-group', {}, h('h2', {}, g.label, h('small', {}, ` ${g.items.length}`)), h('ul.up-list', {}, g.items.map(row)))),
      n ? h('p.hint', {}, `${n} ${n === 1 ? 'game' : 'games'} hidden.`) : null,
      !n && loaded ? emptyBox('Nothing is hidden. Hide a game on Deals, Similar games or Series and it shows up here.') : null,
    ].filter(Boolean));
  }

  draw();
  load();
  return () => { alive = false; };
}
