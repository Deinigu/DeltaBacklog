// Steam page: the games of the Steam library, read on demand (never on a page load). "Not in library" is the review list:
// each game can be added (the usual add form, for PC), matched to a game the library already has, or ignored. "Matched"
// lists the games the hours come from, with a way to fix a wrong match. Nothing is added automatically.
import { h, icon, gradientFor, initials, toast, toastAction, debounce } from './util.js';
import { store, steamList, steamSync, steamSetMatch, steamIgnore, refreshHours } from './api.js';
import { openDrawer } from './drawer.js';
import { openAdd, makeSheet } from './add.js';
import { TABS, FILTERS, buildSteam, matchCandidates, playedLabel, lastPlayedLabel, sourceLabel, syncNote } from './steam.js';

const PAGE = 100; // rows drawn at first and per "Show more": a big Steam library is thousands of apps

// The tab, filter and search survive the page being redrawn (adding a game reloads the whole library view).
let tab = 'new';
let filter = 'all';
let query = '';

const tile = (name) => h('span.pst', {}, h('span.ph', { style: `background:${gradientFor(name)}` }, initials(name)));

export function renderSteam(root) {
  let info = { configured: false, synced_at: '', apps: [], refresh: { running: false } };
  let shown = PAGE;
  let alive = true;
  let timer = null;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.steam-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  async function load() {
    try { info = await steamList(); } catch { /* offline or an old server: the page says nothing is cached */ }
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
        toast(r.failed ? `Steam sync: ${r.report || 'failed'}` : `Steam synced. ${r.report || ''}`, 6000);
        say(r.failed ? 'The Steam sync stopped.' : 'Steam synced.');
        if (!r.failed) refreshHours().catch(() => { /* the next reload shows them */ });
      }
    }, 1500);
  }
  async function sync() {
    try {
      info.refresh = await steamSync();
      say('Reading your Steam library.');
    } catch (e) {
      toast(e.message || 'Could not start the sync');
      return;
    }
    draw();
    schedulePoll();
  }

  // ---- the owner's choices; every endpoint answers with the whole list ----
  async function act(call, done, undo) {
    try {
      info = await call();
    } catch (e) {
      toast(e.message || 'That did not work');
      return;
    }
    draw();
    refreshHours().catch(() => {});
    say(done);
    if (undo) toastAction(done, 'Undo', undo); else toast(done, 3000);
  }
  const ignore = (app, on) => act(() => steamIgnore(app.app_id, on), on ? `Ignored “${app.name}”.` : `“${app.name}” is back in the list.`,
    on ? () => ignore(app, false) : null);
  const matchTo = (app, game) => act(() => steamSetMatch(app.app_id, { game_id: game.id }), `“${app.name}” matched with “${game.title}”.`);
  const noGame = (app) => act(() => steamSetMatch(app.app_id, { none: true }), `“${app.name}” is not in your library.`, () => steamSetMatch(app.app_id, { reset: true }).then((r) => { info = r; draw(); refreshHours().catch(() => {}); }));
  const automatic = (app) => act(() => steamSetMatch(app.app_id, { reset: true }), `“${app.name}” is matched automatically again.`);

  function add(app) {
    openAdd({
      query: app.name,
      platforms: ['PC'],
      onAdded: async (game) => {
        try {
          info = await steamSetMatch(app.app_id, { game_id: game.id });
          await refreshHours();
        } catch (e) {
          toast(e.message || 'The game was added, but the Steam match was not saved. Match it from the Not in library list.', 6000);
        }
        window.dispatchEvent(new Event('steam-changed')); // the page may have been redrawn while the game was added
      },
    });
  }

  function pickGame(app) {
    const ui = makeSheet(`Match “${app.name}”`, 'steam-match');
    const input = h('input', { type: 'search', placeholder: 'Search your library…', autocomplete: 'off', 'aria-label': 'Search your library', value: app.name });
    const results = h('div.cands');
    const run = () => {
      const list = matchCandidates(store.games, input.value);
      if (!input.value.trim()) results.replaceChildren(h('p.hint', {}, 'Type a game title.'));
      else if (!list.length) results.replaceChildren(h('p.hint', {}, 'No game of your library has that title.'));
      else {
        results.replaceChildren(...list.map((g) => h('button.cand', { type: 'button', onclick: () => { ui.close(); matchTo(app, g); } },
          h('div.cinfo', {}, h('div.cname', {}, g.title, g.release_year ? h('span.year', {}, ` (${g.release_year})`) : null),
            h('div.csub', {}, store.statusByCode.get(g.status)?.label || g.status)))));
      }
    };
    input.addEventListener('input', debounce(run, 150));
    ui.body.replaceChildren(h('div.search.pick-search', {}, input), results);
    input.focus({ preventScroll: true });
    input.select();
    run();
  }

  // ---- rows ----
  function newRow({ app }, ignored) {
    const last = lastPlayedLabel(app.last_played);
    const body = h('span.up-body', {}, h('span.up-title', {}, app.name), h('span.up-date', {}, playedLabel(app.playtime_min)), last ? h('span.up-from', {}, last) : null);
    const actions = ignored
      ? [h('button.hide-chip', { type: 'button', 'aria-label': `Show ${app.name} again`, onclick: () => ignore(app, false) }, 'Show again')]
      : [h('button.add-chip', { type: 'button', 'aria-label': `Add ${app.name} to your library`, onclick: () => add(app) }, 'Add'),
        h('button.hide-chip', { type: 'button', 'aria-label': `Match ${app.name} with a game of your library`, onclick: () => pickGame(app) }, 'Match'),
        h('button.hide-chip', { type: 'button', 'aria-label': `Ignore ${app.name}`, onclick: () => ignore(app, true) }, 'Ignore')];
    return h('li.up-row', {}, h('div.up-open', {}, tile(app.name), body), h('span.si-actions', {}, ...actions));
  }
  function matchedRow({ app, game }) {
    const same = game.title.trim().toLowerCase() === app.name.trim().toLowerCase();
    const how = sourceLabel(app.source);
    const body = h('span.up-body', {}, h('span.up-title', {}, game.title),
      same ? null : h('span.up-date', {}, `Steam: ${app.name}`),
      h('span.up-from', {}, `${playedLabel(app.playtime_min)}${how ? ` · matched ${how}` : ''}`));
    const aria = `${game.title}, ${playedLabel(app.playtime_min)}${how ? `, matched ${how}` : ''}`;
    return h('li.up-row', {},
      h('button.up-open', { type: 'button', onclick: () => openDrawer(game), 'aria-label': aria }, tile(game.title), body),
      h('span.si-actions', {},
        h('button.hide-chip', { type: 'button', 'aria-label': `Match ${app.name} with another game`, onclick: () => pickGame(app) }, 'Change'),
        h('button.hide-chip', { type: 'button', 'aria-label': `${app.name} is not ${game.title}`, onclick: () => noGame(app) }, 'Not this game'),
        app.source === 'user' ? h('button.hide-chip', { type: 'button', 'aria-label': `Match ${app.name} automatically`, onclick: () => automatic(app) }, 'Automatic') : null));
  }

  // ---- the page ----
  function syncBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: sync,
      title: info.configured ? '' : 'Steam is not set up (STEAM_API_KEY and STEAM_ID are missing from .env)' },
    r.running ? 'Reading Steam…' : '↻ Sync with Steam');
    const failed = !r.running && r.failed && r.report ? h('p.hint.steam-error', { role: 'alert' }, r.report) : null;
    return [btn, h('p.hint', {}, syncNote(info)), failed];
  }

  // The rows alone are redrawn while typing in the search box, so the box keeps its focus.
  const listBox = h('div.steam-list');
  function drawList() {
    const { rows } = buildSteam(store.games, info, { tab, filter, query });
    let empty = null;
    if (!rows.length) {
      if (!info.apps.length) empty = info.configured ? 'Nothing was read yet. Press Sync with Steam.' : 'Steam is not set up.';
      else if (query.trim()) empty = 'No Steam game has that name.';
      else empty = { new: 'Every Steam game is in your library or ignored.', matched: 'No Steam game is matched to your library yet.', ignored: 'You have not ignored any game.' }[tab];
    }
    listBox.replaceChildren(...[
      rows.length ? h('ul.up-list', {}, rows.slice(0, shown).map((r) => (tab === 'matched' ? matchedRow(r) : newRow(r, tab === 'ignored')))) : null,
      rows.length > shown ? h('button.btn.steam-more', { type: 'button', onclick: () => { shown += PAGE; drawList(); } }, `Show more (${rows.length - shown} left)`) : null,
      empty ? h('div.empty', {}, empty) : null,
    ].filter(Boolean));
  }

  function draw() {
    const { counts } = buildSteam(store.games, info, { tab, filter, query });
    const [btn, note, failed] = syncBar();
    const tabs = h('div.chips', { role: 'group', 'aria-label': 'Steam games' }, TABS.map((t) =>
      h('button.chip', { type: 'button', 'aria-pressed': String(tab === t.key), onclick: () => { tab = t.key; shown = PAGE; draw(); } }, t.label, ' ', h('span.c', {}, String(counts[t.key])))));
    const filters = tab === 'new' ? h('div.chips', { role: 'group', 'aria-label': 'Hours filter' }, FILTERS.map((f) => {
      const n = f.key === 'all' ? counts.new : counts[f.key];
      return h('button.chip', { type: 'button', 'aria-pressed': String(filter === f.key), onclick: () => { filter = f.key; shown = PAGE; draw(); } }, f.label, ' ', h('span.c', {}, String(n)));
    })) : null;
    const search = h('div.search.steam-search', {}, icon('search'), h('input', { type: 'search', value: query, placeholder: 'Search…', autocomplete: 'off', 'aria-label': 'Search Steam games',
      oninput: debounce((e) => { query = e.target.value; shown = PAGE; drawList(); }, 250) }));
    const any = info.apps.length > 0;
    page.replaceChildren(...[
      h('h1', {}, 'Steam'),
      h('p.hint', {}, 'Your Steam library and the hours you played. Games you do not have here yet can be added, matched with one you have, or ignored. Nothing is added on its own.'),
      h('div.chips', {}, btn),
      note, failed,
      any ? tabs : null,
      any && filters ? filters : null,
      any ? search : null,
      listBox,
    ].filter(Boolean));
    drawList();
  }

  const onGames = () => draw();
  window.addEventListener('games-changed', onGames);
  window.addEventListener('steam-changed', load);
  draw();
  load();
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); window.removeEventListener('steam-changed', load); };
}
