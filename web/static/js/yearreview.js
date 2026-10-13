// Year in review: which games belong to a year, how they group, and the name of the year's ranking.
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

// ---- ranking of a year ----
// "Rank this year" opens the ranking named like this (the one the old per-year tier list became in the migration) or
// makes it, pre-filled with the year's games.

export const yearRankingName = (year) => `Tier list ${year}`;

/** The existing ranking of a year among the list from GET /api/rankings, or undefined. */
export const findYearRanking = (rankings, year) => rankings.find((r) => r.name === yearRankingName(year));
