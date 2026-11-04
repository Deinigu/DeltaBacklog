// Similar page: games IGDB lists as similar to the games the user finished or marked as a favourite, that the library
// does not have, ranked by how many of those games suggest them, with Add and Hide. The list is read from the
// server's cache; nothing here asks IGDB unless the owner presses Refresh from IGDB. The search, filters and sort are the
// shared toolbar (listshell.js) over the pure filters of similarfilter.js.
import { h, emptyBox, gradientFor, initials, lazyImg, toast, toastAction, toastError } from './util.js';
import { store, similarList, similarRefresh, similarHide, similarUnhide, igdbGame, missingPosterURL } from './api.js';
import { openAdd } from './add.js';
import { similarIndex, setHiddenFlag, becauseText, isSource } from './similar.js';
import { createListShell } from './listshell.js';
import { toggleIn, withSelected, intIn } from './listfilter.js';
import { SORTS, DEFAULT_SORT, MIN_YEAR, MAX_YEAR, emptySimilarFilter, similarFilterCount, poolOf, filterSimilar, countSimilar, genresOf, platformsOf, sourcesOf, similarStateFromQuery, similarStateToQuery } from './similarfilter.js';
import { formatDate } from './format.js';

function poster(s) {
  const url = missingPosterURL(s.cover, s.v);
  return h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(s.title)}` }, initials(s.title)));
}

export function renderSimilar(root) {
  let info = { configured: false, suggestions: [], refresh: { running: false } };
  let index = similarIndex(info, store.games);
  let alive = true;
  let timer = null;
  let loaded = false;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.sim-page.list-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  // The page state is in the address (#/similar?genre=RPG&from=2015...), so a reload and Back keep it.
  const { filter, sort: startSort } = similarStateFromQuery(location.hash.split('?')[1]);
  let sort = startSort;
  const results = h('section.sv-results', { 'aria-label': 'Suggestions' });
  const tools = h('div.sv-top');
  const ui = createListShell({
    label: 'Search suggestions', placeholder: 'Search suggestions…', noun: ['suggestion', 'suggestions'],
    getQ: () => filter.q, setQ: (v) => { filter.q = v; },
    sorts: SORTS, getSort: () => sort, setSort: (k) => { sort = k; }, defaultSort: DEFAULT_SORT,
    onChange: () => draw(),
    onReset: () => Object.assign(filter, emptySimilarFilter()),
  });
  // The options (genres, platforms, source games) come from every suggestion, hidden or not, and only change with the index:
  // worked out once per index (similarIndex makes a new one on every load and games-changed), not on every redraw.
  let facetsOf = null, facets = null;
  const fac = () => {
    if (facetsOf !== index) {
      const pool = poolOf(index, { withHidden: true });
      const sources = sourcesOf(pool);
      facets = { genres: genresOf(pool).map((x) => x.value), platforms: platformsOf(pool).map((x) => x.value), sources: sources.map((x) => x.value), title: new Map(sources.map((x) => [x.value, x.title])) };
      facetsOf = index;
    }
    return facets;
  };
  const sourceTitle = (id) => fac().title.get(id) ?? `Game ${id}`;
  ui.chips('Genre', { open: true, options: () => withSelected(fac().genres, filter.genres).map((v) => ({ value: v, text: v })),
    has: (v) => filter.genres.has(v), toggle: (v) => toggleIn(filter.genres, v), selected: () => filter.genres.size, count: (v) => countSimilar(index, filter, 'genres', v) });
  ui.chips('Platform', { open: false, lazy: true, options: () => withSelected(fac().platforms, filter.platforms).map((v) => ({ value: v, text: v })),
    has: (v) => filter.platforms.has(v), toggle: (v) => toggleIn(filter.platforms, v), selected: () => filter.platforms.size, count: (v) => countSimilar(index, filter, 'platforms', v) });
  ui.range('Release year', { min: MIN_YEAR, max: MAX_YEAR, get: () => ({ from: filter.from, to: filter.to }),
    set: (from, to) => { filter.from = intIn(from, MIN_YEAR, MAX_YEAR); filter.to = intIn(to, MIN_YEAR, MAX_YEAR); } });
  ui.chips('Because of', { open: false, lazy: true,
    options: () => withSelected(fac().sources, filter.because).map((v) => ({ value: v, text: sourceTitle(v) })),
    has: (v) => filter.because.has(v), toggle: (v) => toggleIn(filter.because, v), selected: () => filter.because.size, count: (v) => countSimilar(index, filter, 'because', v) });
  ui.chips('Games you hid', { open: true, options: () => (index.hidden.length || filter.withHidden ? [{ value: 'withHidden', text: 'Include the games I hid' }] : []),
    has: () => filter.withHidden, toggle: () => { filter.withHidden = !filter.withHidden; }, selected: () => (filter.withHidden ? 1 : 0),
    count: () => countSimilar(index, filter, 'withHidden', true) });
  page.replaceChildren(h('h1', {}, 'Similar games'),
    h('p.hint', {}, 'Games IGDB lists as similar to the ones you finished or marked as favourites, that you do not have. The more of your games suggest one, the higher it is.'),
    tools, ...ui.nodes(results));

  async function load() {
    try {
      info = await similarList();
      index = similarIndex(info, store.games);
      loaded = true;
    } catch { /* offline or old server: the page stays empty */ }
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
        toast(r.failed ? `IGDB refresh: ${r.report || 'failed'}` : `IGDB refreshed. ${r.report || ''}`, 5000);
      }
    }, 1500);
  }
  async function refresh() {
    try {
      info.refresh = await similarRefresh();
      say('Reading similar games from IGDB.');
    } catch (e) {
      toastError('start the refresh', e);
      return;
    }
    draw();
    schedulePoll();
  }

  // ---- add / hide ----
  async function add(s, btn) {
    btn.disabled = true;
    let cand = { igdb_id: s.igdb_id, name: s.title, year: s.year ?? 0, platforms: [], genres: [], themes: [] };
    try { cand = { ...(await igdbGame(s.igdb_id)) }; cand.igdb_parent_id ??= 0; } catch { /* offline or IGDB down: the form opens with what is cached */ }
    btn.disabled = false;
    // igdb_parent_id (the root of its IGDB version chain) is saved with the game, so its other entries stop being suggested at once.
    openAdd({ candidate: cand, status: 'backlog' });
  }
  async function setHidden(s, hide) {
    try {
      await (hide ? similarHide : similarUnhide)(s.igdb_id);
    } catch (e) { toastError(hide ? 'hide the game' : 'show the game again', e); return; }
    // Keep the answer we hold in step with the server: the index is rebuilt from it on every games-changed.
    setHiddenFlag(info, s.igdb_id, hide);
    index = similarIndex(info, store.games);
    draw();
    if (hide) {
      say(`${s.title} hidden.`);
      toastAction(`Hidden “${s.title}”`, 'Undo', () => setHidden(s, false));
    } else say(`${s.title} is back in the list.`);
  }

  // ---- the page ----
  function row(s) {
    const meta = s.year ?? 'Year unknown';
    const body = h('span.up-body', {}, h('span.up-title', {}, s.title), h('span.up-date', {}, s.hidden ? `${meta} · Hidden` : meta), h('span.up-from.sim-why', {}, becauseText(s.sourceGames)));
    const addBtn = h('button.add-chip', { type: 'button', 'aria-label': `Add ${s.title} to your library`, onclick: () => add(s, addBtn) }, 'Add');
    const hideBtn = s.hidden ? h('button.hide-chip', { type: 'button', 'aria-label': `Show ${s.title} again`, onclick: () => setHidden(s, false) }, 'Show again')
      : h('button.hide-chip', { type: 'button', 'aria-label': `Hide ${s.title}`, onclick: () => setHidden(s, true) }, 'Hide');
    return h('li.up-row', {}, h('div.up-open', {}, poster(s), body), h('span.si-actions', {}, addBtn, hideBtn));
  }

  function refreshBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      title: info.configured ? '' : 'IGDB is not set up (keys are missing from .env)' },
    r.running ? `Reading IGDB… ${r.total ? `${r.done}/${r.total}` : ''}` : '↻ Refresh from IGDB');
    const note = info.fetched_at ? `Read from IGDB on ${formatDate(info.fetched_at)}.`
      : info.configured ? 'Press Refresh from IGDB to find games similar to the ones you finished or marked as favourites.' : '';
    return [btn, note];
  }

  function draw() {
    const { items, hidden } = index;
    const [btn, note] = refreshBar();
    const sources = store.games.filter(isSource).length;
    const shown = filterSimilar(index, filter, sort);
    const total = poolOf(index, filter).length;
    const empty = !items.length;
    let emptyText = 'Nothing to suggest right now.';
    if (!sources) emptyText = 'Finish a game, or mark one as a favourite, and link it to IGDB (game details, Edit, Fill from IGDB). Its similar games show up here.';
    else if (loaded && !info.fetched_at) emptyText = info.configured ? 'Nothing was read from IGDB yet.' : 'IGDB is not set up (keys are missing from .env).';
    const firstRead = empty && sources && loaded && !info.fetched_at && info.configured; // the Refresh button sits in the empty state, once
    const noFacets = items.length && !items.some((s) => s.genres?.length || s.platforms?.length) && info.configured; // cached before genres and platforms were read
    const reset = () => { Object.assign(filter, emptySimilarFilter()); ui.input.value = ''; draw(); };
    tools.replaceChildren(...[
      firstRead ? null : h('div.chips', {}, btn),
      note && !firstRead ? h('p.hint', {}, note) : null,
      noFacets ? h('p.hint', {}, 'Genres and platforms show up here after the next Refresh from IGDB.') : null,
    ].filter(Boolean));
    results.replaceChildren(...[
      shown.length ? h('ul.up-list', {}, shown.map(row)) : null,
      empty && !filter.withHidden ? emptyBox(emptyText, firstRead && btn) : null,
      !shown.length && total ? emptyBox('No suggestions match these filters.', h('button.btn', { type: 'button', onclick: reset }, 'Clear filters')) : null,
      items.length ? h('p.hint', {}, `${items.length} ${items.length === 1 ? 'suggestion' : 'suggestions'} from ${sources} of your games.`) : null,
      hidden.length && !filter.withHidden ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${hidden.length})`),
        h('ul', {}, hidden.map((s) => h('li', {}, h('span', {}, s.title, h('small', {}, s.year ? ` (${s.year})` : '')),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${s.title} again`, onclick: () => setHidden(s, false) }, 'Show again'))))) : null,
    ].filter(Boolean));
    const yearText = filter.from && filter.to ? `${filter.from}–${filter.to}` : filter.from ? `from ${filter.from}` : `up to ${filter.to}`;
    const pills = [
      ...[...filter.genres].sort().map((g) => ({ name: `Genre: ${g}`, kids: [h('b', {}, 'Genre'), g], remove: () => filter.genres.delete(g) })),
      ...[...filter.platforms].sort().map((p) => ({ name: `Platform: ${p}`, kids: [h('b', {}, 'Platform'), p], remove: () => filter.platforms.delete(p) })),
      filter.from || filter.to ? { name: `Release year ${yearText}`, kids: [h('b', {}, 'Year'), yearText], remove: () => { filter.from = 0; filter.to = 0; } } : null,
      ...[...filter.because].map((id) => ({ name: `Because of ${sourceTitle(id)}`, kids: [h('b', {}, 'Because of'), sourceTitle(id)], remove: () => filter.because.delete(id) })),
      filter.withHidden ? { name: 'Including the games you hid', kids: ['Including hidden'], remove: () => { filter.withHidden = false; } } : null,
    ].filter(Boolean);
    ui.paint({ filters: similarFilterCount(filter), shown: shown.length, of: Math.max(items.length, shown.length), pills }); // 'of' is the size without the hidden games, like the other pages
    ui.writeUrl('similar', similarStateToQuery({ filter, sort }));
  }

  function onGames() { index = similarIndex(info, store.games); draw(); }
  draw();
  load();
  window.addEventListener('games-changed', onGames);
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); ui.destroy(); };
}
