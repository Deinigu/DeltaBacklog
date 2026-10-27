// Quick add: search IGDB by title or store link, pick a match, review the pre-filled form and cover, save.
// Also the IGDB match picker and the cover and poster pickers used by the game drawer and the ranking board.
import { h, icon, toast, norm, debounce, gradientFor, initials, confirmDialog } from './util.js';
import { store, lookup, coverSearch, createGame, setCover, setPoster } from './api.js';
import { openDrawer } from './drawer.js';
import { mapPlatforms, defaultPlatforms } from './platforms.js';
import { unique, field, text, ensureDatalist, listRow, statusSelect, ownershipSelect, yearsRow, notesField, toggleRow } from './form.js';
import { validateForm } from './editpatch.js';

/** A scrim + sheet pair that closes together. Returns {sheet, head, body, foot, back, title, close}. */
export function makeSheet(titleText, extraClass = '') {
  let onClose = null;
  let guard = null; // async () => boolean: false keeps the sheet open (scrim, ✕ and Escape ask it first)
  const opener = document.activeElement;
  const close = () => {
    scrim.remove(); sheet.remove(); document.removeEventListener('keydown', onKey);
    // not into a text field: on iOS that would pop the keyboard back up
    if (opener?.isConnected && typeof opener.focus === 'function' && !opener.matches('input, textarea, select')) opener.focus({ preventScroll: true });
    onClose && onClose();
  };
  const attempt = async () => { if (guard && !(await guard())) return; close(); };
  const scrim = h('div.scrim.show', { onclick: attempt });
  scrim.style.zIndex = 70;
  const back = h('button.btn', { type: 'button', hidden: true, 'aria-label': 'Back' }, '←');
  const title = h('span', {}, titleText);
  const head = h('div.filters-head', {}, h('span.headl', {}, back, title), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: attempt }, '✕'));
  const body = h('div.filters-body');
  const foot = h('div.filters-foot', { hidden: true });
  // One persistent live region: screen readers often skip regions inserted together with their text
  const live = h('div.sr-only', { role: 'status' });
  const sheet = h('div.sheet.open', { role: 'dialog', 'aria-modal': 'true', 'aria-label': titleText, class: extraClass, tabindex: '-1' }, head, body, foot, live);
  sheet.style.zIndex = 71;
  // Escape closes only the sheet on top: a picker opened over this sheet handles its own Escape.
  const onKey = (e) => {
    if (e.key !== 'Escape' || document.querySelector('.dialog')) return;
    const open = document.querySelectorAll('.sheet.open');
    if (open[open.length - 1] === sheet) attempt();
  };
  document.addEventListener('keydown', onKey);
  document.body.append(scrim, sheet);
  sheet.focus({ preventScroll: true });
  /**
   * Run a re-render without losing keyboard focus: a control marked data-fk keeps focus when it comes back
   * with the same key; otherwise the primary footer button, then the sheet itself, takes it.
   */
  const render = (fn) => {
    const key = sheet.contains(document.activeElement) ? document.activeElement.dataset?.fk : null;
    fn();
    const again = key && [...sheet.querySelectorAll('[data-fk]')].find((el) => el.dataset.fk === key && !el.disabled);
    (again || sheet.querySelector('.filters-foot .btn.primary:not(:disabled)') || sheet).focus({ preventScroll: true });
  };
  const say = (text) => { live.textContent = text; };
  return { sheet, head, body, foot, back, title, close, render, say, set onClose(fn) { onClose = fn; }, set guard(fn) { guard = fn; } };
}

/** The game already in the library that a lookup result is: matched by IGDB id first, then by title. */
function inLibrary(c, excludeId) {
  const others = store.games.filter((g) => g.id !== excludeId);
  return others.find((g) => g.igdb_id === c.igdb_id) || others.find((g) => norm(g.title) === norm(c.name));
}

/**
 * The search step: a box for a title or store link and the IGDB matches under it.
 * state: {query} (kept by the caller so going back restores it). handlers: {onPick(candidate), onManual(typedText) | null, excludeId}
 */
