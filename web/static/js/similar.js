// Similar games: IGDB's similar_games of the games the user finished or marked as a favourite, that the library does not
// have (GET api/similar, read from the server's cache). Pure (no DOM): unit-tested.
// A source is a library game that is linked to IGDB and finished or a favourite. A suggestion is
// ranked by how many sources suggest it, then by how recently the newest of them was played.

import { isFinished } from './series.js';

/** A game the page may take suggestions from: linked to IGDB and finished or a favourite. */
export const isSource = (g) => g.igdb_id != null && (isFinished(g) || !!g.favorite);

/** When a source was played last: its latest year played, else 0. A later year, or a game added later, is newer. */
const recency = (g) => [g.years?.length ? Math.max(...g.years) : 0, g.id ?? 0];
const newer = (a, b) => { const x = recency(a), y = recency(b); return x[0] !== y[0] ? y[0] - x[0] : y[1] - x[1]; };

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/**
 * Sort the sources of one suggestion, newest first, and drop the ones that are no longer sources (a game that went
 * back to the backlog, lost its favourite mark or was deleted).
 */
export function sourcesOf(sugg, games) {
  const byIGDB = new Map();
  for (const g of games) if (isSource(g)) byIGDB.set(g.igdb_id, g);
  return (sugg.sources ?? []).map((id) => byIGDB.get(id)).filter(Boolean).sort(newer);
}

/** "Because you played A, B and C" (at most `n` titles, then "and 2 more"). */
export function becauseText(sources, n = 3) {
  const names = sources.slice(0, n).map((g) => g.title);
  if (!names.length) return '';
  const more = sources.length - names.length;
  const list = names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
  return `Because you played ${list}${more > 0 ? ` and ${more} more` : ''}`;
}

/**
 * Index the answer of GET api/similar against the library as it is now:
 *   items   suggestions the library does not have, not hidden, with at least one source: ranked, each
 *           { ...suggestion, sourceGames } (sourceGames newest first)
 *   hidden  the hidden suggestions that are not owned
 * A game counts as owned with its whole family (the root of its version chain), like on the Series page: the server sent
 * the roots it knew (`owned_roots`), and games added since are matched by IGDB id.
 */
export function similarIndex(resp, games) {
  const ownedIds = new Set();
  for (const g of games) if (g.igdb_id != null) ownedIds.add(g.igdb_id);
  const ownedRoots = new Set(resp?.owned_roots ?? []);
  const rootOf = (s) => (s.parent > 0 ? s.parent : s.igdb_id);
  const isOwned = (s) => ownedIds.has(s.igdb_id) || ownedRoots.has(rootOf(s)) || (s.parent > 0 && ownedIds.has(s.parent));
  const items = [], hidden = [];
  for (const s of resp?.suggestions ?? []) {
    if (isOwned(s)) continue;
    const sourceGames = sourcesOf(s, games);
    if (!sourceGames.length) continue;
    const e = { ...s, sourceGames };
    if (s.hidden) { hidden.push(e); continue; }
    items.push(e);
  }
  const rank = (a, b) => b.sourceGames.length - a.sourceGames.length || newer(a.sourceGames[0], b.sourceGames[0])
    || (b.year ?? 0) - (a.year ?? 0) || collator.compare(a.title, b.title) || a.igdb_id - b.igdb_id;
  items.sort(rank);
  hidden.sort((a, b) => collator.compare(a.title, b.title));
  return { items, hidden };
}

/**
 * Flip the hidden flag of a suggestion inside the answer we hold, so that the index rebuilt from it on every
 * games-changed keeps what the owner hid or showed (the same job as moveHidden for the Series page).
 */
export function setHiddenFlag(resp, igdbId, hidden) {
  for (const s of resp?.suggestions ?? []) if (s.igdb_id === igdbId) s.hidden = hidden;
}

/** The suggestions the cache holds for one library game (for the drawer): not owned, not hidden, ranked. */
export function similarFor(resp, games, game, n = 6) {
  if (!isSource(game)) return [];
  return similarIndex(resp, games).items.filter((s) => s.sourceGames.some((g) => g.id === game.id)).slice(0, n);
}
