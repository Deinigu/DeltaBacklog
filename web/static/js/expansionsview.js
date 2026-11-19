// The Expansions page (#/expansions): every IGDB expansion of the library, with search, filters and sorts (listshell.js over
// the pure filters of expansionlist.js), the "Find expansions for all games" run and the one-time Steam import.
// Nothing here asks IGDB on its own: the data is read when a drawer opens, or when the owner presses the button.
import { h, plural, emptyBox, gradientFor, initials, lazyImg, toast, toastError } from './util.js';
import { store, refreshGames, expansionSyncStatus, expansionSyncStart, expansionSyncCancel, steamImport } from './api.js';
import { createListShell } from './listshell.js';
import { toggleIn, withSelected } from './listfilter.js';
import { badgeStyle } from './colors.js';
import { ownershipIcon } from './ownership.js';
import { playedBadges } from './years.js';
import { formatRelease } from './upcoming.js';
import { expansionCoverURL, expansionRowLabel, isAdded, isDLC, isOwned } from './expansions.js';
import { SORTS, DEFAULT_SORT, NOT_TRACKED, OWNERSHIP, emptyExpansionFilter, expansionFilterCount, filterExpansions, countExpansions, poolOf, platformsOf, poolCount,
  expansionStateFromQuery, expansionStateToQuery, runToLoad, steamOfferCount } from './expansionlist.js';
import { openExpansionSheet } from './expansionsheet.js';
import { openDrawer } from './drawer.js';
import { openLayer } from './layers.js';

/** The line under the buttons while a run goes, or its report. */
export function runLine(st) {
  if (!st) return '';
  if (st.running) return st.kind === 'steam' ? `Reading expansions, then marking the owned ones: ${st.done} of ${st.total} games` : `Reading expansions from IGDB: ${st.done} of ${st.total} games`;
  return st.report || '';
}

// The finish time of the last run whose results were loaded into the store (kept across visits of the page: a run takes minutes
// on the Pi, and the owner may leave the page while it goes).
let loadedRun = null;

const STEAM_URL ='https://store.steampowered.com/dynamicstore/userdata/';
const STEAM_MAX_BYTES = 5 << 20; // the server's limit (expansions.MaxSteamFile)

/** The sheet that explains the Steam import and takes the file. Resolves the run's status, or null when closed. */
function openSteamImport() {
  return new Promise((resolve) => {
    const fail = h('p.exp-err', { role: 'alert' });
    // a browser may save the page as .json, .txt or a web page (.html): the server reads all three
    const file = h('input', { type: 'file', accept: '.json,.txt,.html,.htm,application/json,text/plain,text/html', 'aria-label': 'Steam userdata file' });
    const go = h('button.dl-act', { type: 'button', disabled: true, onclick: upload }, 'Import');
    file.addEventListener('change', () => {
      const f = file.files?.[0];
      const big = !!f && f.size > STEAM_MAX_BYTES; // said at once, without sending megabytes for nothing
      go.disabled = !f || big;
      fail.textContent = big ? 'This file is over 5 MB: it is not the Steam file. Save the page from the address above again.' : '';
    });
    const dlg = h('dialog.dl-sheet.exp-import', { 'aria-label': 'Import owned expansions from Steam' },
      h('b.dl-sheet-title', {}, 'Import owned expansions from Steam'),
      h('p.hint', {}, 'Steam does not list the DLC you own, so this reads one file that you save from your own browser. Nothing is asked of Steam from here: the app never asks for your password, a cookie or a key.'),
      h('ol.exp-steps', {},
        h('li', {}, 'In a browser where you are logged in to Steam, open ', h('a', { href: STEAM_URL, target: '_blank', rel: 'noopener noreferrer' }, STEAM_URL), ' (it shows plain text).'),
        h('li', {}, 'Save the page (Ctrl+S or “Save page as…”) as a file. JSON, text or a web page all work.'),
        h('li', {}, 'Choose that file here and press Import. Only the list of owned apps is read; the file is not kept.')),
      h('p.hint', {}, 'It marks the expansions you own as Owned, only where you have not set an ownership. DLC you own are not added (Steam counts cosmetic packs and soundtracks as DLC): they are shown first, marked Owned on Steam, in their game’s details, so adding the ones you want is a tap. A status or an ownership you set is never changed. You can run it again later.'),
      h('label.frow.col', {}, h('span.flabel', {}, 'The saved file (up to 5 MB)'), file),
      fail,
      h('div.dl-acts', {}, go, h('button.dl-act.dl-close', { type: 'button', onclick: () => dlg.close() }, 'Cancel')));
    let result = null;
    async function upload() {
      const f = file.files?.[0];
      if (!f) return;
      go.disabled = true;
      fail.textContent = '';
      try { result = await steamImport(f); dlg.close(); } catch (e) {
        go.disabled = false;
        fail.textContent = e.status === 409 ? 'A run is already going. Wait for it to finish, then try again.' : e.status === 400 || e.status === 413 ? e.message : 'The upload failed. Check that the server is reachable, then try again.';
      }
    }
    dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
    document.body.append(dlg);
    const release = openLayer(dlg, { requestClose: () => dlg.close(), trap: false });
    dlg.addEventListener('close', () => { dlg.remove(); release(); resolve(result); });
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
    file.focus();
  });
}

