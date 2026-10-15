// Pure helpers for the Screenshots page (#/screenshots): the API query, grouping by game, paging in the viewer.
import { stepIndex } from './shots.js';

export const PAGE = 120;

/** The query string of GET /api/screenshots; empty filters are left out. sort is 'newest' or 'game'. */
export function galleryQuery({ q = '', year = '', sort = 'game', offset = 0, limit = PAGE } = {}) {
  const p = new URLSearchParams();
  if (q.trim()) p.set('q', q.trim());
  if (year) p.set('year', String(year));
  if (sort === 'game') p.set('sort', 'game');
  if (offset > 0) p.set('offset', String(offset));
  p.set('limit', String(limit));
  return `?${p}`;
}

/**
 * Runs of neighbouring screenshots of the same game, for the "by game" layout. Each item keeps its index in the
 * whole list, which is what the viewer needs. Pages that were appended join the last run when it is the same game.
 */
export function groupRuns(list) {
  const runs = [];
  list.forEach((shot, index) => {
    const last = runs[runs.length - 1];
    if (last && last.gameId === shot.game_id) last.items.push({ shot, index });
    else runs.push({ gameId: shot.game_id, title: shot.game_title, items: [{ shot, index }] });
  });
  return runs;
}

/** How many matches are not loaded yet. */
export const remaining = (loaded, total) => Math.max(0, total - loaded);

/** "1 screenshot", "12 screenshots". */
export const shotsLabel = (n) => `${n} screenshot${n === 1 ? '' : 's'}`;

/**
 * The next index in the viewer. Past the last loaded picture while more exist on the server it asks for them
 * (more: true, index unchanged); otherwise it wraps like the drawer's viewer.
 */
export function stepAcross(cur, loaded, total, dir) {
  if (dir > 0 && cur === loaded - 1 && loaded < total) return { index: cur, more: true };
  return { index: stepIndex(cur, loaded, dir), more: false };
}

/** The empty-state sentence for the current filters. */
export function emptyText({ q = '', year = '' } = {}) {
  return q.trim() || year ? 'No screenshots match these filters.' : 'No screenshots yet. Open a game and add some in its Screenshots section.';
}
