// The ranking board: state arithmetic, drop positions and export layout. Pure (no DOM): unit-tested.
//
// state = { tiers: [{id, label, color, games: [gameId...]}], pool: [gameId...] }. A game is in exactly one list.
// Every function returns a new state (or a derived value); the input is never changed.

/** Colours offered for a tier (TierMaker's pastel row plus greys). */
export const PALETTE = [
  '#ff7f7f', '#ffbf7f', '#ffdf7f', '#ffff7f', '#bfff7f', '#7fff7f', '#7fffbf', '#7fffff',
  '#7fbfff', '#7f7fff', '#bf7fff', '#ff7fff', '#ff7fbf', '#e8e8e8', '#b0b0b0', '#6e6e6e',
];

export const MAX_LABEL = 80;

/** Black or white text, whichever reads better on a colour (#rrggbb). */
export function textOn(hex) {
  const n = parseInt(hex.slice(1), 16);
  const lum = (0.299 * (n >> 16) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255)) / 255;
  return lum > 0.58 ? '#14161a' : '#ffffff';
}

/** Build the board from GET /api/rankings/<id> (entries come tier by tier, then the pool). */
export function boardFrom(ranking) {
  const tiers = ranking.tiers.map((t) => ({ id: t.id, label: t.label, color: t.color, games: [] }));
  const byId = new Map(tiers.map((t) => [t.id, t]));
  const pool = [];
  const seen = new Set();
  for (const e of ranking.entries) {
    if (seen.has(e.game_id)) continue;
    seen.add(e.game_id);
    const t = e.tier_id == null ? null : byId.get(e.tier_id);
    (t ? t.games : pool).push(e.game_id);
  }
  return { tiers, pool };
}

/** The body of PUT /api/rankings/<id>/board. */
export const toBody = (state) => ({
  tiers: state.tiers.map((t) => ({ id: t.id, label: t.label, color: t.color, games: t.games })),
  pool: state.pool,
});

export const allGames = (state) => [...state.tiers.flatMap((t) => t.games), ...state.pool];

/** Where a game is: { tier: tierId | null, index } or null when it is not on the board. */
export function locate(state, gameId) {
  for (const t of state.tiers) {
    const i = t.games.indexOf(gameId);
    if (i >= 0) return { tier: t.id, index: i };
  }
  const i = state.pool.indexOf(gameId);
  return i >= 0 ? { tier: null, index: i } : null;
}

const without = (list, id) => list.filter((x) => x !== id);
const clamp = (i, n) => Math.max(0, Math.min(i, n));

/**
 * Put a game in a tier (null = the pool) at `index` (default: the end). `index` counts the target list without the
 * game itself, so it is the position the game ends up at. A game that is not on the board is added.
 */
export function place(state, gameId, tierId, index = Infinity) {
  if (tierId != null && !state.tiers.some((t) => t.id === tierId)) return state;
  const tiers = state.tiers.map((t) => ({ ...t, games: without(t.games, gameId) }));
  let pool = without(state.pool, gameId);
  if (tierId == null) { pool = pool.slice(); pool.splice(clamp(index, pool.length), 0, gameId); } else {
    const t = tiers.find((x) => x.id === tierId);
    t.games.splice(clamp(index, t.games.length), 0, gameId);
  }
  return { tiers, pool };
}

/** Move a game one place earlier (delta -1) or later (+1) inside its own list. Unchanged at either end. */
export function shift(state, gameId, delta) {
  const at = locate(state, gameId);
  if (!at) return state;
  const list = at.tier == null ? state.pool : state.tiers.find((t) => t.id === at.tier).games;
  const to = at.index + delta;
  if (to < 0 || to >= list.length) return state;
  return place(state, gameId, at.tier, to);
}

/** Move a game to the neighbouring tier (-1 up, +1 down; from the pool, down enters the first tier, up the last one). */
export function carry(state, gameId, delta) {
  const at = locate(state, gameId);
  if (!at || !state.tiers.length) return state;
  const n = state.tiers.length;
  const cur = at.tier == null ? (delta > 0 ? -1 : n) : state.tiers.findIndex((t) => t.id === at.tier);
  const next = cur + delta;
  if (next < 0 || next >= n) return at.tier == null ? state : place(state, gameId, null);
  return place(state, gameId, state.tiers[next].id);
}

/** Games not on the board yet go to the end of the pool (ids already on it are skipped). */
export function addGames(state, ids) {
  const have = new Set(allGames(state));
  const fresh = [...new Set(ids)].filter((id) => !have.has(id));
  return fresh.length ? { tiers: state.tiers, pool: [...state.pool, ...fresh] } : state;
}

export function removeGame(state, gameId) {
  return { tiers: state.tiers.map((t) => ({ ...t, games: without(t.games, gameId) })), pool: without(state.pool, gameId) };
}

/** Drop games that no longer exist in the library. */
export function keepOnly(state, exists) {
  const keep = (list) => list.filter(exists);
  return { tiers: state.tiers.map((t) => ({ ...t, games: keep(t.games) })), pool: keep(state.pool) };
}

export function updateTier(state, tierId, patch) {
  return { tiers: state.tiers.map((t) => (t.id === tierId ? { ...t, ...patch } : t)), pool: state.pool };
}

/** Insert a tier (as made by the server) at `index` (default: last). */
export function insertTier(state, tier, index = Infinity) {
  const tiers = state.tiers.slice();
  tiers.splice(clamp(index, tiers.length), 0, { games: [], ...tier });
  return { tiers, pool: state.pool };
}

