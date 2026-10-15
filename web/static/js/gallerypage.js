// The Screenshots page (#/screenshots): every screenshot of the library, by game or newest first.
import { h, toast, debounce, icon, gradientFor, initials } from './util.js';
import { store, coverURL, listAllShots } from './api.js';
import { openDrawer } from './drawer.js';
import { PAGE, galleryQuery, groupRuns, remaining, shotsLabel, runLabel, stepAcross, emptyText } from './gallery.js';
import { openShotViewer } from './shotviewer.js';

const SORT_KEY = 'gallery-sort';
const MAX_RELOAD = 500; // the most pictures a refresh asks for at once (the API's own limit)
const readSort = () => { try { return localStorage.getItem(SORT_KEY) === 'newest' ? 'newest' : 'game'; } catch { return 'game'; } };

export function renderGallery(root) {
  let alive = true;
  let viewer = null;
  let list = [];
  let total = 0;
  let available = true;
  let ticket = 0; // a newer load makes an older answer irrelevant
  let loading = false; // a first-page load is in flight
  let paging = null; // the promise of a "Show more" in flight
  let groups = []; // by game: {gameId, n, grid, nEl}, one per header drawn
  let flat = null; // newest first: the one grid
  const f = { q: '', year: '', sort: readSort() };

  const input = h('input', { type: 'search', placeholder: 'Search by game or caption', 'aria-label': 'Search screenshots', autocomplete: 'off' });
  const year = h('select', { 'aria-label': 'Year' }, h('option', { value: '' }, 'Any year'));
  const sort = h('select', { 'aria-label': 'Layout' },
    h('option', { value: 'game' }, 'By game'), h('option', { value: 'newest' }, 'All, newest first'));
  sort.value = f.sort;
  const count = h('p.hint.gal-count', { role: 'status' });
  const notice = h('p.shots-notice', { role: 'status', hidden: true }, 'Screenshot storage is not available (is the drive connected?). The pictures cannot be shown until it is back.');
  const body = h('div.gal-body');
  const more = h('button.btn.gal-more', { type: 'button', hidden: true, onclick: () => loadMore().catch(() => {}) });
  root.replaceChildren(h('div.gal-page', {},
    h('header.gal-head', {}, h('h1', {}, 'Screenshots')),
    h('div.gal-tools', {}, h('div.search', {}, icon('search'), input), year, sort),
    count, notice, body, more));

  const thumb = (shot, index) => {
    const img = h('img', { src: shot.thumb, alt: shot.caption || `${shot.game_title} screenshot`, loading: 'lazy', decoding: 'async', width: '240', height: '135' });
    const b = h('button.shot-thumb', { type: 'button', 'data-i': String(index), 'aria-label': `Open screenshot ${index + 1} of ${total}: ${shot.game_title}`, onclick: () => openAt(index) }, img);
    img.addEventListener('error', () => b.classList.add('missing'));
    return b;
  };

  function gameSection(run) {
    const g = store.byId.get(run.gameId);
    const url = g && coverURL(g);
    const art = h('span.gal-cover', {}, url ? h('img', { src: url, alt: '', loading: 'lazy', decoding: 'async' }) : h('span.ph', { style: `background:${gradientFor(run.title)}` }, initials(run.title)));
    const nEl = h('span.gal-n');
    const head = h('button.gal-game', { type: 'button', disabled: !g, onclick: () => { const cur = store.byId.get(run.gameId); if (cur) openDrawer(cur); }, 'aria-label': `Open ${run.title}` },
      art, h('span.gal-name', {}, run.title), nEl);
    const grid = h('div.shots-grid.gal-grid');
    body.append(h('section.gal-group', {}, head, grid));
    return { gameId: run.gameId, n: 0, grid, nEl };
  }

  /** Add pictures (list[base...]) to what is on screen: by game they extend the last header when the game goes on. */
  function addItems(items, base) {
    if (f.sort === 'game') {
      for (const run of groupRuns(items, base)) {
        let grp = groups[groups.length - 1];
        if (!grp || grp.gameId !== run.gameId) { grp = gameSection(run); groups.push(grp); }
        grp.grid.append(...run.items.map(({ shot, index }) => thumb(shot, index)));
        grp.n += run.items.length;
      }
    } else {
      if (!flat) { flat = h('div.shots-grid.gal-grid'); body.append(flat); }
      flat.append(...items.map((shot, i) => thumb(shot, base + i)));
    }
  }

  function updateMore() {
    const left = remaining(list.length, total);
    more.hidden = left === 0;
    more.disabled = loading || !!paging;
    more.textContent = `Show more (${left} left)`;
    // only the last game can go on in the next page, so only its count may be short
    groups.forEach((g, i) => { g.nEl.textContent = runLabel(g.n, i === groups.length - 1 && left > 0); });
  }

  /** Redraw everything: after a load (filters changed, or a refresh). */
  function draw() {
    notice.hidden = available;
    body.replaceChildren();
    groups = [];
    flat = null;
    if (!list.length) {
      count.textContent = '';
      body.replaceChildren(h('div.empty', {}, emptyText(f)));
      more.hidden = true;
      return;
    }
    count.textContent = shotsLabel(total);
    addItems(list, 0);
    updateMore();
  }

  /** Load the first page of the current filters; `keep` asks for as many pictures as are on screen (a refresh). */
  async function load(keep = false) {
    const mine = ++ticket;
    loading = true;
    updateMore();
    const limit = keep ? Math.min(MAX_RELOAD, Math.max(PAGE, list.length)) : PAGE;
    let r;
    try { r = await listAllShots(galleryQuery({ ...f, offset: 0, limit })); } catch {
      if (!alive || mine !== ticket) return;
      loading = false;
      if (keep && list.length) { toast('Could not refresh the screenshots'); } // a refresh that fails keeps what is on screen
      else body.replaceChildren(h('div.empty', {}, 'Could not load the screenshots. ', h('button.btn', { type: 'button', onclick: () => load() }, 'Retry')));
      updateMore();
      if (!(keep && list.length)) more.hidden = true;
      return;
    }
    if (!alive || mine !== ticket) return;
    loading = false;
    list = r.screenshots;
    total = r.total;
    available = r.available;
    const keepYear = f.year;
    year.replaceChildren(h('option', { value: '' }, 'Any year'), ...r.years.map((y) => h('option', { value: String(y) }, String(y))));
    year.value = keepYear;
    if (year.value !== keepYear) { f.year = ''; year.value = ''; load(); return; } // the year has no pictures any more
    draw();
  }

  /** Append the next page. One at a time, and never while a first-page load is under way (its offset would be the old list's). */
  function loadMore() {
    if (loading) return Promise.resolve();
    if (paging) return paging;
    const mine = ticket;
    const base = list.length;
    paging = (async () => {
      try {
        const r = await listAllShots(galleryQuery({ ...f, offset: base }));
        if (!alive || mine !== ticket || list.length !== base) return;
        const seen = new Set(list.map((s) => s.id));
        const fresh = r.screenshots.filter((s) => !seen.has(s.id));
        list = list.concat(fresh);
        total = r.total;
        count.textContent = shotsLabel(total);
        addItems(fresh, base);
      } finally {
        paging = null;
        if (alive) updateMore();
      }
    })();
    updateMore();
    return paging;
  }

  async function step(cur, dir) {
    const r = stepAcross(cur, list.length, total, dir);
    if (!r.more) return r.index;
    try { await loadMore(); } catch { return cur; }
    return stepAcross(cur, list.length, total, 1).index;
  }

  function openAt(i) {
    viewer?.close();
    const title = h('p.shot-game');
    const cap = h('p.shot-cap');
    const orig = h('a.btn', { title: 'Open the original in a new tab', target: '_blank', rel: 'noopener' }, 'Original');
    const gameBtn = h('button.btn', { type: 'button', onclick: () => {
      const id = list[viewer.index()]?.game_id;
      viewer.close().then(() => { const g = store.byId.get(id); if (g) openDrawer(g); });
    } }, 'Open game');
    viewer = openShotViewer({
      count: () => total,
      get: (k) => list[k],
      alt: (s) => `${s.game_title} screenshot`,
      step,
      barActions: orig,
      foot: (meta) => [title, cap, meta, h('div.shot-move', {}, gameBtn)],
      onShow: (s) => {
        title.textContent = s.game_title;
        cap.textContent = s.caption || '';
        cap.hidden = !s.caption;
        orig.href = s.src;
      },
      onClose: (cur) => { viewer = null; body.querySelector(`[data-i="${cur}"]`)?.focus?.({ preventScroll: true }); },
    }, i);
  }

  input.addEventListener('input', debounce(() => { f.q = input.value; load(); }, 250));
  year.addEventListener('change', () => { f.year = year.value; load(); });
  sort.addEventListener('change', () => {
    f.sort = sort.value;
    try { localStorage.setItem(SORT_KEY, f.sort); } catch { /* private mode */ }
    load();
  });
  // Pictures, captions or a game's name changed from a drawer opened here (or elsewhere): refresh what is on screen.
  const refresh = debounce(() => { if (alive) load(true); }, 300);
  window.addEventListener('screenshots-changed', refresh);
  window.addEventListener('games-changed', refresh);
  load();
  return () => {
    alive = false;
    window.removeEventListener('screenshots-changed', refresh);
    window.removeEventListener('games-changed', refresh);
    viewer?.close();
  };
}