export function renderExpansions(root) {
  let alive = true;
  let timer = 0;
  let job = null; // the run's state, null until the first answer
  const { filter, sort: initialSort } = expansionStateFromQuery(location.hash.split('?')[1]);
  let sort = initialSort;
  let pool = poolOf(store.games);
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.list-page.exp-page');
  root.replaceChildren(page, live);

  const results = h('section.sv-results', { 'aria-label': 'Expansions' });
  const tools = h('div.sv-top');
  const ui = createListShell({
    label: 'Search expansions', placeholder: 'Search expansions and games…', noun: ['expansion', 'expansions'],
    getQ: () => filter.q, setQ: (v) => { filter.q = v; },
    onChange: () => draw(),
    onReset: () => Object.assign(filter, emptyExpansionFilter()),
    sorts: SORTS, getSort: () => sort, setSort: (k) => { sort = k; }, defaultSort: DEFAULT_SORT,
  });
  const statusName = (k) => (k === NOT_TRACKED ? 'No status' : store.statusByCode.get(k)?.label || k);
  ui.chips('Status', { open: true, options: () => withSelected([NOT_TRACKED, ...store.statuses.map((s) => s.key)], filter.status).map((v) => ({ value: v, text: statusName(v) })),
    has: (v) => filter.status.has(v), toggle: (v) => toggleIn(filter.status, v), selected: () => filter.status.size, count: (v) => countExpansions(pool, filter, 'status', v) });
  ui.chips('Ownership', { open: true, options: () => OWNERSHIP.map((o) => ({ value: o.key, text: o.label })),
    has: (v) => filter.ownership.has(v), toggle: (v) => toggleIn(filter.ownership, v), selected: () => filter.ownership.size, count: (v) => countExpansions(pool, filter, 'ownership', v) });
  ui.chips('Platform of the game', { open: false, lazy: true, options: () => withSelected(platformsOf(pool).map((x) => x.value), filter.platforms).map((v) => ({ value: v, text: v })),
    has: (v) => filter.platforms.has(v), toggle: (v) => toggleIn(filter.platforms, v), selected: () => filter.platforms.size, count: (v) => countExpansions(pool, filter, 'platforms', v) });
  ui.chips('Game', { open: false, options: () => [{ value: 'finished', text: 'Base game finished' }],
    has: () => filter.baseFinished, toggle: () => { filter.baseFinished = !filter.baseFinished; }, selected: () => (filter.baseFinished ? 1 : 0), count: () => countExpansions(pool, filter, 'baseFinished', true) });
  ui.chips('Steam', { open: false, options: () => [{ value: 'steam', text: 'Owned on Steam, not added yet' }],
    has: () => filter.steam, toggle: () => { filter.steam = !filter.steam; }, selected: () => (filter.steam ? 1 : 0), count: () => countExpansions(pool, filter, 'steam', true) });
  ui.chips('Hidden', { open: false, options: () => [{ value: 'hidden', text: 'Only the hidden ones' }],
    has: () => filter.hidden, toggle: () => { filter.hidden = !filter.hidden; }, selected: () => (filter.hidden ? 1 : 0), count: () => countExpansions(pool, filter, 'hidden', true) });
  page.replaceChildren(h('h1', {}, 'Expansions'),
    h('p.hint', {}, 'Your expansions and DLC: the ones you own or added. They live inside their game: tap one to set its status, ownership, years played and notes. They are not counted as games. To add another, open its game: IGDB’s other expansions and DLC are listed there, folded.'),
    tools, ...ui.nodes(results));

  // ---- the runs ----
  async function loadJob() {
    job = await expansionSyncStatus().catch(() => job);
    if (!alive) return;
    drawTools();
    if (job?.running) { schedule(); return; }
    // a run that ended while the page was closed: its expansions and marks are not in the store yet
    const ended = runToLoad(job, loadedRun);
    if (ended) {
      loadedRun = ended;
      await refreshGames().catch(() => {});
      if (alive) onGames();
    }
  }
  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const was = job?.running;
      job = await expansionSyncStatus().catch(() => job);
      if (!alive) return;
      if (was && job && !job.running) {
        loadedRun = job.finished_at || loadedRun;
        await refreshGames().catch(() => {});
        toast(job.report || 'Done', 6000);
        live.textContent = job.report || '';
        onGames();
      }
      drawTools();
      if (job?.running) schedule();
    }, 1200);
  }
  async function start(fn, what) {
    try { job = await fn(); } catch (e) {
      if (e.status === 409 && e.data) { job = e.data; toast('A run is already going.'); } else if (e.status === 503) { toast(e.message); return; } else { toastError(what, e); return; }
    }
    drawTools();
    schedule();
  }
  async function importSteam() {
    const st = await openSteamImport();
    if (!st) return;
    job = st;
    drawTools();
    schedule();
  }
  async function stop() {
    try { job = await expansionSyncCancel(); } catch { toastError('stop the run'); }
    drawTools();
  }

  function drawTools() {
    const running = !!job?.running;
    const line = runLine(job);
    tools.replaceChildren(...[
      h('div.chips', {},
        running ? h('button.chip', { type: 'button', onclick: stop }, 'Stop')
          : h('button.chip', { type: 'button', onclick: () => start(expansionSyncStart, 'start the search') }, '↻ Find expansions for all games'),
        h('button.chip', { type: 'button', disabled: running || null, onclick: importSteam }, 'Import owned expansions from Steam…')),
      running ? h('progress', { max: String(job.total || 1), value: String(job.done || 0), 'aria-label': 'Progress' }) : null,
      line ? h('p.hint', { class: job?.failed ? 'bad' : '' }, line) : null,
    ].filter(Boolean));
  }

  // ---- the list ----
  function row({ x, game }) {
    const st = x.status && store.statusByCode.get(x.status);
    const url = expansionCoverURL(x);
    const played = playedBadges(x, 3);
    const icon = x.ownership && ownershipIcon(x.ownership);
    const date = x.date ? formatRelease(x.date, 0) : 'Release date not announced';
    const open = h('button.up-open', { type: 'button', onclick: () => openExpansionSheet(x.id), 'aria-label': expansionRowLabel(x, (c) => store.statusByCode.get(c)?.label ?? c, game) },
      h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(x.title)}` }, initials(x.title))),
      h('span.up-body', {},
        h('span.up-title', { translate: 'no' }, x.title),
        h('span.up-date', {}, date),
        h('span.exp-meta', {},
          // a row not added (a Steam offer, a hidden IGDB entry) has no status by definition: no "No status" pill (as in the drawer)
          st ? h('span.exp-st', { style: badgeStyle(st.color) }, st.label) : isAdded(x) ? h('span.exp-st.none', {}, 'No status') : null,
          isDLC(x) ? h('span.exp-kind', {}, 'DLC') : null,
          x.steam_owned && !isOwned(x) ? h('span.exp-kind.steam', {}, 'Owned on Steam') : null,
          icon ? h('span.exp-own', { title: x.ownership, role: 'img', 'aria-label': x.ownership }, icon) : null,
          ...played.periods.map((p) => h('span.yr.period', { title: p.label }, p.icon)),
          played.earlier ? h('span.yr.earlier', {}, `+${played.earlier}`) : null,
          ...played.years.map((y) => h('span.yr', {}, String(y.year))))));
    return h('li.up-row' + (x.hidden ? '.is-hidden' : ''), {}, open,
      h('span.si-actions', {}, h('button.exp-game', { type: 'button', 'aria-label': `Open ${game.title}`, onclick: () => openDrawer(game) }, game.title)));
  }

  function draw() {
    const rows = filterExpansions(pool, filter, sort);
    const of = poolCount(pool, filter);
    const nothing = !of && !filter.hidden && !filter.steam;
    const offers = filter.hidden || filter.steam ? 0 : steamOfferCount(pool);
    const showOffers = () => { Object.assign(filter, emptyExpansionFilter(), { steam: true }); ui.input.value = ''; draw(); };
    results.replaceChildren(...[
      offers ? h('p.hint.exp-offers', {}, `${plural(offers, 'DLC', 'DLC')} you own on Steam ${offers === 1 ? 'is' : 'are'} not added yet. `,
        h('button.exp-link', { type: 'button', onclick: showOffers }, 'Show them')) : null,
      filter.steam && !filter.hidden && rows.length ? h('p.hint', {}, 'The Steam import found these among your apps. Open one and set a status or an ownership to add it; Steam counts cosmetic packs and soundtracks as DLC too, so hide the ones you do not want.') : null,
      rows.length ? h('ul.up-list', {}, rows.map(row)) : null,
      nothing ? emptyBox('No expansions yet. Open a game and add one from its expansions (set a status, or say you own it), or read them for every game at once and import the ones you own on Steam.', h('button.btn', { type: 'button', onclick: () => start(expansionSyncStart, 'start the search') }, '↻ Find expansions for all games')) : null,
      !nothing && !rows.length ? emptyBox(filter.hidden ? 'You have not hidden any expansion.' : filter.steam && !of ? 'No DLC you own on Steam is waiting to be added. The Steam import finds them.' : 'No expansions match these filters.', h('button.btn', { type: 'button', onclick: () => { Object.assign(filter, emptyExpansionFilter()); ui.input.value = ''; draw(); } }, 'Clear filters')) : null,
    ].filter(Boolean));
    const pills = [
      ...[...filter.status].sort().map((s) => ({ name: `Status: ${statusName(s)}`, kids: [h('b', {}, 'Status'), statusName(s)], remove: () => filter.status.delete(s) })),
      ...OWNERSHIP.filter((o) => filter.ownership.has(o.key)).map((o) => ({ name: `Ownership: ${o.label}`, kids: [h('b', {}, 'Ownership'), o.label], remove: () => filter.ownership.delete(o.key) })),
      ...[...filter.platforms].sort().map((p) => ({ name: `Platform: ${p}`, kids: [h('b', {}, 'Platform'), p], remove: () => filter.platforms.delete(p) })),
      ...(filter.baseFinished ? [{ name: 'Base game finished', kids: [h('b', {}, 'Game'), 'finished'], remove: () => { filter.baseFinished = false; } }] : []),
      ...(filter.steam ? [{ name: 'Owned on Steam, not added yet', kids: [h('b', {}, 'Showing'), 'on Steam'], remove: () => { filter.steam = false; } }] : []),
      ...(filter.hidden ? [{ name: 'Hidden ones', kids: [h('b', {}, 'Showing'), 'hidden ones'], remove: () => { filter.hidden = false; } }] : []),
    ];
    ui.paint({ filters: expansionFilterCount(filter), shown: rows.length, of, pills });
    ui.writeUrl('expansions', expansionStateToQuery({ filter, sort }));
  }

  function onGames() { pool = poolOf(store.games); draw(); }
  drawTools();
  draw();
  loadJob();
  window.addEventListener('games-changed', onGames);
  window.addEventListener('games-reloaded', onGames);
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); window.removeEventListener('games-reloaded', onGames); ui.destroy(); };
}
