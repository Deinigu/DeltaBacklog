// "Needs attention": games that need a fix, in collapsible groups, each row with a one-tap fix. The grouping lives in
// attention.js (pure); this file builds the DOM and opens the right screen. Nothing here reads the network on its own:
// the list comes from the games already loaded, plus the IGDB review queue and the ignore list (both local API calls).
import { h, toast, toastAction, toastError, emptyBox, brand } from './util.js';
import { store, linkQueue, linkAccept, linkNone, loadLinkPending, attentionIgnored, attentionIgnore, ttbSyncStatus, ttbSyncStart, ttbSyncCancel, refreshGames } from './api.js';
import { pickCandidate, openCoverPicker, openPosterPicker } from './add.js';
import { openDrawer } from './drawer.js';
import { relinkPrompt } from './relinkprompt.js';
import { buildGroups, totalOpen, ignoredTotal, syncLine, groupOpen, withGroupOpen } from './attention.js';
import { moreButton } from './actionsheet.js';

/** What each group's main button says and does. `run(game, row, ctx)` may return a promise. */
const FIXES = {
  link: { label: 'Link…', run: (g, row, ctx) => ctx.linkByHand(g) },
  confirm: { label: (row) => (row.candidate ? 'Confirm' : 'Link…'), run: (g, row, ctx) => (row.candidate ? ctx.accept(g, row.candidate) : ctx.linkByHand(g)) },
  cover: { label: 'Pick cover', run: (g) => openCoverPicker(g) },
  poster: { label: 'Pick poster', run: (g) => openPosterPicker(g) },
  ttb: { label: 'Find time', run: (g) => openDrawer(g, { ttb: true }) },
  platforms: { label: 'Set platform', run: (g) => openDrawer(g, { edit: 'Platforms' }) },
  year: { label: 'Set year', run: (g) => openDrawer(g, { edit: 'Released' }) },
  ownership: { label: 'Set ownership', run: (g) => openDrawer(g, { edit: 'Ownership' }) },
  genres: { label: 'Set genres', run: (g) => openDrawer(g, { edit: 'Genres' }) },
};

// Which groups the person opened or closed, kept for the session (the page is rebuilt every time it is shown).
const STATE_KEY = 'att-open';
let openState = (() => { try { return JSON.parse(sessionStorage.getItem(STATE_KEY)) || {}; } catch { return {}; } })();
const saveState = () => { try { sessionStorage.setItem(STATE_KEY, JSON.stringify(openState)); } catch { /* private window: kept in memory only */ } };

