// The ranking editor (#/rankings/<id>): a dark TierMaker-style board of tiers with big poster tiles, and the pool of
// games that are not ranked yet. Games move by dragging (touch: press and hold), or with a tap to select and the
// buttons of the selection bar, or with the keyboard. Everything is saved through the save queue.
import { h, toast, toastAction, gradientFor, initials, icon, promptDialog, confirmDialog } from './util.js';
import {
  store, coverURL, posterURL, getRanking, patchRanking, deleteRanking, duplicateRanking, addRankingTier, deleteRankingTier,
  addRankingGames, removeRankingGame, saveBoard, saveBoardKeepalive, applyPosters,
} from './api.js';
import { openDrawer } from './drawer.js';
import { openGamePicker } from './gamepicker.js';
import { attachDrag } from './rankdrag.js';
import { createSaveQueue } from './savequeue.js';
import { exportRankingPNG } from './tierpng.js';
import {
  PALETTE, MAX_LABEL, SIZES, boardFrom, toBody, allGames, locate, place, shift, carry, addGames, removeGame, updateTier, insertTier,
  moveTier, removeTier, restoreTier, duplicateTier, tileWidth, tileHeight, textOn,
} from './rankboard.js';

const PRESET = [['S', '#ff7f7f'], ['A', '#ffbf7f'], ['B', '#ffdf7f'], ['C', '#bfff7f'], ['D', '#7fbfff'], ['F', '#bf7fff']];

const read = (k, def) => { try { return localStorage.getItem(k) ?? def; } catch { return def; } };
const write = (k, v) => { try { localStorage.setItem(k, v); } catch { /* private mode */ } };

