// Year in review page (#/years/<year>): what was played, a breakdown, and that year's tier list.
import { h, toast, gradientFor, initials, lazyImg } from './util.js';
import { store, coverURL, getTierlist, saveTierlist } from './api.js';
import { openDrawer } from './drawer.js';
import { groupYear, gamesOfYear, breakdown, yearCounts, neighbours, tierState, tierOf, unranked, place, toEntries } from './yearreview.js';
import { exportTierlistPNG } from './tierpng.js';

const thisYear = () => new Date().getFullYear();

/** Latest year with games (or this year when nothing has been played yet): where "#/years" lands. */
export function defaultYear() {
  const counts = yearCounts(store.games);
  return counts.length ? counts[0].year : thisYear();
}

export function renderYear(root, year) {
  let alive = true;
  const counts = yearCounts(store.games);
  const nb = neighbours(counts, year, thisYear());
  const groups = groupYear(store.games, year);
  const pool = gamesOfYear(store.games, year);
  const poolIds = pool.map((g) => g.id);
  const total = pool.length;

  const nav = (to, label, glyph) => to
    ? h('a.btn.year-step', { href: `#/years/${to}`, 'aria-label': `${label}: ${to}` }, glyph)
    : h('span.btn.year-step.off', { 'aria-hidden': 'true' }, glyph);
  const stripYears = counts.some((c) => c.year === year) ? counts : [{ year, count: 0 }, ...counts].sort((a, b) => b.year - a.year);
  const strip = h('nav.year-strip', { 'aria-label': 'Years' },
    ...stripYears.map((c) => h('a.chip', { href: `#/years/${c.year}`, 'aria-current': c.year === year ? 'page' : null }, String(c.year), h('span.c', {}, c.count))));

  const head = h('header.year-head', {},
    nav(nb.prev, 'Previous year', '‹'),
    h('div.year-title', {}, h('h1', {}, String(year)), h('p.hint', {}, total ? `${total} game${total === 1 ? '' : 's'} played` : 'No games played')),
    nav(nb.next, 'Next year', '›'));

  const body = h('div');
  root.replaceChildren(h('div.year-page', {}, head, strip, body));

  if (!total) {
    body.append(h('div.empty', {}, `Nothing is recorded for ${year}.`, h('br'), 'A game counts for a year when it has that year in its “Years played” (moving a game to Playing or Finished adds the current year).'));
    return () => { alive = false; };
  }

  // ---- summary ----
  const tile = (g) => h('div.stat', {}, h('b', {}, String(g.games.length)), h('span', {}, g.label));
  const bars = (title, rows) => {
    const top = rows.slice(0, 6);
    const max = top[0]?.count || 1;
    return h('section.card-box', {}, h('h2', {}, title),
      top.length ? h('ul.bars', {}, ...top.map((r) => h('li', {}, h('span.bar-name', {}, r.name), h('span.bar', {}, h('i', { style: `width:${Math.max(6, (r.count / max) * 100)}%` })), h('span.bar-n', {}, String(r.count))))) : h('p.hint', {}, 'None recorded'));
  };
  body.append(
    h('div.stats', {}, ...groups.map(tile)),
    h('div.two', {}, bars('Platforms', breakdown(pool, (g) => g.platforms)), bars('Genres', breakdown(pool, (g) => g.genres))));

  // ---- tier list ----
  const tierBox = h('section.card-box.tiers-box');
  body.append(tierBox);
  let state = null;
  let menu = null;
  let loadFailed = false;

  const tileEl = (g) => {
    const url = coverURL(g);
    const el = h('div.ttile', { role: 'button', tabindex: '0', 'data-id': g.id, title: g.title, 'aria-label': `${g.title}: choose a tier` },
      url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)),
      h('span.tname', {}, g.title));
    el.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openMenu(g); } });
    el.addEventListener('click', () => openMenu(g));
    attachDrag(el, g);
    return el;
  };
  const byId = new Map(pool.map((g) => [g.id, g]));

  function drawTiers() {
    if (loadFailed) {
      tierBox.replaceChildren(h('h2', {}, 'Tier list'), h('div.empty', {}, 'Could not load the tier list. ', h('button.btn', { type: 'button', onclick: loadTiers }, 'Retry')));
      return;
    }
    if (!state) { tierBox.replaceChildren(h('h2', {}, 'Tier list'), h('p.hint', {}, 'Loading…')); return; }
    const rows = state.tiers.map((t) => h('div.trow', {},
      h('div.tlabel', { style: `background:${t.color}` }, t.name),
      h('div.tcell', { 'data-tier': t.id, role: 'group', 'aria-label': `Tier ${t.name}` }, ...state.rows[t.id].map((id) => tileEl(byId.get(id))))));
    const free = unranked(state, poolIds);
    tierBox.replaceChildren(
      h('div.tiers-head', {}, h('h2', {}, 'Tier list'),
        h('button.btn', { type: 'button', onclick: () => exportTierlistPNG({ year, state, byId }).catch(() => toast('Could not make the image')) }, 'Export PNG')),
      h('p.hint', {}, 'Drag a game into a tier (touch: press and hold), or tap it to choose a tier. Saved automatically.'),
      h('div.tiers', {}, ...rows),
      h('h3', {}, `Not ranked (${free.length})`),
      h('div.tcell.pool', { 'data-tier': '', role: 'group', 'aria-label': 'Not ranked' }, ...free.map((id) => tileEl(byId.get(id)))));
  }

  // ---- saving: debounced, one request at a time, always sends the latest state ----
  let timer = null, saving = false, dirty = false;
  function changed(next) {
    state = next;
    drawTiers();
    dirty = true;
    clearTimeout(timer);
    timer = setTimeout(flush, 400);
  }
  async function flush() {
    if (saving || !dirty) return;
    saving = true; dirty = false;
    try { await saveTierlist(year, toEntries(state)); } catch { dirty = true; toast('Could not save the tier list. Will retry.'); setTimeout(flush, 4000); }
    saving = false;
    if (dirty && !timer) flush();
  }
  const onHide = () => { if (dirty && !saving) { clearTimeout(timer); saveTierlistBeacon(); } };
  function saveTierlistBeacon() {
    fetch(`/api/tierlist/${year}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ entries: toEntries(state) }), keepalive: true }).catch(() => {});
    dirty = false;
  }
  window.addEventListener('pagehide', onHide);

  async function loadTiers() {
    loadFailed = false; state = null; drawTiers();
    try {
      const tl = await getTierlist(year);
      if (!alive) return;
      state = tierState(tl.tiers, tl.entries, poolIds);
    } catch { if (!alive) return; loadFailed = true; }
    drawTiers();
  }

  // ---- the tap menu: the touch-friendly alternative to dragging ----
  function closeMenu() { menu?.remove(); menu = null; }
  function openMenu(g) {
    closeMenu();
    const cur = tierOf(state, g.id);
    const choose = (tierId) => { closeMenu(); if (tierId !== cur) changed(place(state, g.id, tierId)); };
    const scrim = h('div.scrim', { onclick: closeMenu });
    const sheet = h('div.tier-menu', { role: 'dialog', 'aria-label': `Move ${g.title}` },
      h('h3', {}, g.title),
      h('div.tier-choices', {}, ...state.tiers.map((t) => h('button.tier-choice', { type: 'button', style: `background:${t.color}`, 'aria-pressed': String(t.id === cur), onclick: () => choose(t.id) }, t.name)),
        h('button.btn.tier-none', { type: 'button', 'aria-pressed': String(cur == null), onclick: () => choose(null) }, 'Not ranked')),
      h('div.dialog-actions', {}, h('button.btn', { type: 'button', onclick: () => { closeMenu(); openDrawer(g); } }, 'Details'), h('button.btn', { type: 'button', onclick: closeMenu }, 'Close')));
    menu = h('div.tier-menu-wrap', {}, scrim, sheet);
    document.body.append(menu);
    sheet.querySelector('button')?.focus();
  }
  const onKey = (e) => { if (e.key === 'Escape' && menu) closeMenu(); };
  document.addEventListener('keydown', onKey);

  // ---- dragging (pointer events: mouse drags at once, touch after a long press) ----
  function attachDrag(el, g) {
    el.addEventListener('pointerdown', (e) => {
      if (e.button > 0) return;
      const sx = e.clientX, sy = e.clientY, touch = e.pointerType !== 'mouse';
      let dragging = false, ghost = null, hot = null, pressTimer = null;
      const onTouchMove = (ev) => { if (dragging) ev.preventDefault(); };
      const start = () => {
        dragging = true;
        clearTimeout(pressTimer);
        el.classList.add('dragging');
        ghost = el.cloneNode(true);
        ghost.classList.add('ghost');
        ghost.style.width = `${el.offsetWidth}px`;
        document.body.append(ghost);
        navigator.vibrate?.(15);
      };
      let px = 0, py = 0, raf = 0;
      // Near the top or bottom edge the page scrolls by itself, so a far-away tier can be reached mid-drag.
      const autoScroll = () => {
        raf = 0;
        if (!dragging) return;
        const edge = 70, dy = py < edge ? -(edge - py) / 4 : py > innerHeight - edge ? (py - (innerHeight - edge)) / 4 : 0;
        if (dy) { window.scrollBy(0, dy); move(px, py); }
      };
      const move = (x, y) => {
        px = x; py = y;
        if (!raf) raf = requestAnimationFrame(autoScroll);
        ghost.style.transform = `translate(${x - el.offsetWidth / 2}px, ${y - el.offsetHeight / 2}px)`;
        const cell = document.elementFromPoint(x, y)?.closest('.tcell');
        if (cell !== hot) { hot?.classList.remove('hot'); hot = cell; hot?.classList.add('hot'); }
      };
      const finish = (ev, cancelled) => {
        clearTimeout(pressTimer);
        cancelAnimationFrame(raf);
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('pointerup', onUp);
        window.removeEventListener('pointercancel', onCancel);
        document.removeEventListener('touchmove', onTouchMove);
        if (!dragging) return; // a plain tap is handled by the click listener below
        ghost.remove();
        el.classList.remove('dragging');
        hot?.classList.remove('hot');
        el.addEventListener('click', (c) => c.stopImmediatePropagation(), { once: true, capture: true });
        if (!cancelled && hot) {
          const tierId = hot.dataset.tier === '' ? null : Number(hot.dataset.tier);
          const others = [...hot.querySelectorAll('.ttile')].filter((t) => t !== el);
          let index = 0;
          for (const t of others) {
            const r = t.getBoundingClientRect();
            if (r.bottom < ev.clientY || (ev.clientY <= r.bottom && ev.clientY >= r.top && r.left + r.width / 2 < ev.clientX)) index++;
          }
          if (tierId !== tierOf(state, g.id) || index !== state.rows[tierId]?.indexOf(g.id)) changed(place(state, g.id, tierId, index));
        }
      };
      const onMove = (ev) => {
        if (!dragging) {
          if (Math.hypot(ev.clientX - sx, ev.clientY - sy) > (touch ? 10 : 5)) {
            if (touch) { finish(null, true); return; } // the finger scrolls the page instead
            start();
          } else return;
        }
        ev.preventDefault();
        move(ev.clientX, ev.clientY);
      };
      const onUp = (ev) => finish(ev, false);
      const onCancel = (ev) => finish(ev, true);
      window.addEventListener('pointermove', onMove);
      window.addEventListener('pointerup', onUp);
      window.addEventListener('pointercancel', onCancel);
      if (touch) {
        document.addEventListener('touchmove', onTouchMove, { passive: false });
        pressTimer = setTimeout(() => { start(); move(sx, sy); }, 380);
      }
    });
    el.addEventListener('contextmenu', (e) => { if (e.pointerType === 'touch' || el.classList.contains('dragging')) e.preventDefault(); });
  }

  // ---- what was played, by outcome ----
  const gameRow = (g) => {
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    return h('button.yrow', { type: 'button', onclick: () => openDrawer(g), style: st ? `--st:${st.color}` : null },
      h('span.ythumb', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title))),
      h('span.yinfo', {}, h('span.yname', {}, g.title), h('span.ysub', {}, [st?.label, g.platforms.slice(0, 2).join(', ')].filter(Boolean).join(' · '))));
  };
  body.append(h('div.ygroups', {}, ...groups.filter((g) => g.games.length).map((grp) =>
    h('section.card-box', {}, h('h2', {}, grp.label, h('span.c', {}, String(grp.games.length))), h('div.ylist', {}, ...grp.games.map(gameRow))))));

  loadTiers();

  return () => {
    alive = false;
    closeMenu();
    clearTimeout(timer);
    if (dirty && !saving && state) saveTierlistBeacon();
    window.removeEventListener('pagehide', onHide);
    document.removeEventListener('keydown', onKey);
  };
}

