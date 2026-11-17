// Expansions (IGDB game_type 2) of the library's games: the rules every page shares. Pure (no DOM): unit-tested.
// An expansion lives inside its base game (game.expansions); it has the same statuses and ownership list as a game, but
// both are empty ("Not tracked") until the owner sets them, and it is never counted as a game.
import { norm } from './util.js';
import { groupOf } from './yearreview.js';

/** The statuses that mean the owner is done with an expansion (or never wants it). */
export const CLOSED = new Set(['finished', 'dropped', 'not_interested']);

export const expansionsOf = (g) => g.expansions || [];
/** The expansions the owner has not hidden (a wrong IGDB entry can be hidden). */
export const visible = (g) => expansionsOf(g).filter((x) => !x.hidden);

/** Owned: any ownership value except "Nope" (a game the owner does not have). */
export const isOwned = (x) => !!x.ownership && norm(x.ownership).trim() !== 'nope';
/** Tracked: the owner gave it a status. */
export const isTracked = (x) => !!x.status;
export const isFinished = (x) => x.status === 'finished';
/** Owned, and not finished, dropped or not wanted (no status counts as not started). */
export const leftToPlay = (x) => isOwned(x) && !CLOSED.has(x.status);

/** True when the game has an expansion left to play: the Library's "Expansions left to play" filter. */
export const hasLeftToPlay = (g) => visible(g).some(leftToPlay);

/** The Library card's badge: finished / (owned or tracked, not hidden), e.g. "2/3 ✓". null when there is nothing to count. */
export function expansionBadge(g) {
  const counted = visible(g).filter((x) => isOwned(x) || isTracked(x));
  if (!counted.length) return null;
  const finished = counted.filter(isFinished).length;
  return { finished, total: counted.length, text: `${finished}/${counted.length} ✓`, label: `${finished} of ${counted.length} expansions finished` };
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const byTitle = (a, b) => collator.compare(a.title, b.title);
/** An expansion's release as a sortable number (unix seconds); TBA is Infinity. */
export const releaseKey = (x) => (x.date ?? Infinity);

/** Every expansion with its base game: [{x, game}], hidden ones left out unless asked for. */
export function allExpansions(games, { hidden = false } = {}) {
  const out = [];
  for (const game of games) for (const x of expansionsOf(game)) if (hidden || !x.hidden) out.push({ x, game });
  return out;
}

// ---- Needs attention ----

/** The group of the Needs attention page: the base game is finished, the expansion owned and not done. Rows by game, then release. */
export function attentionRows(games) {
  return allExpansions(games.filter((g) => g.status === 'finished')).filter(({ x }) => leftToPlay(x))
    .sort((a, b) => byTitle(a.game, b.game) || releaseKey(a.x) - releaseKey(b.x) || byTitle(a.x, b.x));
}

// ---- Upcoming ----

/**
 * The expansions that appear on the Upcoming page: of a library game, not hidden, not dropped or unwanted, with a release date
 * today or later or none (TBA). The item has the shape of the page's other items (see upcoming.js); `date` is IGDB's day.
 */
export function upcomingExpansionItems(games, releasePeriod, isReleased, today) {
  const out = [];
  for (const { x, game } of allExpansions(games)) {
    if (x.status === 'dropped' || x.status === 'not_interested' || x.status === 'finished') continue;
    const period = releasePeriod(x.date ?? null, 0);
    if (period && isReleased(period, today)) continue;
    out.push({ id: `x${x.id}`, kind: 'expansion', title: x.title, date: x.date ?? null, format: period ? 0 : null, period, platforms: game.platforms || [],
      seriesName: null, gameTitle: game.title, expansion: x, game });
  }
  return out;
}

// ---- Stats ----

/** The year an expansion was finished: the last one it was played in (as for games); null when only periods or nothing are recorded. */
export const finishedYear = (x) => (x.years?.length ? Math.max(...x.years) : null);

/**
 * { total, owned, finished, perYear: [{year, count}] oldest first, unplaced }.
 * owned counts the expansions you own, plus finished ones you did not mark as owned (so finished never exceeds owned).
 * Never added to the games' own numbers.
 */
export function expansionStats(games) {
  const list = allExpansions(games).map(({ x }) => x);
  const years = new Map();
  let unplaced = 0;
  for (const x of list.filter(isFinished)) {
    const y = finishedYear(x);
    if (y === null) unplaced++; else years.set(y, (years.get(y) || 0) + 1);
  }
  return {
    total: list.length,
    owned: list.filter((x) => isOwned(x) || isFinished(x)).length,
    finished: list.filter(isFinished).length,
    perYear: [...years].map(([year, count]) => ({ year, count })).sort((a, b) => a.year - b.year),
    unplaced,
  };
}

// ---- Year in review ----

/** The expansions played in a year, split like the games (finished, dropped, still playing, played): [{key, label, items: [{x, game}]}]. */
export function expansionsOfYear(games, year, groups) {
  const by = Object.fromEntries(groups.map((g) => [g.key, []]));
  for (const row of allExpansions(games)) if (row.x.years?.includes(year)) by[groupOf(row.x.status)].push(row);
  for (const k of Object.keys(by)) by[k].sort((a, b) => byTitle(a.game, b.game) || releaseKey(a.x) - releaseKey(b.x));
  return groups.map((g) => ({ ...g, items: by[g.key] }));
}

/** How many expansions were played in a year. */
export const countExpansionsOfYear = (games, year) => allExpansions(games).filter(({ x }) => x.years?.includes(year)).length;

/** The years in which an expansion was played, for the Year in review strip (a year with only expansions still has a page). */
export const expansionYears = (games) => new Set(allExpansions(games).flatMap(({ x }) => x.years || []));

// ---- the poster ----
/** The URL of an expansion's small poster (the server downloaded it once), or null. v is IGDB's image id: a changed cover is a new URL. */
export const expansionCoverURL = (x) => (x.cover ? `covers/${encodeURIComponent(x.cover)}?v=${encodeURIComponent(x.cover_v || '')}` : null);

/** "Hearts of Stone" -> "Hearts of Stone (2015)" */
export const withYear = (x) => (x.year ? `${x.title} (${x.year})` : x.title);
