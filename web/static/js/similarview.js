// Similar page: games IGDB lists as similar to the games the user finished or marked as a favourite, that the library
// does not have, ranked by how many of those games suggest them, with Add and Hide. The list is read from the
// server's cache; nothing here asks IGDB unless the owner presses Refresh from IGDB.
import { h, emptyBox, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { store, similarList, similarRefresh, similarHide, similarUnhide, igdbGame, missingPosterURL } from './api.js';
import { openAdd } from './add.js';
import { similarIndex, setHiddenFlag, becauseText, isSource } from './similar.js';

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
  const page = h('div.manage.series-page.up-page.sim-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

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
      toast(e.message || 'Could not start the refresh');
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
    } catch (e) { toast(e.message || (hide ? 'Could not hide the game' : 'Could not show the game again')); return; }
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
    const body = h('span.up-body', {}, h('span.up-title', {}, s.title), h('span.up-date', {}, meta), h('span.up-from.sim-why', {}, becauseText(s.sourceGames)));
    const addBtn = h('button.add-chip', { type: 'button', 'aria-label': `Add ${s.title} to your library`, onclick: () => add(s, addBtn) }, 'Add');
    return h('li.up-row', {}, h('div.up-open', {}, poster(s), body),
      h('span.si-actions', {}, addBtn, h('button.hide-chip', { type: 'button', 'aria-label': `Hide ${s.title}`, onclick: () => setHidden(s, true) }, 'Hide')));
  }

  function refreshBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      title: info.configured ? '' : 'IGDB is not set up (keys are missing from .env)' },
    r.running ? `Reading IGDB… ${r.total ? `${r.done}/${r.total}` : ''}` : '↻ Refresh from IGDB');
    const note = info.fetched_at ? `Read from IGDB on ${new Date(info.fetched_at).toLocaleDateString('en-GB')}.`
      : info.configured ? 'Press Refresh from IGDB to find games similar to the ones you finished or marked as favourites.' : '';
    return [btn, note];
  }

  function draw() {
    const { items, hidden } = index;
    const [btn, note] = refreshBar();
    const sources = store.games.filter(isSource).length;
    const empty = !items.length;
    let emptyText = 'Nothing to suggest right now.';
    if (!sources) emptyText = 'Finish a game, or mark one as a favourite, and link it to IGDB (game details, Edit, Fill from IGDB). Its similar games show up here.';
    else if (loaded && !info.fetched_at) emptyText = info.configured ? 'Nothing was read from IGDB yet.' : 'IGDB is not set up (keys are missing from .env).';
    const firstRead = empty && sources && loaded && !info.fetched_at && info.configured; // the Refresh button sits in the empty state, once
    page.replaceChildren(...[
      h('h1', {}, 'Similar games'),
      h('p.hint', {}, 'Games IGDB lists as similar to the ones you finished or marked as favourites, that you do not have. The more of your games suggest one, the higher it is.'),
      firstRead ? null : h('div.chips', {}, btn),
      note && !firstRead ? h('p.hint', {}, note) : null,
      items.length ? h('ul.up-list', {}, items.map(row)) : null,
      empty ? emptyBox(emptyText, firstRead && btn) : null,
      items.length ? h('p.hint', {}, `${items.length} ${items.length === 1 ? 'suggestion' : 'suggestions'} from ${sources} of your games.`) : null,
      hidden.length ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${hidden.length})`),
        h('ul', {}, hidden.map((s) => h('li', {}, h('span', {}, s.title, h('small', {}, s.year ? ` (${s.year})` : '')),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${s.title} again`, onclick: () => setHidden(s, false) }, 'Show again'))))) : null,
    ].filter(Boolean));
  }

  function onGames() { index = similarIndex(info, store.games); draw(); }
  draw();
  load();
  window.addEventListener('games-changed', onGames);
  return () => { alive = false; clearTimeout(timer); window.removeEventListener('games-changed', onGames); };
}