export function renderRanking(root, id) {
  let alive = true;
  let ranking = null;     // { name, description } of the loaded ranking
  let board = null;       // see rankboard.js
  let selected = null;    // the tapped game, shown in the selection bar
  let focusId = null;     // the tile that holds the roving tab stop
  let size = SIZES.includes(read('rk-size', 'M')) ? read('rk-size', 'M') : 'M';
  let titles = read('rk-titles', '0') === '1';
  let sheet = null;       // the open tier sheet or menu, if any
  let polling = false;

  const page = h('div.rk-page');
  root.replaceChildren(page);
  const live = h('div.sr-only', { 'aria-live': 'polite', role: 'status' });
  document.body.append(live);
  const say = (msg) => { live.textContent = ''; setTimeout(() => { live.textContent = msg; }, 20); };

  // ---------- loading ----------
  async function load() {
    page.replaceChildren(h('p.hint', {}, 'Loading…'));
    try {
      const r = await getRanking(id);
      if (!alive) return;
      ranking = { name: r.name, description: r.description };
      applyPosters(r.entries); // posters made since the game list was loaded (also by fetch-posters)
      board = boardFrom(r);
      selected = null;
      drawAll();
      if (r.posters_pending > 0) pollPosters();
    } catch (e) {
      if (!alive) return;
      if (e.status === 404) { page.replaceChildren(h('div.empty', {}, 'This ranking does not exist (any more). ', h('a.btn', { href: '#/rankings' }, 'Back to the rankings'))); return; }
      page.replaceChildren(h('div.empty', {}, 'Could not load the ranking. ', h('button.btn', { type: 'button', onclick: load }, 'Retry')));
    }
  }

  /** Posters are made in the background: look again every few seconds until the server has none left to make. */
  async function pollPosters() {
    if (polling) return;
    polling = true;
    try {
      for (let i = 0; i < 60 && alive; i++) {
        await new Promise((r) => setTimeout(r, 2500));
        if (!alive) return;
        const r = await getRanking(id);
        if (applyPosters(r.entries) && alive) drawBoard();
        if (r.posters_pending === 0) return;
      }
    } catch { /* offline: the posters show up on the next visit */ } finally { polling = false; }
  }

  // ---------- saving ----------
  const queue = createSaveQueue({
    save: () => saveBoard(id, toBody(board)),
    isFatal: (e) => e.status === 400 || e.status === 404, // the server rejected this exact board: retrying cannot help
    onError: () => toast('Could not save the ranking. Will retry.'),
    onFatal: () => { toast('The ranking changed elsewhere, so your last change could not be saved. It was reloaded.'); if (alive) load(); },
  });
  const onHide = () => { if (board) queue.finish(() => saveBoardKeepalive(id, toBody(board))); };
  window.addEventListener('pagehide', onHide);

  /** Apply a new board state: show it and save it. */
  function changed(next, announce) {
    board = next;
    drawBoard();
    queue.change();
    if (announce) say(typeof announce === 'function' ? announce() : announce);
  }

  // ---------- tile geometry (configurable size) ----------
  function applySize() {
    const w = tileWidth(size, window.innerWidth);
    page.style.setProperty('--tw', `${w}px`);
    page.style.setProperty('--th', `${tileHeight(w)}px`);
  }
  const onResize = () => applySize();
  window.addEventListener('resize', onResize);

  // ---------- drawing ----------
  const byId = (gid) => store.byId.get(gid);
  const tierLabelOf = (gid) => {
    const at = locate(board, gid);
    return at?.tier == null ? 'Not ranked' : board.tiers.find((t) => t.id === at.tier).label;
  };

  function tileEl(g) {
    const url = posterURL(g) || coverURL(g);
    const at = locate(board, g.id);
    const list = at.tier == null ? board.pool : board.tiers.find((t) => t.id === at.tier).games;
    if (focusId == null) focusId = g.id;
    const el = h('div.rk-tile', {
      role: 'button', tabindex: g.id === focusId ? '0' : '-1', 'data-id': g.id, title: g.title,
      'aria-label': `${g.title}. ${at.tier == null ? 'Not ranked' : 'Tier ' + tierLabelOf(g.id)}, ${at.index + 1} of ${list.length}`,
      'aria-pressed': String(g.id === selected), class: g.id === selected ? 'selected' : '',
    },
    url ? h('img', { src: url, alt: g.title, loading: 'lazy', decoding: 'async', draggable: 'false' })
      : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)),
    titles && h('span.rk-tname', {}, g.title));
    el.addEventListener('click', () => select(g.id === selected ? null : g.id));
    el.addEventListener('focus', () => { focusId = g.id; });
    el.addEventListener('keydown', (e) => tileKey(e, g));
    attachDrag(el, { onDrop: (tierId, index) => changed(place(board, g.id, tierId, index), () => `${g.title}: ${tierLabelOf(g.id)}, position ${locate(board, g.id).index + 1}`) });
    return el;
  }

  const tiles = (ids) => ids.map(byId).filter(Boolean).map(tileEl);

  const boardBox = h('div.rk-board');
  const addRowBox = h('div.rk-addtier');
  const poolBox = h('section.rk-poolbox');
  const selBar = h('div.rk-selbar', { hidden: true, role: 'toolbar', 'aria-label': 'Move the selected game' });

  function drawBoard() {
    if (!board) return;
    if (focusId != null && !locate(board, focusId)) focusId = null;
    boardBox.replaceChildren(...(board.tiers.length
      ? board.tiers.map((t) => h('div.rk-row', {},
        h('button.rk-label', { type: 'button', style: `background:${t.color};color:${textOn(t.color)}`, 'aria-label': `Tier ${t.label}: ${selected != null ? 'move the selected game here' : 'settings'}`, onclick: () => (selected != null ? moveSelectedTo(t.id) : openTierSheet(t.id)) },
          h('span.rk-label-text', {}, t.label)),
        h('div.rk-cell', { 'data-tier': t.id, role: 'group', 'aria-label': `Tier ${t.label}` }, tiles(t.games))))
      : [h('div.empty.rk-notiers', {}, 'No tiers yet. Add the first one below.')]));
    addRowBox.replaceChildren(h('button.btn', { type: 'button', onclick: addTierClicked }, icon('add'), 'Add tier'),
      ...(board.tiers.length ? [] : [h('button.btn', { type: 'button', onclick: addPreset }, 'Add S to F')]));
    const free = board.pool.filter(byId);
    poolBox.replaceChildren(
      h('h2', {}, `Not ranked (${free.length})`),
      h('div.rk-cell.rk-pool', { 'data-tier': '', role: 'group', 'aria-label': 'Not ranked', onclick: (e) => { if (e.target === e.currentTarget && selected != null) moveSelectedTo(null); } },
        free.length ? tiles(free) : h('p.rk-empty', {}, allGames(board).length ? 'Every game is ranked.' : 'Nothing here yet. Use “Add games” to pick the games you want to rank.')));
    paintSelection();
  }

  function paintSelection() {
    document.body.classList.toggle('rk-selecting', selected != null);
    const g = selected != null && byId(selected);
    if (!g) { selBar.hidden = true; return; }
    const at = locate(board, g.id);
    const list = at.tier == null ? board.pool : board.tiers.find((t) => t.id === at.tier).games;
    const btn = (label, glyph, fn, off = false) => h('button.btn', { type: 'button', 'aria-label': label, title: label, disabled: off || null, onclick: fn }, glyph);
    selBar.hidden = false;
    selBar.replaceChildren(
      h('div.rk-sel-info', {}, h('b', {}, g.title), h('span', {}, at.tier == null ? 'Not ranked' : tierLabelOf(g.id)), h('span.hint', {}, 'Tap a tier to move it there')),
      h('div.rk-sel-btns', {},
        btn('Move earlier', '◀', () => moveSel(shift(board, g.id, -1)), at.index === 0),
        btn('Move later', '▶', () => moveSel(shift(board, g.id, 1)), at.index === list.length - 1),
        btn('Up one tier', '▲', () => moveSel(carry(board, g.id, -1)), !board.tiers.length),
        btn('Down one tier', '▼', () => moveSel(carry(board, g.id, 1)), !board.tiers.length),
        btn('Back to not ranked', '⤓', () => moveSelectedTo(null), at.tier == null),
        btn('Details', 'ⓘ', () => openDrawer(g)),
        btn('Remove from this ranking', '🗑', () => removeSelected(g)),
        btn('Done', '✕', () => select(null))));
  }

  function select(gid) {
    selected = gid;
    if (gid != null) focusId = gid;
    drawBoard();
    if (gid != null) focusTile(gid);
  }
  const focusTile = (gid) => page.querySelector(`.rk-tile[data-id="${gid}"]`)?.focus({ preventScroll: false });

  function moveSel(next) {
    if (next === board) return;
    const gid = selected;
    changed(next, () => `${byId(gid)?.title}: ${tierLabelOf(gid)}, position ${locate(board, gid).index + 1}`);
    focusTile(gid);
  }
  function moveSelectedTo(tierId) {
    const gid = selected;
    if (gid == null) return;
    const at = locate(board, gid);
    if (at.tier === tierId) return;
    changed(place(board, gid, tierId), () => `${byId(gid)?.title}: ${tierLabelOf(gid)}`);
    focusTile(gid);
  }
  async function removeSelected(g) {
    const gid = g.id;
    select(null);
    const was = locate(board, gid); // where it was, for Undo
    board = removeGame(board, gid);
    drawBoard();
    try { await removeRankingGame(id, gid); } catch { toast('Could not remove the game. Reloading.'); load(); return; }
    toastAction(`Removed “${g.title}” from the ranking`, 'Undo', async () => {
      try {
        await addRankingGames(id, [gid]);
        const tier = was && board.tiers.some((t) => t.id === was.tier) ? was.tier : null; // its tier may be gone since
        board = place(addGames(board, [gid]), gid, tier, was && tier === was.tier ? was.index : Infinity);
        drawBoard(); queue.change();
      } catch { toast('Could not bring it back'); }
    });
  }

  // ---------- keyboard ----------
  function allTiles() { return [...page.querySelectorAll('.rk-tile')]; }
  function tileKey(e, g) {
    if (e.target !== e.currentTarget) return;
    const k = e.key;
    if (k === 'Enter' || k === ' ') { e.preventDefault(); select(g.id === selected ? null : g.id); return; }
    if (k === 'Escape' && selected != null) { e.preventDefault(); select(null); return; }
    const arrows = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[k];
    if (!arrows) return;
    e.preventDefault();
    if (selected === g.id) { // move the selected game
      moveSel(arrows[0] ? shift(board, g.id, arrows[0]) : carry(board, g.id, arrows[1]));
      return;
    }
    const all = allTiles(), cur = e.currentTarget, r = cur.getBoundingClientRect();
    const cx = (r.left + r.right) / 2, cy = (r.top + r.bottom) / 2;
    let best = null, bestScore = Infinity;
    for (const t of all) {
      if (t === cur) continue;
      const q = t.getBoundingClientRect();
      const qx = (q.left + q.right) / 2, qy = (q.top + q.bottom) / 2;
      const dx = qx - cx, dy = qy - cy;
      let score;
      if (arrows[0]) { if (Math.abs(dy) > r.height / 2 || dx * arrows[0] <= 0) continue; score = Math.abs(dx); } else {
        if (dy * arrows[1] <= r.height / 2) continue;
        score = Math.abs(dy) * 4 + Math.abs(dx);
      }
      if (score < bestScore) { best = t; bestScore = score; }
    }
    if (!best && arrows[0]) { // end of a row: continue on the next / previous row
      const i = all.indexOf(cur) + arrows[0];
      best = all[i] || null;
    }
    best?.focus();
  }

  // ---------- tiers ----------
  let paletteNext = 0;
  async function newTier(label, color, index) {
    const t = await addRankingTier(id, { label, color, ...(index == null ? {} : { index }) });
    board = insertTier(board, t, index);
    return t;
  }
  async function addTierClicked() {
    try {
      const color = PALETTE[paletteNext++ % PALETTE.length];
      const t = await newTier('New tier', color);
      drawBoard();
      queue.change();
      openTierSheet(t.id, true);
    } catch { toast('Could not add the tier'); }
  }
  async function addPreset() {
    try {
      for (const [label, color] of PRESET) await newTier(label, color);
      drawBoard();
      queue.change();
      say('Tiers S to F added');
    } catch { toast('Could not add all the tiers'); drawBoard(); }
  }
  async function deleteTierClicked(t) {
    closeSheet();
    const { state, removed } = removeTier(board, t.id);
    board = state;
    drawBoard();
    try { await queue.flush(); await deleteRankingTier(id, t.id); } catch { toast('Could not delete the tier. Reloading.'); load(); return; }
    toastAction(`Deleted the tier “${t.label}”`, 'Undo', async () => {
      try {
        const nt = await addRankingTier(id, { label: removed.tier.label, color: removed.tier.color, index: removed.index });
        board = restoreTier(board, removed, nt.id);
        drawBoard();
        queue.change();
      } catch { toast('Could not bring the tier back'); }
    }, 8000);
  }
  async function duplicateTierClicked(t) {
    try {
      const at = board.tiers.findIndex((x) => x.id === t.id);
      const nt = await addRankingTier(id, { label: t.label, color: t.color, index: at + 1 });
      board = duplicateTier(board, t.id, nt.id);
      drawBoard();
      queue.change();
      paintSheet?.();
    } catch { toast('Could not duplicate the tier'); }
  }

  // ---------- sheets (tier settings, ranking menu) ----------
  let paintSheet = null;
  function closeSheet() {
    if (!sheet) return;
    sheet.scrim.remove(); sheet.el.remove();
    document.removeEventListener('keydown', sheet.onKey);
    const back = sheet.opener;
    sheet = null; paintSheet = null;
    if (back?.isConnected) back.focus();
  }
  function showSheet(el, { opener = document.activeElement } = {}) {
    closeSheet();
    const scrim = h('div.scrim.show', { onclick: closeSheet });
    scrim.style.zIndex = 95;
    el.style.zIndex = 96;
    const onKey = (e) => {
      if (e.key === 'Escape') { e.preventDefault(); closeSheet(); return; }
      if (e.key !== 'Tab') return;
      const items = [...el.querySelectorAll('button:not([disabled]), input, textarea, a')];
      const first = items[0], last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, el);
    sheet = { el, scrim, onKey, opener };
  }

  function openTierSheet(tierId, fresh = false) {
    const current = () => board.tiers.find((t) => t.id === tierId);
    if (!current()) return;
    const label = h('textarea.rk-label-in', { rows: '2', maxlength: String(MAX_LABEL), 'aria-label': 'Tier label', placeholder: 'Label, e.g. Me ha gustado mucho' });
    label.value = current().label;
    label.addEventListener('input', () => {
      const v = label.value.replace(/\s+/g, ' ');
      if (!v.trim()) return; // a tier needs a label: keep the last good one while the field is empty
      board = updateTier(board, tierId, { label: v.trim() });
      drawBoard(); queue.change();
    });
    label.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); closeSheet(); } });
    label.addEventListener('blur', () => { if (!label.value.trim() && current()) label.value = current().label; });

    const swatches = h('div.rk-swatches', { role: 'group', 'aria-label': 'Colour' });
    const custom = h('input.rk-custom', { type: 'color', 'aria-label': 'Custom colour' });
    const setColor = (c) => { board = updateTier(board, tierId, { color: c }); drawBoard(); queue.change(); paint(); };
    custom.addEventListener('input', () => setColor(custom.value));
    const up = h('button.btn', { type: 'button', onclick: () => { board = moveTier(board, tierId, board.tiers.findIndex((t) => t.id === tierId) - 1); drawBoard(); queue.change(); paint(); } }, '▲ Move up');
    const down = h('button.btn', { type: 'button', onclick: () => { board = moveTier(board, tierId, board.tiers.findIndex((t) => t.id === tierId) + 1); drawBoard(); queue.change(); paint(); } }, '▼ Move down');
    const dup = h('button.btn', { type: 'button', onclick: () => duplicateTierClicked(current()) }, 'Duplicate');
    const del = h('button.btn.danger-text', { type: 'button', onclick: () => deleteTierClicked(current()) }, 'Delete tier');

    function paint() {
      const t = current();
      if (!t) { closeSheet(); return; }
      swatches.replaceChildren(...PALETTE.map((c) => h('button.rk-swatch', { type: 'button', style: `background:${c}`, 'aria-label': `Colour ${c}`, 'aria-pressed': String(c === t.color), onclick: () => setColor(c) })));
      custom.value = t.color;
      const i = board.tiers.findIndex((x) => x.id === tierId);
      up.disabled = i === 0; down.disabled = i === board.tiers.length - 1;
      const n = t.games.length;
      del.title = n ? `Its ${n} game${n === 1 ? '' : 's'} go back to not ranked` : '';
    }
    paintSheet = paint;
    const el = h('div.sheet.open.rk-tier-sheet', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Tier settings' },
      h('div.filters-head', {}, h('span', {}, 'Tier'), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: closeSheet }, '✕')),
      h('div.filters-body', {},
        h('label.rk-field', {}, h('span', {}, 'Label'), label),
        h('div.rk-field', {}, h('span', {}, 'Colour'), swatches, h('label.rk-custom-wrap', {}, custom, h('span', {}, 'Custom colour'))),
        h('div.rk-sheet-actions', {}, up, down, dup, del)),
      h('div.filters-foot', {}, h('button.btn.primary', { type: 'button', onclick: closeSheet }, 'Done')));
    showSheet(el, { opener: null });
    paint();
    label.focus();
    if (fresh) label.select();
  }

  function openMenu(opener) {
    const item = (text, fn, cls = '') => h('button.pick-row', { type: 'button', class: cls, onclick: () => { closeSheet(); fn(); } }, h('span', {}, text));
    const el = h('div.sheet.open', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Ranking menu' },
      h('div.filters-head', {}, h('span', {}, ranking.name), h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: closeSheet }, '✕')),
      h('div.filters-body', {},
        item('Rename', renameRanking),
        item(ranking.description ? 'Edit description' : 'Add a description', editDescription),
        item('Add the tiers S to F', addPreset),
        item('Duplicate this ranking', duplicateClicked),
        item('Delete this ranking', deleteClicked, 'danger-text')));
    showSheet(el, { opener });
    el.querySelector('button.pick-row')?.focus();
  }

  async function renameRanking() {
    const name = await promptDialog({ title: 'Rename the ranking', value: ranking.name });
    if (!name || name === ranking.name || !alive) return;
    try { await patchRanking(id, { name }); ranking.name = name; drawHead(); } catch { toast('Could not rename'); }
  }
  async function editDescription() {
    const d = await promptDialog({ title: 'Description', value: ranking.description || '', placeholder: 'What is this ranking about?', multiline: true, maxlength: 500, allowEmpty: true });
    if (d == null || !alive) return;
    try { await patchRanking(id, { description: d }); ranking.description = d || null; drawHead(); } catch { toast('Could not save the description'); }
  }
  async function duplicateClicked() {
    try { await queue.flush(); const copy = await duplicateRanking(id); toast('Duplicated'); location.hash = `#/rankings/${copy.id}`; } catch { toast('Could not duplicate'); }
  }
  async function deleteClicked() {
    if (!await confirmDialog({ title: `Delete “${ranking.name}”?`, message: 'The games stay in your library; only this ranking is deleted.' })) return;
    try { await deleteRanking(id); alive = false; board = null; location.hash = '#/rankings'; } catch { toast('Could not delete'); }
  }

  // ---------- adding games, export ----------
  async function addGamesClicked() {
    const picked = await openGamePicker({ title: 'Add games to rank', existing: new Set(allGames(board)) });
    if (!picked?.length || !alive) return;
    try {
      const res = await addRankingGames(id, picked);
      board = addGames(board, picked);
      drawBoard();
      toast(`${res.added.length} game${res.added.length === 1 ? '' : 's'} added to “Not ranked”`);
      pollPosters();
    } catch { toast('Could not add the games'); }
  }
  async function exportClicked(btn) {
    btn.disabled = true;
    try { await exportRankingPNG({ name: ranking.name, board, byId: store.byId }); } catch (e) { toast(/too many/.test(e?.message) ? 'Too many ranked games for one picture' : 'Could not make the image'); } finally { btn.disabled = false; }
  }

  // ---------- page chrome ----------
  const head = h('header.rk-head');
  function drawHead() {
    const sizeBtns = SIZES.map((s) => h('button.btn', { type: 'button', 'aria-pressed': String(s === size), 'aria-label': `Tile size ${s}`, onclick: () => { size = s; write('rk-size', s); applySize(); drawHead(); } }, s));
    const titlesBtn = h('button.btn', { type: 'button', 'aria-pressed': String(titles), onclick: () => { titles = !titles; write('rk-titles', titles ? '1' : '0'); drawHead(); drawBoard(); } }, 'Titles');
    const exportBtn = h('button.btn', { type: 'button', onclick: (e) => exportClicked(e.currentTarget) }, 'Export PNG');
    const moreBtn = h('button.btn', { type: 'button', 'aria-haspopup': 'dialog', 'aria-label': 'Ranking menu', onclick: (e) => openMenu(e.currentTarget) }, icon('more'));
    head.replaceChildren(
      h('a.rk-back', { href: '#/rankings' }, '‹ Rankings'),
      h('div.rk-titlebar', {}, h('h1', {}, h('button.rk-title', { type: 'button', title: 'Rename', onclick: renameRanking }, ranking.name)),
        ranking.description && h('p.hint.rk-desc', {}, ranking.description)),
      h('div.rk-tools', {},
        h('button.btn.primary', { type: 'button', onclick: addGamesClicked }, icon('add'), 'Add games'),
        h('div.rk-seg', { role: 'group', 'aria-label': 'Tile size' }, sizeBtns),
        titlesBtn, exportBtn, moreBtn));
  }

  function drawAll() {
    applySize();
    drawHead();
    page.replaceChildren(head,
      h('p.hint.rk-help', {}, 'Drag a game into a tier (touch: press and hold), or tap it and then a tier or the buttons. Tap a tier name to change it. Saved automatically.'),
      boardBox, addRowBox, poolBox, selBar);
    drawBoard();
  }

  const onKeyGlobal = (e) => { if (e.key === 'Escape' && selected != null && !sheet && !document.querySelector('.dialog')) select(null); };
  document.addEventListener('keydown', onKeyGlobal);
  // A game got a new cover or poster: the tiles that show it follow.
  const onGameChanged = () => { if (board) drawBoard(); };
  window.addEventListener('games-changed', onGameChanged);
  load();

  return () => {
    alive = false;
    closeSheet();
    if (board) queue.finish(() => saveBoardKeepalive(id, toBody(board)));
    document.body.classList.remove('rk-selecting');
    live.remove();
    window.removeEventListener('pagehide', onHide);
    window.removeEventListener('resize', onResize);
    document.removeEventListener('keydown', onKeyGlobal);
    window.removeEventListener('games-changed', onGameChanged);
  };
}
