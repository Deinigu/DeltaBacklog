// "Surprise me": which games can be picked, how the mood narrows them, and a random choice that never repeats.
// Pure (no DOM, no Math.random unless no rng is given): unit-tested.
import { matches, activeCount } from './filters.js';
import { isShort } from './ttb.js';

/** Statuses that are never offered unless the filters ask for them explicitly. */
export const EXCLUDED_STATUSES = ['finished', 'dropped', 'not_interested', 'evergreen'];
/** A game counts as "not played recently" when its last calendar year is at least this many years back. */
export const STALE_YEARS = 2;

/** A small seedable generator (mulberry32): same seed, same sequence. Returns floats in [0, 1). */
export function seededRng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6D2B79F5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** True when the state has no search text and no filter at all (sort and layout do not count). */
export const hasNoFilters = (state) => !state.q && activeCount(state) === 0;

/**
 * The games a "Surprise me" can pick from: what the Library or view shows, minus the statuses that are never
 * offered unless the filters include them. With no filters at all it is the Up next games, or the Backlog
 * games when there are none.
 */
export function buildPool(state, games) {
  const shown = games.filter((g) => matches(state, g));
  if (hasNoFilters(state)) {
    const upNext = shown.filter((g) => g.status === 'up_next');
    return upNext.length ? upNext : shown.filter((g) => g.status === 'backlog');
  }
  const asked = state.in.status;
  return shown.filter((g) => !EXCLUDED_STATUSES.includes(g.status) || asked.has(g.status));
}

/** Never played (`childhood` counts as long ago, `always` does not), or last played in a calendar year at least STALE_YEARS before `year` (periods are not calendar years). */
export function notPlayedRecently(game, year) {
  if (game.periods?.includes('always')) return false; // "always" means played all the time
  if (!game.years.length) return true;
  return Math.max(...game.years) <= year - STALE_YEARS;
}

/** The status "Play it" sets: Evergreen games stay Playing - Evergreen, everything else becomes Playing. */
export const playTarget = (status) => (status === 'evergreen' || status === 'playing_evergreen' ? 'playing_evergreen' : 'playing');

export const emptyMood = () => ({ platforms: new Set(), genres: new Set(), vr: false, stale: false, quick: false });
export const moodIsEmpty = (m) => !m.platforms.size && !m.genres.size && !m.vr && !m.stale && !m.quick;

/** Does the game pass the mood? `skip` ignores one part ('platform' | 'genre' | 'vr' | 'stale' | 'quick'), for chip counts. */
function passes(mood, g, year, skip) {
  if (skip !== 'platform' && mood.platforms.size && !g.platforms.some((p) => mood.platforms.has(p))) return false;
  if (skip !== 'genre' && mood.genres.size && !g.genres.some((x) => mood.genres.has(x))) return false;
  if (skip !== 'vr' && mood.vr && !g.vr) return false;
  if (skip !== 'stale' && mood.stale && !notPlayedRecently(g, year)) return false;
  if (skip !== 'quick' && mood.quick && !isShort(g)) return false;
  return true;
}

/** The pool narrowed by the mood. Within platforms (or genres) any selected value is enough; the parts combine with AND. */
export const applyMood = (pool, mood, year) => pool.filter((g) => passes(mood, g, year));

/**
 * What each mood choice would leave, given the other choices: {platform: Map, genre: Map, vr: n, stale: n, quick: n}.
 * Maps are ordered by count (then name), so the commonest chips come first.
 */
export function moodCounts(pool, mood, year) {
  const tally = (key, get) => {
    const m = new Map();
    for (const v of new Set(pool.flatMap(get))) m.set(v, 0);
    for (const g of pool) if (passes(mood, g, year, key)) for (const v of new Set(get(g))) m.set(v, m.get(v) + 1);
    return new Map([...m].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])));
  };
  return {
    platform: tally('platform', (g) => g.platforms),
    genre: tally('genre', (g) => g.genres),
    vr: pool.filter((g) => passes(mood, g, year, 'vr') && g.vr).length,
    stale: pool.filter((g) => passes(mood, g, year, 'stale') && notPlayedRecently(g, year)).length,
    quick: pool.filter((g) => passes(mood, g, year, 'quick') && isShort(g)).length,
  };
}

/**
 * Pick one game at random from `candidates`, never one in `shown` (already offered) or `skipped` (excluded for the session).
 * Returns {game, left, exhausted, allSkipped}: `left` is how many unseen games remain after this pick; `exhausted` is
 * true (with game = null) when every candidate has been offered or skipped, so the caller can offer to start over;
 * `allSkipped` is true when starting over would not help because the skip list covers every candidate.
 * With no candidates at all it returns {game: null, left: 0, exhausted: false, allSkipped: false} (nothing matches).
 */
export function pickOne(candidates, shown, skipped, rng = Math.random) {
  if (!candidates.length) return { game: null, left: 0, exhausted: false, allSkipped: false };
  const open = candidates.filter((g) => !skipped.has(g.id));
  if (!open.length) return { game: null, left: 0, exhausted: true, allSkipped: true };
  const fresh = open.filter((g) => !shown.has(g.id));
  if (!fresh.length) return { game: null, left: 0, exhausted: true, allSkipped: false };
  const game = fresh[Math.min(fresh.length - 1, Math.floor(rng() * fresh.length))];
  return { game, left: fresh.length - 1, exhausted: false, allSkipped: false };
}