function renderSearch(ui, state, { onPick, onManual, excludeId }) {
  const input = h('input', { type: 'search', placeholder: 'Game title…', autocomplete: 'off', enterkeyhint: 'search', 'aria-label': 'Game title', value: state.query });
  const results = h('div.cands');
  const manual = onManual && h('button.btn.manual', { type: 'button', onclick: () => onManual(input.value.trim()) });
  // replaceChildren is the DOM method, which would print a null as the text "null": only pass what exists
  ui.body.replaceChildren(h('div.search.pick-search', {}, icon('search'), input), results, ...(manual ? [manual] : []));

  let seq = 0;
  const run = async () => {
    const q = input.value.trim();
    state.query = q;
    if (manual) manual.textContent = q && !/^https?:/i.test(q) ? `Add “${q}” without a lookup` : 'Add manually';
    if (q.length < 2) { results.replaceChildren(h('p.hint', {}, 'Type a game title.')); return; }
    const mine = ++seq;
    results.replaceChildren(h('p.hint', {}, 'Searching…'));
    try {
      const r = await lookup(q);
      if (mine !== seq) return;
      if (!r.configured) { results.replaceChildren(h('p.hint', {}, 'Lookups are not set up yet (IGDB keys are missing from .env).' + (manual ? ' You can still add the game manually.' : ''))); return; }
      if (!r.results.length) { results.replaceChildren(h('p.hint', {}, 'No matches. Try another spelling' + (manual ? ', or add it manually.' : '.'))); return; }
      results.replaceChildren(...r.results.map((c) => candidate(c)));
    } catch (e) {
      if (mine === seq) results.replaceChildren(h('p.hint', {}, e.status === 422 ? e.message : 'The lookup service did not answer. Check the connection' + (manual ? ', or add the game manually.' : '.')));
    }
  };
  input.addEventListener('input', debounce(run, 450));
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); run(); } });
  input.focus({ preventScroll: true });
  run();

  function candidate(c) {
    const mine = inLibrary(c, excludeId);
    const st = mine && store.statusByCode.get(mine.status);
    const sub = [c.developer, c.genres.slice(0, 3).join(', ')].filter(Boolean).join(' · ');
    return h('button.cand', { type: 'button', onclick: () => onPick(c) },
      h('div.cthumb', {}, c.cover_url ? h('img', { src: c.cover_url.replace('t_cover_big', 't_cover_small'), alt: '', loading: 'lazy' }) : h('div.ph', { style: `background:${gradientFor(c.name)}` }, initials(c.name))),
      h('div.cinfo', {},
        h('div.cname', {}, c.name, c.year ? h('span.year', {}, ` (${c.year})`) : null),
        sub && h('div.csub', {}, sub),
        c.platforms.length ? h('div.csub', {}, c.platforms.slice(0, 6).join(' · ')) : null,
        mine && h('span.inlib', {}, `Already in your library${st ? ': ' + st.label : ''}`)));
  }
}

/** Search IGDB and resolve to the chosen match, or null when the sheet is dismissed. Used by "Fill from IGDB". */
export function pickCandidate({ query = '', excludeId = null } = {}) {
  return new Promise((resolve) => {
    const ui = makeSheet('Match on IGDB');
    let picked = null;
    ui.onClose = () => resolve(picked);
    renderSearch(ui, { query }, { onPick: (c) => { picked = c; ui.close(); }, onManual: null, excludeId });
  });
}

// ---------------------------------------------------------------- add a game

