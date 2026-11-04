// "Needs attention": which games need a fix, grouped. Pure (no DOM, no network): unit-tested.

/** Statuses where the time to beat matters: the games that are about to be played. */
export const TTB_STATUSES = new Set(['backlog', 'up_next', 'playing']);

/** Groups in display order. `fix` is what the row's main button does (attentionview.js maps it to a screen). */
export const GROUPS = [
  { key: 'unlinked', title: 'Not linked to IGDB', fix: 'link', hint: 'IGDB gives a game its genres, release date, times and poster. Games marked “not on IGDB” are not listed.' },
  { key: 'uncertain', title: 'Uncertain IGDB match', fix: 'confirm', hint: 'The sync found a likely match but was not sure enough to link it. Confirm the top one or pick another.' },
  { key: 'no_cover', title: 'No cover', fix: 'cover', hint: '' },
  { key: 'poster', title: 'No poster, or only a crop of the cover', fix: 'poster', hint: 'The poster is the portrait picture the Rankings use.' },
  { key: 'no_ttb', title: 'No time to beat', fix: 'ttb', hint: 'Only games in Backlog, Up Next and Playing, where the time helps you choose.' },
  { key: 'no_platform', title: 'No platform', fix: 'platforms', hint: '' },
  { key: 'no_year', title: 'No release year', fix: 'year', hint: '' },
  { key: 'no_ownership', title: 'No ownership', fix: 'ownership', hint: '' },
  { key: 'no_genres', title: 'No genres', fix: 'genres', hint: '' },
];

export const groupByKey = (key) => GROUPS.find((g) => g.key === key);

const empty = (a) => !a || a.length === 0;

/** The poster state of a game: 'none' (no poster yet), 'crop' (an automatic crop of the wide cover) or null (fine). */
export function posterState(g) {
  if (!g.poster) return 'none';
  return g.poster_src === 'crop' && !g.poster_user ? 'crop' : null;
}

/**
 * The problems of a game as [group key, note shown on its row] pairs.
 * `uncertain` is the set of game ids the IGDB review queue has proposals for.
 */
export function problemsOf(g, uncertain = new Set()) {
  const out = [];
  if (g.igdb_id == null && !g.igdb_not_found) out.push([uncertain.has(g.id) ? 'uncertain' : 'unlinked', '']);
  if (!g.cover) out.push(['no_cover', '']);
  const p = posterState(g);
  if (p === 'none') out.push(['poster', 'No poster yet']);
  else if (p === 'crop') out.push(['poster', 'Only a crop of the cover']);
  if (TTB_STATUSES.has(g.status) && g.ttb_hastily == null && g.ttb_normally == null && g.ttb_completely == null) out.push(['no_ttb', '']);
  if (empty(g.platforms)) out.push(['no_platform', '']);
  if (g.release_year == null) out.push(['no_year', '']);
  if (!g.ownership && g.status !== 'wishlist') out.push(['no_ownership', '']);
  if (empty(g.genres)) out.push(['no_genres', '']);
  return out;
}

/**
 * The groups with their rows. `queue` is the IGDB review queue (items with game_id and candidates), `ignored` maps a
 * group key to the ignored game ids. Empty groups are left out. Rows are in title order.
 * With showIgnored the ignored rows are included, flagged `ignored: true`; otherwise they are only counted in `ignoredCount`.
 */
export function buildGroups(games, queue = [], ignored = {}, showIgnored = false) {
  const proposals = (queue || []).filter((q) => q.candidates?.length);
  const uncertain = new Set(proposals.map((q) => q.game_id));
  const top = new Map(proposals.map((q) => [q.game_id, q.candidates[0]]));
  const rows = new Map(GROUPS.map((g) => [g.key, []]));
  const hidden = new Map(GROUPS.map((g) => [g.key, 0]));
  const sorted = [...games].sort((a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base', numeric: true }));
  for (const g of sorted) {
    for (const [key, note] of problemsOf(g, uncertain)) {
      const isIgnored = (ignored[key] || []).includes(g.id);
      if (isIgnored && !showIgnored) { hidden.set(key, hidden.get(key) + 1); continue; }
      rows.get(key).push({ game: g, note, ignored: isIgnored, candidate: key === 'uncertain' ? top.get(g.id) || null : null });
    }
  }
  return GROUPS.map((def) => ({ ...def, rows: rows.get(def.key), ignoredCount: hidden.get(def.key) })).filter((g) => g.rows.length > 0);
}

/** The line shown while the bulk time lookup runs: "Looking up times: 3 of 12", or the final report. */
export function syncLine(st) {
  if (!st) return '';
  if (st.running) return `Looking up times: ${st.done} of ${st.total}`;
  return st.report || '';
}

/** How many problems are open (ignored ones not counted). */
export const totalOpen = (groups) => groups.reduce((n, g) => n + g.rows.filter((r) => !r.ignored).length, 0);

/** How many problems are ignored in total (the page offers to show them). */
export function ignoredTotal(games, queue = [], ignored = {}) {
  return buildGroups(games, queue, ignored, true).reduce((n, g) => n + g.rows.filter((r) => r.ignored).length, 0);
}


// ---- which groups are open ----
// A group with this many open items or fewer starts open; a longer one starts collapsed (showing only its count).
export const OPEN_AT_MOST = 3;

/** Is the group open? `state` maps a group key to what the person chose (true/false); with no choice the size decides. */
export function groupOpen(state, key, count) {
  return key in state ? !!state[key] : count <= OPEN_AT_MOST;
}

/** The state after the person opened or closed a group (a new object). */
export const withGroupOpen = (state, key, open) => ({ ...state, [key]: !!open });
