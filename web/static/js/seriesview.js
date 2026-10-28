// Series page: a grid of series tiles (a fan of the series' posters, progress), and each series opened on its own
// route (#/series/<key>) as a poster shelf in release order. The games of the series' IGDB collection that the
// library does not have sit in the shelf as "Not in your library", with Add and Hide.
import { h, emptyBox, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { store, coverURL, posterURL, seriesMissing, seriesRefresh, seriesHide, seriesUnhide, igdbGame, missingPosterURL } from './api.js';
import { openDrawer } from './drawer.js';
import { openAdd } from './add.js';
import { LOCALE } from './upcoming.js';
import {
  buildSeries, collapseMissing, withLeft, withMissing, moveHidden, barShares, isFinished, isPlayed, missingIndex, extraOf, shelfItems, fanGames, seriesSummary, oldestFetch,
} from './series.js';

// The filters survive going into a series and back (the route re-renders the page).
let onlyLeft = false;
let onlyNew = false;
const SHOW_MISSING = 6; // a series with more games that you do not have than this is collapsed behind "Show all"

const imageOf = (g) => posterURL(g) || coverURL(g);
const href = (key) => `#/series/${encodeURIComponent(key)}`;
const bar = (s) => {
  const sh = barShares(s);
  return h('span.sbar', { 'aria-hidden': 'true' }, h('i.fin', { style: `width:${sh.finished}%` }), h('i.pl', { style: `width:${sh.played}%` }));
};

/** A 2:3 picture box: the game's poster (or wide cover, centre-cropped), else a coloured placeholder with initials. */
function poster(game, extraClass = '') {
  const url = imageOf(game);
  return h('span.pst' + extraClass, {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(game.title)}` }, initials(game.title)));
}

function stateOf(g) { return isFinished(g) ? 'finished' : isPlayed(g) ? 'played' : 'unplayed'; }
const STATE_LABEL = { finished: 'Finished', played: 'Played', unplayed: 'Not played yet' };

export function renderSeries(root, openKey = null) {
  let index = new Map();
  let info = { configured: false, refresh: { running: false } };
  let alive = true;
  let timer = null;
  let focused = false;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.sv-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  // ---- loading the cached "not registered" games (the server never asks IGDB for this) ----
  async function loadMissing() {
    try {
      info = await seriesMissing();
      index = missingIndex(info, store.games);
    } catch { /* offline or old server: the page works without them */ }
    if (!alive) return;
    draw();
    if (info.refresh?.running) schedulePoll();
  }
  function schedulePoll() {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const was = info.refresh;
      await loadMissing();
      if (alive && was.running && !info.refresh.running) {
        const r = info.refresh;
        toast(r.failed ? `IGDB refresh: ${r.report || 'failed'}` : `IGDB refreshed. ${r.report || ''}`, 5000);
      }
    }, 1500);
  }
  async function refresh() {
    try {
      info.refresh = await seriesRefresh();
      say('Reading your series from IGDB.');
    } catch (e) {
      toast(e.message || 'Could not start the refresh');
      return;
    }
    draw();
    schedulePoll();
  }

  // ---- add / hide ----
  async function add(m, seriesName, btn) {
    btn.disabled = true;
    const fallback = { igdb_id: m.igdb_id, name: m.title, year: m.year ?? 0, platforms: [], genres: [], themes: [] };
    let cand = fallback;
    try { cand = { ...(await igdbGame(m.igdb_id)) }; cand.igdb_parent_id ??= 0; } catch { /* offline or IGDB down: the form opens with what is cached */ }
    btn.disabled = false;
    // The series is the user's own name for it, not IGDB's. igdb_parent_id (the root of its IGDB version chain; 0 = itself)
    // is saved with the game, so its original and remaster stop being listed at once.
    openAdd({ candidate: { ...cand, series: seriesName } });
  }
  async function hide(m) {
    try {
      await seriesHide(m.collection_id, m.igdb_id);
    } catch (e) { toast(e.message || 'Could not hide the game'); return; }
    moveTo(m, 'hidden');
    draw();
    say(`${m.title} hidden.`);
    toastAction(`Hidden “${m.title}”`, 'Undo', () => unhide(m));
  }
  async function unhide(m) {
    try {
      await seriesUnhide(m.collection_id, m.igdb_id);
    } catch (e) { toast(e.message || 'Could not show the game again'); return; }
    moveTo(m, 'missing');
    draw();
    say(`${m.title} is back in the list.`);
  }
  // Keep the answer we hold in step with the server (the index is rebuilt from it on every games-changed), so a hidden
  // game stays hidden and the page redraws without another request.
  function moveTo(m, to) {
    moveHidden(info, m, to);
    index = missingIndex(info, store.games);
  }

  // ---- the page of tiles ----
  function tile(s) {
    const extra = extraOf(index, s);
    const games = fanGames(s, (g) => !!imageOf(g));
    const n = extra.missing.length;
    return h('a.tile', { href: href(s.key), 'aria-label': `${s.name}: ${seriesSummary(s, extra)}` },
      h('span.fan', { 'aria-hidden': 'true' }, games.map((g, i) => {
        const p = poster(g);
        p.style.setProperty('--i', String(i - (games.length - 1) / 2));
        return p;
      })),
      h('span.stitle', {}, s.name),
      bar(s),
      h('small', {}, `${s.finished}/${s.total} finished`, n ? h('b.newn', {}, ` · +${n} not in your library`) : null));
  }

  function refreshBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      title: info.configured ? 'Refresh from IGDB' : 'IGDB is not set up (keys are missing from .env)' },
    r.running ? `Reading IGDB… ${r.total ? `${r.done}/${r.total}` : ''}` : '↻ Refresh');
    const old = oldestFetch(index);
    const note = old ? `Missing games read from IGDB on ${new Date(old).toLocaleDateString(LOCALE)}.`
      : info.configured ? 'Press Refresh to read the games of your series that you do not have yet.' : '';
    return [btn, note];
  }

  function drawList() {
    const all = buildSeries(store.games);
    let shown = onlyLeft ? withLeft(all) : all;
    if (onlyNew) shown = withMissing(shown, index);
    const noYear = all.reduce((n, s) => n + (s.total > 1 ? s.noYear : 0), 0); // a game alone in its series has no order to get wrong
    const leftBtn = h('button.chip', { type: 'button', 'aria-pressed': String(onlyLeft), onclick: () => { onlyLeft = !onlyLeft; draw(); } }, 'Left to play');
    const newBtn = h('button.chip', { type: 'button', 'aria-pressed': String(onlyNew), onclick: () => { onlyNew = !onlyNew; draw(); } }, 'Not in library');
    const [rbtn, note] = refreshBar();
    page.replaceChildren(...[ // replaceChildren would print a null as the text "null"
      h('h1', {}, 'Series'),
      h('p.hint', {}, 'Progress through each series. Open one for its games in release order.'),
      h('div.chips', {}, leftBtn, newBtn, rbtn),
      all.length || note ? h('p.hint', {}, [all.length ? `${shown.length} of ${all.length} series` : '', note].filter(Boolean).join(' · ')) : null,
      noYear ? h('p.hint', {}, `${noYear} ${noYear === 1 ? 'game has' : 'games have'} no release year, so ${noYear === 1 ? 'its' : 'their'} order is only by title. Use Fill from IGDB in a game’s details to set it.`) : null,
      shown.length ? h('div.series-list.tiles', {}, shown.map(tile))
        : !all.length ? emptyBox('No game has a series yet. Set one in a game’s details.', h('a.btn.primary', { href: '#/library' }, 'Open the Library'))
          : onlyNew ? emptyBox(oldestFetch(index) ? 'No series has a game you do not have yet.' : 'Nothing was read from IGDB yet.', info.configured && !oldestFetch(index) && rbtn)
            : emptyBox('Nothing left to play in any series.', h('button.chip', { type: 'button', onclick: () => { onlyLeft = false; draw(); } }, 'Show all series')),
    ].filter(Boolean));
  }

  // ---- one series as a poster shelf ----
  function cell(item, s) {
    if (item.kind === 'owned') {
      const g = item.game;
      const st = store.statusByCode.get(g.status);
      const state = stateOf(g);
      const box = poster(g, '.pst-' + state);
      if (state === 'finished') box.append(h('i.pmark', { 'aria-hidden': 'true' }, '✓'));
      else if (state === 'played') box.append(h('i.pmark.pl', { 'aria-hidden': 'true' }, '▶'));
      return h('div.shelf-cell', {},
        h('button.si', { type: 'button', onclick: () => openDrawer(g), 'aria-label': `${g.title}, ${g.release_year ?? 'year unknown'}, ${STATE_LABEL[state]}${st ? `, ${st.label}` : ''}` }, box),
        h('span.si-name', {}, g.title), h('small', {}, g.release_year ?? 'Year unknown'));
    }
    if (item.kind === 'wishlist') { // not in the library yet: the missing look, with its own mark
      const g = item.game;
      const box = poster(g, '.pst-missing');
      box.append(h('i.pnew', {}, 'On your wishlist'));
      return h('div.shelf-cell.missing.wish', {},
        h('button.si', { type: 'button', onclick: () => openDrawer(g), 'aria-label': `${g.title}, ${g.release_year ?? 'year unknown'}, on your wishlist` }, box),
        h('span.si-name', {}, g.title), h('small', {}, g.release_year ?? 'Year unknown'));
    }
    const m = item.game;
    const url = missingPosterURL(m.cover, m.v);
    const box = h('span.pst.pst-missing', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(m.title)}` }, h('b', {}, m.title)));
    box.append(h('i.pnew', {}, 'Not in your library'));
    const addBtn = h('button.add-chip', { type: 'button', 'aria-label': `Add ${m.title} to your library`, onclick: () => add(m, s.name, addBtn) }, 'Add');
    return h('div.shelf-cell.missing', { role: 'group', 'aria-label': `${m.title}, ${m.year ?? 'year unknown'}, not in your library` },
      box, h('span.si-name', {}, m.title), h('small', {}, m.year ?? 'Year unknown'),
      h('span.si-actions', {}, addBtn, h('button.hide-chip', { type: 'button', 'aria-label': `Hide ${m.title} from this series`, onclick: () => hide(m) }, 'Hide')));
  }

  let showAllMissing = false;
  // Show all redraws the page: keep the keyboard where the user was (the first game that was hidden) and say what happened.
  function showAll(shown, missing) {
    showAllMissing = true;
    draw();
    page.querySelectorAll('.pgrid > .shelf-cell')[shown]?.querySelector('button')?.focus();
    say(`Showing all ${missing} games that are not in your library.`);
  }
  function drawSeries() {
    const s = buildSeries(store.games).find((x) => x.key === openKey);
    const back = h('a.chip.back', { href: '#/series' }, '‹ All series');
    if (!s) {
      page.replaceChildren(back, h('div.empty', {}, 'That series is not in your library (any more).'));
      return;
    }
    const extra = extraOf(index, s);
    const title = h('h1.sv-title', { tabindex: '-1' }, s.name);
    const hidden = extra.hidden.length ? h('details.hidden-list', {},
      h('summary', {}, `Hidden (${extra.hidden.length})`),
      h('ul', {}, extra.hidden.map((m) => h('li', {}, h('span', {}, m.title, h('small', {}, m.year ? ` (${m.year})` : '')),
        h('button.hide-chip', { type: 'button', 'aria-label': `Show ${m.title} again`, onclick: () => unhide(m) }, 'Show again'))))) : null;
    const [rbtn] = refreshBar();
    const shelf = showAllMissing ? { items: shelfItems(s, extra), hidden: 0 } : collapseMissing(shelfItems(s, extra), SHOW_MISSING);
    page.replaceChildren(...[back, title, h('p.hint', {}, seriesSummary(s, extra)), bar(s),
      h('div.chips.sv-tools', {}, rbtn),
      h('div.pgrid', {}, shelf.items.map((it) => cell(it, s))),
      shelf.hidden ? h('div.chips.sv-more', {}, h('button.chip', { type: 'button', onclick: () => showAll(shelf.items.length, extra.missing.length) }, `Show all ${extra.missing.length} not in your library (${shelf.hidden} more)`)) : null,
      !extra.collections.length && info.configured ? h('p.hint', {}, 'No IGDB collection is known for this series yet. Link its games to IGDB, then use Refresh from IGDB.') : null,
      hidden].filter(Boolean));
    if (!focused) { focused = true; title.focus({ preventScroll: true }); }
  }

  function draw() {
    if (openKey != null) drawSeries(); else drawList();
  }
  function onGames() { index = missingIndex(info, store.games); draw(); }
  draw();
  loadMissing();
  window.addEventListener('games-changed', onGames);
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); };
}