/** candidate: skip the search and open the pre-filled review step for this IGDB game (the Series page's Add). */
export function openAdd({ query = '', candidate = null } = {}) {
  const ui = makeSheet('Add a game', 'add');
  const state = { query: candidate?.name || query };

  function toSearch() {
    ui.guard = null;
    ui.back.hidden = true;
    ui.foot.hidden = true;
    ui.title.textContent = 'Add a game';
    renderSearch(ui, state, { onPick: (c) => toForm(c), onManual: (typed) => toForm(null, typed) });
  }

  // ---------- step 2: review and save ----------
  function toForm(c, typedTitle = '') {
    ui.back.hidden = false;
    ui.title.textContent = c ? 'Review and add' : 'Add manually';

    const platformOptions = unique((g) => g.platforms);
    // The platforms IGDB lists for this game, in the library's spelling: offered as one-tap suggestions.
    const suggested = c ? mapPlatforms(c.platforms, platformOptions) : [];
    const platformCounts = new Map();
    for (const g of store.games) for (const n of g.platforms) platformCounts.set(n, (platformCounts.get(n) || 0) + 1);
    const f = {
      title: c?.name || typedTitle.replace(/^https?:.*/i, ''),
      status: 'backlog',
      platforms: c ? defaultPlatforms(suggested, platformCounts) : (platformOptions.includes('PC') ? ['PC'] : []),
      genres: [...(c?.genres || [])],
      themes: [...(c?.themes || [])],
      series: c?.series || '',
      developer: c?.developer || '',
      release_year: c?.year ? String(c.year) : '',
      ownership: unique((g) => (g.ownership ? [g.ownership] : [])).includes('Owned') ? 'Owned' : '',
      years: [], players: [], notes: '', favorite: false, vr: false, completed: false,
      cover: null,
    };

    // What the user typed beyond the pre-filled form: leaving would lose it, so ask first (like the drawer's edit mode).
    const dirty = () => f.years.length || f.players.length || f.notes.trim() || f.favorite || f.vr || f.completed;
    const confirmLeave = async () => !dirty() || confirmDialog({ title: 'Discard your changes?', message: 'You have entered details that are not saved.', confirm: 'Discard' });
    const back = async () => { if (await confirmLeave()) toSearch(); };
    ui.guard = confirmLeave;
    ui.back.onclick = back;

    const titleIn = text(f, 'title', { placeholder: 'Title', 'aria-label': 'Title', required: true });
    const yearIn = text(f, 'release_year', { type: 'number', inputmode: 'numeric', placeholder: 'e.g. 2025', 'aria-label': 'Release year' });
    const seriesIn = text(f, 'series', { list: ensureDatalist('dl-add-series', unique((g) => (g.series ? [g.series] : []))), 'aria-label': 'Series' });
    const devIn = text(f, 'developer', { list: ensureDatalist('dl-add-dev', unique((g) => (g.developer ? [g.developer] : []))), 'aria-label': 'Developer' });

    // cover choices
    const strip = h('div.cover-strip');
    const coverHint = h('p.hint', {});
    let coverSeq = 0;
    const refreshPressed = () => strip.querySelectorAll('.ctile').forEach((t) => t.setAttribute('aria-pressed', String(t.dataset.url === (f.cover || ''))));
    function drawCovers(options) {
      const tile = (url, label, thumb) => h('button.ctile', { type: 'button', 'data-url': url || '', 'aria-label': label, onclick: () => { f.cover = url; refreshPressed(); } },
        url ? h('img', { src: thumb || url, alt: '', loading: 'lazy' }) : h('span.none', {}, 'No cover'));
      const tiles = options.map((o) => tile(o.url, 'Cover option', o.thumb));
      if (c?.cover_url) tiles.push(tile(c.cover_url, 'IGDB cover', c.cover_url.replace('t_cover_big', 't_cover_small')));
      tiles.push(tile(null, 'No cover', null));
      strip.replaceChildren(...tiles);
      f.cover = options[0]?.url ?? c?.cover_url ?? null; // best guess first: a wide SteamGridDB image, else the IGDB cover
      refreshPressed();
    }
    async function findCovers() {
      const q = f.title.trim();
      if (!q) return;
      const mine = ++coverSeq;
      coverHint.textContent = 'Looking for covers…';
      strip.replaceChildren();
      try {
        const r = await coverSearch(q);
        if (mine !== coverSeq) return;
        f.cover = null;
        coverHint.textContent = r.configured ? (r.covers.length ? 'Pick a cover (wide images match your library).' : 'No covers found.') : 'Cover lookup is not set up.';
        drawCovers(r.covers);
      } catch {
        if (mine === coverSeq) { coverHint.textContent = 'Could not look up covers.'; drawCovers([]); }
      }
    }

    // The same game may already be in the library.
    const existing = c && inLibrary(c, null);
    const existingStatus = existing && store.statusByCode.get(existing.status);
    const dupe = existing && h('div.dupe', {},
      h('span', {}, `“${existing.title}” is already in your library${existingStatus ? ' (' + existingStatus.label + ')' : ''}.`),
      h('button.pill', { type: 'button', onclick: () => { ui.close(); openDrawer(existing); } }, 'Open it'));

    const form = h('div.addform', {},
      dupe,
      field('Title', titleIn),
      field('Status', statusSelect(f)),
      listRow(f, 'Platforms', 'platforms', platformOptions, { suggestions: suggested.slice(0, 10) }),
      yearsRow(f),
      listRow(f, 'Played with', 'players', unique((g) => g.players)),
      field('Ownership', ownershipSelect(f)),
      h('details.more-details', {}, h('summary', {}, 'More details'),
        listRow(f, 'Genres', 'genres', unique((g) => g.genres)),
        listRow(f, 'Themes', 'themes', unique((g) => g.themes)),
        field('Series', seriesIn),
        field('Developer', devIn),
        field('Released', yearIn),
        toggleRow(f, 'favorite', '★ Favorite'),
        toggleRow(f, 'vr', 'VR'),
        toggleRow(f, 'completed', '100% completed'),
        h('div.frow.col', {}, h('span.flabel', {}, 'Notes'), notesField(f))),
      h('div.frow.col', {}, h('span.flabel', {}, 'Cover', h('button.pill', { type: 'button', style: 'margin-left:10px', onclick: findCovers }, 'Search again')), coverHint, strip));
    ui.body.replaceChildren(form);

    const saveBtn = h('button.btn.primary', { type: 'button', onclick: save }, 'Add to library');
    ui.foot.replaceChildren(h('button.btn', { type: 'button', onclick: back }, 'Back'), saveBtn);
    ui.foot.hidden = false;
    findCovers();
    if (!f.title) titleIn.focus({ preventScroll: true });

    async function save() {
      f.title = f.title.trim();
      if (!f.title) { toast('Give the game a title'); titleIn.focus(); return; }
      const problem = validateForm({ ...f, years: f.years });
      if (problem) {
        toast(problem);
        if (/release year/i.test(problem)) { form.querySelector('.more-details').open = true; yearIn.focus(); }
        return;
      }
      saveBtn.disabled = true;
      saveBtn.textContent = 'Adding…';
      const body = {
        title: f.title, status: f.status, platforms: f.platforms, genres: f.genres, themes: f.themes,
        series: f.series.trim(), developer: f.developer.trim(), ownership: f.ownership,
        players: f.players, notes: f.notes, favorite: f.favorite, vr: f.vr, completed: f.completed,
        years: f.years.filter((y) => /^\d{4}$/.test(y)).map(Number),
        periods: f.years.filter((y) => /^(childhood|always)$/i.test(y)).map((y) => y.toLowerCase()),
      };
      if (/^\d{4}$/.test(String(f.release_year).trim())) body.release_year = Number(f.release_year);
      if (c?.igdb_id) body.igdb_id = c.igdb_id;
      if (c?.igdb_id && c.igdb_parent_id != null) body.igdb_parent_id = c.igdb_parent_id; // the Series page's Add knows the version root
      if (f.cover) body.cover_url = f.cover;
      try {
        const { game, cover_error: coverError } = await createGame(body);
        ui.close();
        toast(`Added “${game.title}”`);
        if (coverError) toast(coverError, 5000);
        openDrawer(game);
      } catch (e) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Add to library';
        if (e.status === 409 && e.data?.id && store.byId.get(e.data.id)) {
          toast('That game is already in your library');
          ui.close();
          openDrawer(store.byId.get(e.data.id));
        } else toast(e.message || 'Could not add the game');
      }
    }
  }

  if (candidate) toForm(candidate); else toSearch();
}