export function renderAttention(root) {
  let queue = null; // null while loading
  let ignored = {};
  let showIgnored = false;
  let alive = true;
  let sync = null; // the bulk HowLongToBeat run: {running, done, total, found, manual, report, failed}
  let poll = 0;

  const body = h('div.att-body');
  root.replaceChildren(h('div.manage.att-page', {}, h('h1', {}, 'Needs attention'), body));

  async function load() {
    const [q, ig] = await Promise.all([linkQueue().catch(() => []), attentionIgnored().catch(() => ({}))]);
    queue = q;
    ignored = ig;
    sync = await ttbSyncStatus().catch(() => null);
    if (sync?.running) watch();
    if (alive) draw();
  }

  // The run lives on the server; ask how it is doing once a second while it runs, then reload the games it changed.
  function watch() {
    clearInterval(poll);
    poll = setInterval(async () => {
      const st = await ttbSyncStatus().catch(() => null);
      if (!alive || !st) return;
      const was = sync?.running;
      sync = st;
      if (!st.running) { clearInterval(poll); if (was) await refreshGames().catch(() => {}); }
      draw();
    }, 1000);
  }

  async function startSync() {
    try { sync = await ttbSyncStart(); } catch { toastError('start the lookup'); return; }
    watch();
    draw();
  }
  async function stopSync() {
    try { sync = await ttbSyncCancel(); } catch { toastError('stop the lookup'); }
    draw();
  }

  function syncBar(group) {
    const open = group.rows.filter((r) => !r.ignored).length;
    if (!open && !sync?.report) return null;
    const line = syncLine(sync);
    return h('div.att-sync', {},
      sync?.running
        ? h('button.btn', { type: 'button', onclick: stopSync }, 'Stop')
        : open ? h('button.btn.primary', { type: 'button', onclick: startSync }, 'Find times for all') : null,
      line && h('span.att-sync-line', { role: 'status', class: sync?.failed ? 'bad' : '' }, line),
      sync?.running ? h('progress', { max: String(sync.total || 1), value: String(sync.done), 'aria-label': 'Progress' }) : null);
  }

  const ctx = {
    async accept(game, cand) {
      try {
        await linkAccept(game.id, cand.igdb_id);
        queue = queue.filter((q) => q.game_id !== game.id);
        loadLinkPending(); // the navigation's "Link IGDB" count
        toast(`Linked “${game.title}”`);
        relinkPrompt(game, cand.name);
      } catch (e) {
        if (e.status === 409) toast(e.message); else toastError('link the game');
      }
      draw();
    },
    async notOnIGDB(game) {
      try {
        await linkNone(game.id);
        game.igdb_not_found = true;
        queue = queue.filter((q) => q.game_id !== game.id);
        loadLinkPending();
        toast(`“${game.title}” marked as not on IGDB`);
      } catch (e) {
        if (e.status === 409) toast('That game is linked to IGDB already'); else toastError('save that');
      }
      draw();
    },
    async linkByHand(game) {
      const c = await pickCandidate({ query: game.title, excludeId: game.id });
      if (c) await ctx.accept(game, c);
    },
  };

  async function setIgnored(group, game, on) {
    try {
      ignored = await attentionIgnore(group, game.id, on);
    } catch { toastError('save that'); return; }
    draw();
    if (on) toastAction(`Ignoring “${game.title}” here`, 'Undo', () => setIgnored(group, game, false));
  }

  function rowEl(group, row) {
    const g = row.game;
    const fix = FIXES[group.fix];
    const label = typeof fix.label === 'function' ? fix.label(row) : fix.label;
    const st = store.statusByCode.get(g.status);
    const sub = [row.candidate && `IGDB suggests “${row.candidate.name}”${row.candidate.year ? ` (${row.candidate.year})` : ''} · ${row.candidate.score}%`,
      row.note || (group.key === 'no_ttb' && sync?.manual?.includes(g.id) ? 'Needs a manual pick' : ''), st && st.label, g.platforms.join(', '), g.release_year].filter(Boolean).join(' · ');
    const main = row.ignored
      ? h('button.btn', { type: 'button', onclick: () => setIgnored(group.key, g, false) }, 'Stop ignoring')
      : h('button.btn.primary', { type: 'button', onclick: () => fix.run(g, row, ctx) }, label);
    // Secondary actions live behind "⋯" (an action sheet), so a long group is one button per row.
    const more = [];
    if (!row.ignored) {
      if (group.fix === 'confirm' && row.candidate) more.push({ label: 'Change…', run: () => ctx.linkByHand(g) });
      if (group.fix === 'link' || group.fix === 'confirm') more.push({ label: h('span', {}, 'Not on ', brand('IGDB')), run: () => ctx.notOnIGDB(g) });
      more.push({ label: 'Ignore in this list', run: () => setIgnored(group.key, g, true) });
    }
    return h('li.att-row' + (row.ignored ? '.is-ignored' : ''), {},
      h('button.att-name', { type: 'button', onclick: () => openDrawer(g) }, h('span.att-title', {}, g.title), sub && h('span.att-sub', {}, sub)),
      h('div.att-actions', {}, main, more.length ? moreButton(`More actions for “${g.title}”`, () => ({ title: g.title, lines: [sub], actions: more })) : null));
  }

  function groupEl(group) {
    const n = group.rows.filter((r) => !r.ignored).length;
    const d = h('details.att-group', { open: groupOpen(openState, group.key, n) },
      h('summary', {}, h('span.att-gtitle', {}, group.title), h('span.att-count', {}, String(n)),
        group.ignoredCount && !showIgnored ? h('span.att-ign-n', {}, `${group.ignoredCount} ignored`) : null),
      group.key === 'no_ttb' ? syncBar(group) : null,
      group.hint ? h('p.hint', {}, group.hint, group.key === 'uncertain' ? [' ', h('a', { href: '#/link' }, 'Review them one by one')] : null) : null,
      h('ul.att-list', {}, group.rows.map((r) => rowEl(group, r))));
    d.addEventListener('toggle', () => { if (d.open !== groupOpen(openState, group.key, n)) { openState = withGroupOpen(openState, group.key, d.open); saveState(); } });
    return d;
  }

  function draw() {
    if (queue === null) { body.replaceChildren(h('p.hint', {}, 'Loading…')); return; }
    const groups = buildGroups(store.games, queue, ignored, showIgnored);
    const nIgnored = ignoredTotal(store.games, queue, ignored);
    const left = totalOpen(groups);
    const top = [];
    if (!left && !nIgnored) {
      body.replaceChildren(emptyBox('Nothing needs attention. Every game is linked and has its cover, poster and basic details.', h('a.btn', { href: '#/library' }, 'Back to the Library')));
      return;
    }
    top.push(h('p.hint', {}, left ? `${left} ${left === 1 ? 'thing' : 'things'} to fix, in ${groups.filter((g) => g.rows.some((r) => !r.ignored)).length} lists.` : 'Everything is fixed.'));
    if (nIgnored) {
      top.push(h('button.pill', { type: 'button', 'aria-pressed': String(showIgnored), onclick: () => { showIgnored = !showIgnored; draw(); } },
        showIgnored ? 'Hide ignored' : `Show ${nIgnored} ignored`));
    }
    body.replaceChildren(...top, ...groups.map(groupEl));
  }

  let raf = 0;
  const onGames = () => { if (!raf) raf = requestAnimationFrame(() => { raf = 0; if (alive && queue) draw(); }); };
  window.addEventListener('games-changed', onGames);
  window.addEventListener('games-reloaded', onGames);
  draw();
  load();
  return () => { alive = false; clearInterval(poll); window.removeEventListener('games-changed', onGames); window.removeEventListener('games-reloaded', onGames); };
}
