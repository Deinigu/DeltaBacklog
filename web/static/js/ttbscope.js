// The one rule for "does this game need a time to beat?", shared by Stats (which only adds up times of these games) and
// Needs attention (which lists the ones still missing). Pure (no DOM): unit-tested. The server has the same list of
// statuses for the bulk lookup (internal/api/ttbsync.go ttbStatusList): change both together.
import { ttbMinutes } from './ttb.js';

/** The statuses that need a time, and why. Dropped and evergreen games are not here: Stats does not estimate them. */
export const TTB_RULE = [
  { status: 'backlog', why: 'Stats adds up the hours left in the backlog' },
  { status: 'up_next', why: 'Stats adds up the hours left in Up next' },
  { status: 'playing', why: 'the time helps you plan the game you are playing' },
  { status: 'finished', why: 'Stats estimates the time spent on finished games' },
];

export const TTB_STATUSES = new Set(TTB_RULE.map((r) => r.status));

/** The Needs attention group of a status: the finished games have their own list. */
export const ttbGroupOfStatus = (status) => (status === 'finished' ? 'no_ttb_finished' : 'no_ttb');
export const TTB_GROUPS = ['no_ttb', 'no_ttb_finished'];

/**
 * Where a game stands: null (its status does not need a time), 'known' (it has one), 'none' (it has none and the
 * owner marked "no time on HowLongToBeat": no time exists) or 'missing' (it has none and one could be found).
 */
export function ttbState(g) {
  if (!TTB_STATUSES.has(g.status)) return null;
  if (ttbMinutes(g) !== null) return 'known';
  return g.ttb_not_found ? 'none' : 'missing';
}

/** Counts over a set of games: { total, known, missing, none } for the games whose status needs a time. */
export function ttbCoverage(games) {
  const r = { total: 0, known: 0, missing: 0, none: 0 };
  for (const g of games) {
    const s = ttbState(g);
    if (s === null) continue;
    r.total++;
    r[s]++;
  }
  return r;
}