// ---------------------------------------------------------------- change a cover or a poster

const SOURCES = [
  { key: 'steamgriddb', label: 'SteamGridDB', hint: 'Community-made covers. Wide images match your library.', posterHint: 'Community-made portrait box art.' },
  { key: 'igdb', label: 'IGDB', hint: 'Official artwork, screenshots and the box cover. Wide pictures look best on the cards.', posterHint: 'The official box cover.' },
];
const LAST_SOURCE = 'cover-source';

/** Change a game's cover: pick the source (SteamGridDB or IGDB), optionally search another title, tap an image. */
export function openCoverPicker(game) { return openImagePicker(game, 'cover'); }

/** Change a game's portrait poster (rankings and tier list picture): IGDB and SteamGridDB portraits, a pasted link, a crop of the wide cover, or automatic again. */
export function openPosterPicker(game) { return openImagePicker(game, 'poster'); }

/** What the sheet says when a source fails: SteamGridDB being blocked or down is a known, temporary thing. */
function failureNote(label, e) {
  return e?.data?.unreachable === false ? `${label} refused the request.` : `${label} can't be reached right now. Try again in a little while.`;
}

function openImagePicker(game, kind) {
  const poster = kind === 'poster';
  const ui = makeSheet(`${poster ? 'Poster' : 'Cover'} · ${game.title}`, poster ? 'poster-picker' : '');
  let source = 'steamgriddb';
  try { const saved = localStorage.getItem(LAST_SOURCE); if (SOURCES.some((s) => s.key === saved)) source = saved; } catch { /* private mode */ }

  const input = h('input', { type: 'search', value: game.title, placeholder: 'Search a different title…', 'aria-label': 'Title to search', enterkeyhint: 'search' });
  const tabs = SOURCES.map((s) => h('button', { type: 'button', onclick: () => { source = s.key; try { localStorage.setItem(LAST_SOURCE, s.key); } catch { /* ignore */ } load(); } }, s.label));
  const status = h('p.hint', {});
  const grid = h('div.cover-grid');
  // A poster shows both sources at once, each in its own section: IGDB answers fast and is never held up by SteamGridDB.
  const sections = [SOURCES[1], SOURCES[0]].map((s) => ({ s, head: h('h3.pp-head', {}, s.label), status: h('p.hint', {}), grid: h('div.cover-grid.poster') }));
  const tools = h('div.cover-tools', {}, h('div.search.pick-search', {}, icon('search'), input), h('button.btn', { type: 'button', onclick: () => load() }, 'Search'));
  if (poster) {
    const link = h('input', { type: 'url', placeholder: 'Paste an image link…', 'aria-label': 'Image link', inputmode: 'url', autocomplete: 'off' });
    const useLink = () => { const u = link.value.trim(); if (u) apply({ url: u }, 'Poster updated'); else link.focus(); };
    link.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); useLink(); } });
    const hasCover = !!game.cover;
    ui.body.replaceChildren(
      h('div.pp-actions', {},
        h('button.btn', { type: 'button', disabled: hasCover ? null : true, title: hasCover ? '' : 'This game has no wide cover to crop', onclick: () => apply({ crop: true }, 'Poster cropped from the cover') }, 'Crop of the wide cover'),
        h('button.btn', { type: 'button', onclick: () => apply({ auto: true }, 'Poster fetched again') }, 'Fetch automatically again')),
      h('div.cover-tools', {}, h('div.search.pick-search', {}, link), h('button.btn', { type: 'button', onclick: useLink }, 'Use link')),
      tools, status,
      ...sections.flatMap((x) => [x.head, x.status, x.grid]));
  } else {
    ui.body.replaceChildren(h('div.mode', { role: 'group', 'aria-label': 'Cover source' }, ...tabs), tools, status, grid);
  }
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); load(); } });

  let seq = 0;
  async function lookupInto(info, st, gr, mine) {
    st.textContent = 'Looking for images…';
    gr.replaceChildren();
    const q = input.value.trim();
    // The game's own IGDB link is only used while the title is unchanged: a different title means a different game.
    const igdbId = info.key === 'igdb' && q === game.title ? game.igdb_id || 0 : 0;
    try {
      const r = await coverSearch(q, { source: info.key, igdbId, shape: poster ? 'poster' : 'cover' });
      if (mine !== seq) return;
      if (!r.configured) { st.textContent = `${info.label} is not set up (its key is missing from .env).`; return; }
      if (!r.covers.length) { st.textContent = `No ${poster ? 'portrait images' : 'images'} found on ${info.label} for this title. Try another spelling.`; return; }
      st.textContent = poster ? info.posterHint : info.hint;
      gr.replaceChildren(...r.covers.map((o) => tile(o)));
    } catch (e) {
      if (mine === seq) st.textContent = failureNote(info.label, e);
    }
  }
  function load() {
    const mine = ++seq;
    if (poster) { status.textContent = ''; sections.forEach((x) => lookupInto(x.s, x.status, x.grid, mine)); return; }
    SOURCES.forEach((s, i) => tabs[i].setAttribute('aria-pressed', String(s.key === source)));
    return lookupInto(SOURCES.find((s) => s.key === source), status, grid, mine);
  }

  // One change at a time: while a request is on its way (a slow "fetch again" can take a while) nothing else can be tapped.
  async function apply(body, done) {
    if (ui.sheet.classList.contains('busy')) return;
    ui.sheet.classList.add('busy');
    ui.body.inert = true;
    status.textContent = 'Saving…';
    try { await setPoster(game.id, body); toast(done); ui.close(); } catch (e) {
      ui.body.inert = false; ui.sheet.classList.remove('busy'); ui.sheet.focus({ preventScroll: true });
      status.textContent = e.status ? `${e.message}.` : 'Could not use that image.';
    }
  }

  function tile(o) {
    const shape = poster ? null : o.width && o.height ? `aspect-ratio:${o.width} / ${o.height}` : null;
    const what = poster ? 'poster' : (o.kind || 'cover').toLowerCase();
    return h('button.ctile.big', { type: 'button', class: poster ? 'portrait' : '', style: shape, 'aria-label': `Use this ${what}`, onclick: async () => {
      if (poster) { apply({ url: o.url }, 'Poster updated'); return; }
      status.textContent = 'Saving…';
      try { await setCover(game.id, o.url); toast('Cover updated'); ui.close(); } catch { status.textContent = 'Could not use that image.'; }
    } }, h('img', { src: o.thumb || o.url, alt: '', loading: 'lazy' }), o.kind && !poster ? h('span.kind', {}, o.kind) : null);
  }

  load();
  return ui;
}
