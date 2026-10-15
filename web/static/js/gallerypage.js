// The Screenshots page (#/screenshots): every screenshot of the library, by game or newest first.
import { h, debounce, icon, gradientFor, initials } from './util.js';
import { store, coverURL, listAllShots } from './api.js';
import { openDrawer } from './drawer.js';
import { galleryQuery, groupRuns, remaining, shotsLabel, emptyText } from './gallery.js';
import { openGalleryViewer } from './galleryviewer.js';

const SORT_KEY = 'gallery-sort';
const readSort = () => { try { return localStorage.getItem(SORT_KEY) === 'newest' ? 'newest' : 'game'; } catch { return 'game'; } };

export function renderGallery(root) {
  let alive = true;
  let closeViewer = null;
  let list = [];
  let total = 0;
  let available = true;
  let ticket = 0; // a newer load makes an older answer irrelevant
  let yearsDone = false;
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

  const thumb = ({ shot, index }) => {
    const img = h('img', { src: shot.thumb, alt: shot.caption || `${shot.game_title} screenshot`, loading: 'lazy', decoding: 'async', width: '240', height: '135' });
    const b = h('button.shot-thumb', { type: 'button', 'data-i': String(index), 'aria-label': `Open screenshot ${index + 1} of ${total}: ${shot.game_title}`, onclick: () => openAt(index) }, img);
    img.addEventListener('error', () => b.classList.add('missing'));
    return b;
  };

  function gameHead(run) {
    const g = store.byId.get(run.gameId);
    const url = g && coverURL(g);
    const art = h('span.gal-cover', {}, url ? h('img', { src: url, alt: '', loading: 'lazy', decoding: 'async' }) : h('span.ph', { style: `background:${gradientFor(run.title)}` }, initials(run.title)));
    return h('button.gal-game', { type: 'button', disabled: !g, onclick: () => g && openDrawer(g), 'aria-label': `Open ${run.title}` },
      art, h('span.gal-name', {}, run.title), h('span.gal-n', {}, shotsLabel(run.items.length)));
  }

  function draw() {
    notice.hidden = available;
    if (!list.length) {
      count.textContent = '';
      body.replaceChildren(h('div.empty', {}, available ? emptyText(f) : 'The screenshot storage is not available, so nothing can be listed from it yet.'));
      more.hidden = true;
      return;
    }
    count.textContent = shotsLabel(total);
    if (f.sort === 'game') {
      body.replaceChildren(...groupRuns(list).map((run) => h('section.gal-group', {}, gameHead(run), h('div.shots-grid.gal-grid', {}, run.items.map(thumb)))));
    } else {
      body.replaceChildren(h('div.shots-grid.gal-grid', {}, list.map((shot, index) => thumb({ shot, index }))));
    }
    const left = remaining(list.length, total);
    more.hidden = left === 0;
    more.textContent = `Show more (${left} left)`;
  }

  async function load() {
    const mine = ++ticket;
    let r;
    try { r = await listAllShots(galleryQuery({ ...f, offset: 0 })); } catch {
      if (alive && mine === ticket) body.replaceChildren(h('div.empty', {}, 'Could not load the screenshots. ', h('button.btn', { type: 'button', onclick: load }, 'Retry')));
      return;
    }
    if (!alive || mine !== ticket) return;
    list = r.screenshots;
    total = r.total;
    available = r.available;
    if (!yearsDone) { // the years offered do not depend on the other filters
      yearsDone = true;
      year.replaceChildren(h('option', { value: '' }, 'Any year'), ...r.years.map((y) => h('option', { value: String(y) }, String(y))));
    }
    draw();
  }

  async function loadMore() {
    const mine = ticket;
    const r = await listAllShots(galleryQuery({ ...f, offset: list.length }));
    if (!alive || mine !== ticket) return;
    const seen = new Set(list.map((s) => s.id));
    list = list.concat(r.screenshots.filter((s) => !seen.has(s.id)));
    total = r.total;
    draw();
  }

  function openAt(i) {
    closeViewer?.();
    closeViewer = openGalleryViewer({
      list: () => list,
      total: () => total,
      loadMore,
      openGame: (id) => { const g = store.byId.get(id); if (g) openDrawer(g); },
      onClose: (cur) => { closeViewer = null; body.querySelector(`[data-i="${cur}"]`)?.focus?.({ preventScroll: true }); },
    }, i);
  }

  input.addEventListener('input', debounce(() => { f.q = input.value; load(); }, 250));
  year.addEventListener('change', () => { f.year = year.value; load(); });
  sort.addEventListener('change', () => {
    f.sort = sort.value;
    try { localStorage.setItem(SORT_KEY, f.sort); } catch { /* private mode */ }
    load();
  });
  load();
  return () => { alive = false; closeViewer?.(); };
}
