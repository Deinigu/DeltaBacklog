// Upcoming page: the games that are not out yet (or just came out), grouped by month. They are the Wishlist games of
// the library and the games of the user's series that the library lacks (cached from IGDB, see seriesview.js).
// Nothing here asks IGDB on its own: the data is read when the owner presses Refresh from IGDB.
import { h, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { store, load as reloadGames, coverURL, posterURL, seriesMissing, seriesRefresh, seriesHide, seriesUnhide, igdbGame, missingPosterURL } from './api.js';
import { openDrawer } from './drawer.js';
import { openAdd } from './add.js';
import { missingIndex, moveHidden, oldestFetch } from './series.js';
import { buildUpcoming, formatRelease, LOCALE } from './upcoming.js';

const imageOf = (g) => posterURL(g) || coverURL(g);

function poster(item) {
  const url = item.kind === 'owned' ? imageOf(item.game) : missingPosterURL(item.missing.cover, item.missing.v);
  const box = h('span.pst' + (item.kind === 'missing' ? '.pst-missing' : ''), {},
    url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(item.title)}` }, initials(item.title)));
  return box;
}

export function renderUpcoming(root) {
  let index = new Map();
  let info = { configured: false, refresh: { running: false } };
  let alive = true;
  let timer = null;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  async function load() {
    try {
      info = await seriesMissing();
      index = missingIndex(info, store.games);
    } catch { /* offline or old server: the wishlist part still works */ }
    if (!alive) return;
    draw();
    if (info.refresh?.running) schedulePoll();
  }
  function schedulePoll() {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const was = info.refresh;
      await load();
      if (alive && was.running && !info.refresh.running) {
        const r = info.refresh;
        try { await reloadGames(); onGames(); } catch { /* offline: the dates of the wishlist show after the next load */ }
        toast(r.failed ? `IGDB refresh: ${r.report || 'failed'}` : `IGDB refreshed. ${r.report || ''}`, 5000);
      }
    }, 1500);
  }
  async function refresh() {
    try {
      info.refresh = await seriesRefresh();
      say('Reading your series and wishlist from IGDB.');
    } catch (e) {
      toast(e.message || 'Could not start the refresh');
      return;
    }
    draw();
    schedulePoll();
  }

  // ---- add / hide (the same cache and hide list as the Series page) ----
  async function add(item, btn) {
    const m = item.missing;
    btn.disabled = true;
    let cand = { igdb_id: m.igdb_id, name: m.title, year: m.year ?? 0, platforms: [], genres: [], themes: [] };
    try { cand = { ...(await igdbGame(m.igdb_id)) }; cand.igdb_parent_id ??= 0; } catch { /* offline or IGDB down: the form opens with what is cached */ }
    btn.disabled = false;
    // igdb_parent_id (the root of its IGDB version chain) is saved with the game, so its original stops being listed at once.
    openAdd({ candidate: { ...cand, series: item.seriesName || cand.series }, status: 'wishlist' });
  }
  async function setHidden(item, hide) {
    const m = item.missing;
    try {
      await (hide ? seriesHide : seriesUnhide)(m.collection_id, m.igdb_id);
    } catch (e) { toast(e.message || (hide ? 'Could not hide the game' : 'Could not show the game again')); return; }
    // Keep the answer we hold in step with the server: the index is rebuilt from it on every games-changed.
    moveHidden(info, m, hide ? 'hidden' : 'missing');
    index = missingIndex(info, store.games);
    draw();
    if (hide) {
      say(`${item.title} hidden.`);
      toastAction(`Hidden “${item.title}”`, 'Undo', () => setHidden(item, false));
    } else say(`${item.title} is back in the list.`);
  }

  // ---- the page ----
  function row(item, released) {
    const date = item.period ? formatRelease(item.date, item.format) : 'Release date not announced';
    const meta = [released ? `Released ${date}` : date, item.platforms.length ? item.platforms.join(', ') : null].filter(Boolean).join(' · ');
    const from = item.kind === 'owned'
      ? h('span.up-tag.own', {}, 'On your wishlist')
      : h('span.up-tag', {}, 'Not in your library');
    const body = h('span.up-body', {},
      h('span.up-title', {}, item.title),
      h('span.up-date', {}, meta),
      h('span.up-from', {}, from, item.seriesName ? h('small', {}, ` · ${item.seriesName}`) : null));
    if (item.kind === 'owned') {
      return h('li.up-row', {}, h('button.up-open', { type: 'button', onclick: () => openDrawer(item.game), 'aria-label': `${item.title}, ${meta}, on your wishlist` }, poster(item), body));
    }
    const addBtn = h('button.add-chip', { type: 'button', 'aria-label': `Add ${item.title} to your wishlist`, onclick: () => add(item, addBtn) }, 'Add');
    return h('li.up-row', {}, h('div.up-open', {}, poster(item), body),
      h('span.si-actions', {}, addBtn, h('button.hide-chip', { type: 'button', 'aria-label': `Hide ${item.title}`, onclick: () => setHidden(item, true) }, 'Hide')));
  }

  function refreshBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      title: info.configured ? '' : 'IGDB is not set up (keys are missing from .env)' },
    r.running ? `Reading IGDB… ${r.total ? `${r.done}/${r.total}` : ''}` : '↻ Refresh from IGDB');
    const old = oldestFetch(index);
    const note = old ? `Release dates were read from IGDB on ${new Date(old).toLocaleDateString(LOCALE)}.`
      : info.configured ? 'Press Refresh from IGDB to read the release dates of your wishlist and your series.' : '';
    return [btn, note];
  }

  function draw() {
    const { groups, recent, total } = buildUpcoming(store.games, index);
    const [btn, note] = refreshBar();
    const nothing = !groups.length && !recent.length;
    page.replaceChildren(...[
      h('h1', {}, 'Upcoming'),
      h('p.hint', {}, 'Games on your wishlist and new games of your series that are not out yet, with release dates from IGDB.'),
      h('div.chips', {}, btn),
      note ? h('p.hint', {}, note) : null,
      recent.length ? h('section.up-group', {}, h('h2', {}, 'Recently released'), h('ul.up-list', {}, recent.map((it) => row(it, true)))) : null,
      ...groups.map((g) => h('section.up-group', {}, h('h2', {}, g.label, h('small', {}, ` ${g.items.length}`)), h('ul.up-list', {}, g.items.map((it) => row(it, false))))),
      nothing ? h('div.empty', {}, 'Nothing is coming up. Set a game’s status to Wishlist, or add series to your library and press Refresh from IGDB.') : null,
      total ? h('p.hint', {}, `${total} ${total === 1 ? 'game' : 'games'} to look forward to.`) : null,
    ].filter(Boolean));
  }

  function onGames() { index = missingIndex(info, store.games); draw(); }
  draw();
  load();
  window.addEventListener('games-changed', onGames);
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); };
}
