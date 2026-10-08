// Year in review: which games belong to a year, how they group, and the tier list arithmetic.
// Pure (no DOM): unit-tested. A game belongs to a year only through its `play` rows (game.years).

export const GROUPS = [
  { key: 'finished', label: 'Finished' },
  { key: 'dropped', label: 'Dropped' },
  { key: 'playing', label: 'Still playing' },
  { key: 'played', label: 'Played' },
];

/** The group a game falls in, from its current status. */
export function groupOf(status) {
  if (status === 'finished') return 'finished';
  if (status === 'dropped') return 'dropped';
  if (status === 'playing' || status === 'playing_evergreen') return 'playing';
  return 'played';
}

export const gamesOfYear = (games, year) => games.filter((g) => g.years.includes(year));

/** Games of a year split into GROUPS (empty groups are kept so callers can hide or show them). */
export function groupYear(games, year) {
  const by = Object.fromEntries(GROUPS.map((g) => [g.key, []]));
  for (const g of gamesOfYear(games, year)) by[groupOf(g.status)].push(g);
  return GROUPS.map((grp) => ({ ...grp, games: by[grp.key] }));
}

/** [{name, count}] biggest first, then by name; `pick(game)` returns the list of names to count. */
export function breakdown(games, pick) {
  const counts = new Map();
  for (const g of games) for (const n of new Set(pick(g))) counts.set(n, (counts.get(n) || 0) + 1);
  return [...counts].map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
}

/** Years that have at least one game, newest first, with their counts. */
export function yearCounts(games) {
  const counts = new Map();
  for (const g of games) for (const y of new Set(g.years)) counts.set(y, (counts.get(y) || 0) + 1);
  return [...counts].map(([year, count]) => ({ year, count })).sort((a, b) => b.year - a.year);
}

/**
 * Previous and next year to browse to: one calendar year at a time between the first year with games and
 * `thisYear` (or the latest year with games, if later), so a gap year is reachable. null at either end.
 */
export function neighbours(counts, year, thisYear) {
  if (!counts.length) return { prev: null, next: null };
  const first = counts[counts.length - 1].year;
  const last = Math.max(counts[0].year, thisYear);
  return { prev: year > first ? year - 1 : null, next: year < last ? year + 1 : null };
}

/** Route "years/2024" gives 2024, "years" gives null (the page picks a default), anything else undefined. */
export function parseYearPath(path) {
  const m = /^years(?:\/(\d{4}))?$/.exec(path);
  if (!m) return undefined;
  if (!m[1]) return null;
  const y = Number(m[1]);
  return y >= 1970 && y <= 2100 ? y : undefined;
}

// ---- tier list ----
// State: { tiers: [{id, name, color}], rows: {tierId: [gameId...]} }. Games in no row are unranked.

/** Build the state from the server's tiers and entries, ignoring games that are not in the year's pool. */
export function tierState(tiers, entries, poolIds) {
  const pool = new Set(poolIds);
  const rows = Object.fromEntries(tiers.map((t) => [t.id, []]));
  const seen = new Set();
  for (const e of entries) {
    if (!pool.has(e.game_id) || !rows[e.tier_id] || seen.has(e.game_id)) continue;
    seen.add(e.game_id);
    rows[e.tier_id].push(e.game_id);
  }
  return { tiers, rows };
}

/**
 * Remember where each game of the pool is ranked (`known`: game id -> tier id). Games outside the pool keep their
 * last placement, so a year removed from a game and added back returns it to its tier.
 */
export function remember(known, state, poolIds) {
  const ranked = new Map(Object.entries(state.rows).flatMap(([tier, ids]) => ids.map((id) => [id, Number(tier)])));
  for (const id of poolIds) { if (ranked.has(id)) known.set(id, ranked.get(id)); else known.delete(id); }
}

/** Entries to put back for games that joined the pool again: their remembered placement, if any. */
export const restored = (known, ids) => ids.filter((id) => known.has(id)).map((id) => ({ game_id: id, tier_id: known.get(id) }));

export const tierOf = (state, gameId) => {
  for (const t of state.tiers) if (state.rows[t.id].includes(gameId)) return t.id;
  return null;
};

/** Games of the pool that are in no tier, in pool order. */
export const unranked = (state, poolIds) => {
  const ranked = new Set(Object.values(state.rows).flat());
  return poolIds.filter((id) => !ranked.has(id));
};

/**
 * Put a game in a tier (null = back to unranked) at `index` (default: the end). Returns a new state;
 * the input is not changed.
 */
export function place(state, gameId, tierId, index = Infinity) {
  const rows = Object.fromEntries(state.tiers.map((t) => [t.id, state.rows[t.id].filter((id) => id !== gameId)]));
  if (tierId != null && rows[tierId]) rows[tierId].splice(Math.max(0, Math.min(index, rows[tierId].length)), 0, gameId);
  return { tiers: state.tiers, rows };
}

/** The body of PUT /api/tierlist/<year>. */
export const toEntries = (state) => state.tiers.flatMap((t) => state.rows[t.id].map((game_id) => ({ game_id, tier_id: t.id })));
