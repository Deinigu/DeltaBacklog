// Quick add: search IGDB by title or store link, pick a match, review the pre-filled form and cover, save.
// Also the IGDB match picker and the cover picker used by the game drawer.
import { h, icon, toast, norm, debounce, gradientFor, initials } from './util.js';
import { store, lookup, coverSearch, createGame, setCover } from './api.js';
import { openDrawer } from './drawer.js';
import { mapPlatforms, defaultPlatforms } from './platforms.js';
import { unique, field, text, ensureDatalist, listRow, statusSelect, ownershipSelect } from './form.js';

/** A scrim + sheet pair that closes together. Returns {sheet, head, body, foot, back, title, close}. */
export function makeSheet(titleText, extraClass = '') {
  let onClose = null;
  const close = () => { scrim.remove(); sheet.remove(); document.removeEventListener('keydown', onKey); onClose && onClose(); };
  const scrim = h('div.scrim.show', { onclick: close });
  scrim.style.zIndex = 70;
  const back = h('button.btn', { type: 'button', hidden: true, 'aria-label': 'Back' }, '←');
  const title = h('span', {}, titleText);
  const head = h('div.filters-head', {}, h('span.headl', {}, back, title), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕'));
  const body = h('div.filters-body');
  const foot = h('div.filters-foot', { hidden: true });
  const sheet = h('div.sheet.open', { role: 'dialog', 'aria-modal': 'true', 'aria-label': titleText, class: extraClass }, head, body, foot);
  sheet.style.zIndex = 71;
  const onKey = (e) => { if (e.key === 'Escape' && !document.querySelector('.sheet.open + .sheet.open')) close(); };
  document.addEventListener('keydown', onKey);
  document.body.append(scrim, sheet);
  return { sheet, head, body, foot, back, title, close, set onClose(fn) { onClose = fn; } };
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
  ui.body.replaceChildren(h('div.search.pick-search', {}, icon('search'), input), results, manual);

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

export function openAdd({ query = '' } = {}) {
  const ui = makeSheet('Add a game', 'add');
  const state = { query };

  function toSearch() {
    ui.back.hidden = true;
    ui.foot.hidden = true;
    ui.title.textContent = 'Add a game';
    renderSearch(ui, state, { onPick: (c) => toForm(c), onManual: (typed) => toForm(null, typed) });
  }

  // ---------- step 2: review and save ----------
  function toForm(c, typedTitle = '') {
    ui.back.hidden = false;
    ui.back.onclick = toSearch;
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
      cover: null,
    };

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
      listRow(f, 'Genres', 'genres', unique((g) => g.genres)),
      listRow(f, 'Themes', 'themes', unique((g) => g.themes)),
      field('Series', seriesIn),
      field('Developer', devIn),
      field('Released', yearIn),
      field('Ownership', ownershipSelect(f)),
      h('div.frow.col', {}, h('span.flabel', {}, 'Cover', h('button.pill', { type: 'button', style: 'margin-left:10px', onclick: findCovers }, 'Search again')), coverHint, strip));
    ui.body.replaceChildren(form);

    const saveBtn = h('button.btn.primary', { type: 'button', onclick: save }, 'Add to library');
    ui.foot.replaceChildren(h('button.btn', { type: 'button', onclick: toSearch }, 'Back'), saveBtn);
    ui.foot.hidden = false;
    findCovers();
    if (!f.title) titleIn.focus({ preventScroll: true });

    async function save() {
      f.title = f.title.trim();
      if (!f.title) { toast('Give the game a title'); titleIn.focus(); return; }
      saveBtn.disabled = true;
      saveBtn.textContent = 'Adding…';
      const body = {
        title: f.title, status: f.status, platforms: f.platforms, genres: f.genres, themes: f.themes,
        series: f.series.trim(), developer: f.developer.trim(), ownership: f.ownership,
      };
      if (/^\d{4}$/.test(String(f.release_year).trim())) body.release_year = Number(f.release_year);
      if (c?.igdb_id) body.igdb_id = c.igdb_id;
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

  toSearch();
}

// ---------------------------------------------------------------- change a cover

export function openCoverPicker(game) {
  const ui = makeSheet(`Cover · ${game.title}`);
  const status = h('p.hint', {}, 'Looking for covers…');
  const grid = h('div.cover-grid');
  ui.body.replaceChildren(status, grid);
  coverSearch(game.title).then((r) => {
    if (!r.configured) { status.textContent = 'Cover lookup is not set up (SteamGridDB key missing from .env).'; return; }
    status.textContent = r.covers.length ? 'Tap an image to use it.' : 'No covers found for this title.';
    grid.replaceChildren(...r.covers.map((o) => h('button.ctile.big', { type: 'button', 'aria-label': 'Use this cover', onclick: async () => {
      status.textContent = 'Saving…';
      try { await setCover(game.id, o.url); toast('Cover updated'); ui.close(); } catch { status.textContent = 'Could not use that image.'; }
    } }, h('img', { src: o.thumb || o.url, alt: '', loading: 'lazy' }))));
  }).catch(() => { status.textContent = 'Could not look up covers.'; });
}
