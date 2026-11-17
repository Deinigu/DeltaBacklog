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
/** Out already: its release date is not in the future (no date counts as out: an owned expansion without one is an old one). */
export const isReleased = (x, now = Date.now()) => !(x.date && x.date * 1000 > now);
/** Owned, out, and not finished, dropped or not wanted (no status counts as not started). A pre-order is not left to play yet. */
export const leftToPlay = (x, now = Date.now()) => isOwned(x) && !CLOSED.has(x.status) && isReleased(x, now);

/** True when the game has an expansion left to play: the Library's "Expansions left to play" filter. */
export const hasLeftToPlay = (g, now = Date.now()) => visible(g).some((x) => leftToPlay(x, now));

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

/** How long a game's expansions stay fresh on the server: they are not read again within this many days. */
export const FRESH_DAYS = 30;

/** A linked game whose expansions were never read from IGDB, or not within FRESH_DAYS: the drawer reads them in the background. */
export function isStale(g, now = Date.now()) {
  if (!g.igdb_id) return false;
  const t = Date.parse(g.expansions_fetched || '');
  return !(t > 0) || now - t > FRESH_DAYS * 86400000;
}

// ---- Needs attention ----

/** The group of the Needs attention page: the base game is finished, the expansion owned and not done. Rows by game, then release. */
export function attentionRows(games, now = Date.now()) {
  return allExpansions(games.filter((g) => g.status === 'finished')).filter(({ x }) => leftToPlay(x, now))
    .sort((a, b) => byTitle(a.game, b.game) || releaseKey(a.x) - releaseKey(b.x) || byTitle(a.x, b.x));
}

// ---- Upcoming ----

/** Statuses that mean the owner is playing it: it is out, whatever IGDB's date says. */
const UNDER_WAY = new Set(['playing', 'playing_evergreen', 'evergreen', 'hold']);

/**
 * Whether an expansion without a release date is still "to be announced": not when the owner owns it (as for isReleased), played it,
 * or is playing it. IGDB lacks the date of some old expansions; those would otherwise sit under "Later / TBA" for ever.
 */
export const isAwaited = (x) => !isOwned(x) && !x.years?.length && !x.periods?.length && !UNDER_WAY.has(x.status);

/**
 * The expansions the Upcoming page may list: of a library game, not hidden, not finished, dropped or ruled out, dated or TBA (see isAwaited). The
 * page (buildUpcoming) then drops the ones that came out long ago, as it does for games. The item has the shape of the page's
 * other items (see upcoming.js); `date` is IGDB's day (an expansion has no coarser precision), `period` comes from releasePeriod.
 */
export function upcomingExpansionItems(games, releasePeriod) {
  const out = [];
  for (const { x, game } of allExpansions(games)) {
    if (x.status === 'dropped' || x.status === 'not_interested' || x.status === 'finished') continue;
    if (!x.date && !isAwaited(x)) continue; // no date, but owned or played: out already
    const period = releasePeriod(x.date ?? null, 0);
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

/**
 * One text cell for the table and the CSV export: "Hearts of Stone (Finished, Owned); Blood and Wine (Not tracked)". Hidden
 * expansions are left out. statusLabel maps a status code to its label.
 */
export function expansionsCell(g, statusLabel = (c) => c) {
  return visible(g).map((x) => `${x.title} (${[x.status ? statusLabel(x.status) : 'Not tracked', x.ownership].filter(Boolean).join(', ')})`).join('; ');
}

/**
 * What a screen reader hears for an expansion's row (the visible status, ownership and years included):
 * "Hearts of Stone (2015), Finished, Owned, played 2016: edit". statusLabel maps a status code to its label; game adds "of <title>".
 */
export function expansionRowLabel(x, statusLabel = (c) => c, game = null) {
  const played = [...(x.periods || []), ...(x.years || []).map(String)];
  return [withYear(x) + (game ? `, expansion of ${game.title}` : ''), x.status ? statusLabel(x.status) : 'Not tracked', x.ownership || null,
    played.length ? `played ${played.join(', ')}` : null, x.hidden ? 'hidden' : null].filter(Boolean).join(', ') + ': edit';
}

/** "Hearts of Stone" -> "Hearts of Stone (2015)" */
export const withYear = (x) => (x.year ? `${x.title} (${x.year})` : x.title);