/** Move a tier to position `to`. */
export function moveTier(state, tierId, to) {
  const from = state.tiers.findIndex((t) => t.id === tierId);
  if (from < 0 || to < 0 || to >= state.tiers.length || to === from) return state;
  const tiers = state.tiers.slice();
  const [t] = tiers.splice(from, 1);
  tiers.splice(to, 0, t);
  return { tiers, pool: state.pool };
}

/**
 * Remove a tier; its games go to the end of the pool. Returns { state, removed } where `removed` ({tier, index, games})
 * is what restoreTier needs to bring it back.
 */
export function removeTier(state, tierId) {
  const index = state.tiers.findIndex((t) => t.id === tierId);
  if (index < 0) return { state, removed: null };
  const tier = state.tiers[index];
  return {
    state: { tiers: state.tiers.filter((t) => t.id !== tierId), pool: [...state.pool, ...tier.games] },
    removed: { tier: { id: tier.id, label: tier.label, color: tier.color }, index, games: tier.games.slice() },
  };
}

/** Undo removeTier: the tier comes back (under `newId`, since the server made a new one) with the games it had that are still in the pool. */
export function restoreTier(state, removed, newId) {
  const games = removed.games.filter((id) => state.pool.includes(id));
  const tiers = state.tiers.slice();
  tiers.splice(clamp(removed.index, tiers.length), 0, { ...removed.tier, id: newId, games });
  return { tiers, pool: state.pool.filter((id) => !games.includes(id)) };
}

/** A copy of a tier next to the original (same label and colour, no games); `id` is the server's new id. */
export function duplicateTier(state, tierId, id) {
  const index = state.tiers.findIndex((t) => t.id === tierId);
  if (index < 0) return state;
  const t = state.tiers[index];
  return insertTier(state, { id, label: t.label, color: t.color }, index + 1);
}

// ---- dropping ----

/**
 * Where a dragged tile lands among the tiles of a wrapped list. `rects` are the other tiles' boxes in order
 * ({left, top, right, bottom}); a tile is before the pointer when its row is entirely above it, or it is in the pointer's
 * row and its centre is left of it. Returns the index to pass to `place`.
 */
export function dropIndex(rects, x, y) {
  let n = 0;
  for (const r of rects) {
    if (r.bottom < y || (y >= r.top && (r.left + r.right) / 2 < x)) n++;
  }
  return n;
}

// ---- sizes ----

export const SIZES = ['S', 'M', 'L'];
const WIDTHS = { phone: { S: 52, M: 64, L: 84 }, desktop: { S: 70, M: 90, L: 120 } };

/** Tile width in px for a size on a viewport of `viewport` px (phones are below 640). */
export function tileWidth(size, viewport) {
  return WIDTHS[viewport < 640 ? 'phone' : 'desktop'][size] ?? WIDTHS[viewport < 640 ? 'phone' : 'desktop'].M;
}
/** Posters are 2:3. */
export const tileHeight = (w) => Math.round(w * 1.5);

// ---- PNG export layout ----

/** Split text into lines of at most maxWidth, by words (a word wider than a line is cut). `measure(text)` gives pixels. */
export function wrapText(measure, text, maxWidth) {
  const lines = [];
  let line = '';
  const push = () => { if (line) lines.push(line); line = ''; };
  for (const word of String(text).split(/\s+/).filter(Boolean)) {
    let w = word;
    while (measure(w) > maxWidth && w.length > 1) { // cut a word that cannot fit on one line
      let k = w.length - 1;
      while (k > 1 && measure(w.slice(0, k)) > maxWidth) k--;
      push();
      lines.push(w.slice(0, k));
      w = w.slice(k);
    }
    const next = line ? `${line} ${w}` : w;
    if (line && measure(next) > maxWidth) { push(); line = w; } else line = next;
  }
  push();
  return lines.length ? lines : [''];
}

/**
 * Geometry of the exported picture: `counts` is the number of games of each tier, then the pool is appended by the caller
 * if wanted. Rows of tiles wrap at `perRow`; a tier is at least one tile row high. Returns the canvas size and, per tier,
 * its top `y`, height `h` and a `tile(k)` giving the {x, y} of its k-th tile.
 */
export function exportLayout({ counts, perRow, tileW, tileH, gap, labelW, pad, headH }) {
  const width = pad * 2 + labelW + perRow * (tileW + gap) + gap;
  let y = headH;
  const rows = counts.map((n) => {
    const lines = Math.max(1, Math.ceil(n / perRow));
    const h = lines * (tileH + gap) + gap;
    const top = y;
    y += h + gap;
    return {
      y: top, h, lines,
      tile: (k) => ({ x: pad + labelW + gap + (k % perRow) * (tileW + gap), y: top + gap + Math.floor(k / perRow) * (tileH + gap) }),
    };
  });
  return { width, height: y + pad - gap, rows };
}

/**
 * exportLayout, with more tiles per row until the picture fits what mobile browsers can draw (iOS Safari: about 16.7
 * million pixels, and a side of at most 16384 px). Returns { layout, perRow }; `layout` is null when even 60 per row do
 * not fit (the caller should say so).
 */
export function fitExportLayout(opts, { maxSide = 14000, maxArea = 16_000_000 } = {}) {
  for (let perRow = opts.perRow; perRow <= 60; perRow += 2) {
    const layout = exportLayout({ ...opts, perRow });
    if (layout.height <= maxSide && layout.width <= maxSide && layout.width * layout.height <= maxArea) return { layout, perRow };
  }
  return { layout: null, perRow: 60 };
}
