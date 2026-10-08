// Series progress: group games by series and count how far through each one the user is. Pure (no DOM): unit-tested.
// Definitions (see the user manual): a game is *finished* when its status is Finished; it is *played* when it is
// finished, has any year or period played, or its status says it was started (Playing, Evergreen, Hold, Dropped).

const STARTED = new Set(['playing', 'playing_evergreen', 'evergreen', 'hold', 'dropped']);
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

export const isFinished = (g) => g.status === 'finished';

export const isPlayed = (g) => isFinished(g) || STARTED.has(g.status) || g.years.length > 0 || g.periods.length > 0;

/** Still worth playing: never played, and not marked Not Interested. */
export const isLeft = (g) => !isPlayed(g) && g.status !== 'not_interested';

/** Games of one series: release year ascending (unknown year last), then title. */
export function compareInSeries(a, b) {
  const ya = a.release_year ?? Infinity, yb = b.release_year ?? Infinity;
  if (ya !== yb) return ya < yb ? -1 : 1;
  return collator.compare(a.title, b.title) || a.id - b.id;
}

const keyOf = (name) => name.trim().toLowerCase();

/**
 * Every series that has at least one game, as
 *  { key, name, games (ordered), total, finished, played, left }
 * `played` includes the finished games. Series are ordered by name; names that differ only in case are one series.
 */
export function buildSeries(games) {
  const map = new Map();
  for (const g of games) {
    if (!g.series || !g.series.trim()) continue;
    const key = keyOf(g.series);
    let s = map.get(key);
    if (!s) map.set(key, (s = { key, name: g.series.trim(), games: [] }));
    s.games.push(g);
  }
  const out = [...map.values()];
  for (const s of out) {
    s.games.sort(compareInSeries);
    s.total = s.games.length;
    s.finished = s.games.filter(isFinished).length;
    s.played = s.games.filter(isPlayed).length;
    s.left = s.games.filter(isLeft).length;
  }
  return out.sort((a, b) => collator.compare(a.name, b.name));
}

/** Series that still have a game left to play. */
export const withLeft = (series) => series.filter((s) => s.left > 0);

/** Bar segment widths in percent: finished, played but not finished, and the rest. */
export function barShares(s) {
  if (!s.total) return { finished: 0, played: 0, rest: 100 };
  const finished = (s.finished / s.total) * 100;
  const played = ((s.played - s.finished) / s.total) * 100;
  return { finished, played, rest: 100 - finished - played };
}

/** "3 finished · 4 played · 5 games" for the series header and screen readers. */
export function progressText(s) {
  return `${s.finished} finished · ${s.played} played · ${s.total} ${s.total === 1 ? 'game' : 'games'}`;
}

/** Where a game stands in its series: { position, total, name } (position follows release order), or null. */
export function seriesPosition(game, games) {
  if (!game.series || !game.series.trim()) return null;
  const s = buildSeries(games).find((x) => x.key === keyOf(game.series));
  if (!s || s.total < 2) return null;
  const i = s.games.findIndex((g) => g.id === game.id);
  return i < 0 ? null : { position: i + 1, total: s.total, name: s.name };
}
