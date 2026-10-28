// Steam sync: the lists of the Steam page, built from the server's cache of the Steam library ({apps: [{app_id, name,
// playtime_min, last_played (unix s), game_id, source, ignored}]}). Pure (no DOM): unit-tested.
import { formatTime } from './ttb.js';

export const LOCALE = 'en-GB';
export const TABS = [
  { key: 'new', label: 'Not in library' },
  { key: 'matched', label: 'Matched' },
  { key: 'ignored', label: 'Ignored' },
];
export const FILTERS = [
  { key: 'all', label: 'All' },
  { key: 'played', label: 'Played' },
  { key: 'never', label: 'Never played' },
];

const SOURCE_LABEL = { link: 'by its Steam link', title: 'by its title', user: 'by you' };
/** How a match was made, for the Matched tab. */
export const sourceLabel = (source) => SOURCE_LABEL[source] || '';

/** Lower case without accents or punctuation, for the search box and the library picker. */
export const fold = (s) => String(s ?? '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/[^\p{L}\p{N}]+/gu, ' ').trim();

/** "12 h played", "45 min played" or "Never played". */
export const playedLabel = (minutes) => (minutes > 0 ? `${formatTime(minutes)} played` : 'Never played');

/** "Last played 12 Mar 2024" from unix seconds; '' without one. Read in UTC so the day does not depend on the time zone. */
export function lastPlayedLabel(unix, locale = LOCALE) {
  if (!(unix > 0)) return '';
  return `Last played ${new Date(unix * 1000).toLocaleDateString(locale, { timeZone: 'UTC', day: 'numeric', month: 'short', year: 'numeric' })}`;
}

const byName = (a, b) => a.app.name.localeCompare(b.app.name, 'en', { sensitivity: 'base' });
/** Most played first, then by name. */
const byHours = (a, b) => (b.app.playtime_min - a.app.playtime_min) || byName(a, b);

/**
 * What a tab lists.
 *  games  the library (store.games); info  the server's answer; tab  a TABS key; filter  a FILTERS key (only the
 *  "Not in library" tab uses it); query  the search box text.
 * Returns {rows, counts}: rows are {app, game} (game is the library game for the Matched tab, else null); counts are the
 * sizes of every tab and of the two filters of the first one, before the search box is applied.
 * An app matched to a game the page does not know yet (just deleted elsewhere) counts as not in the library.
 */
export function buildSteam(games, info, { tab = 'new', filter = 'all', query = '' } = {}) {
  const byId = new Map(games.map((g) => [g.id, g]));
  const lists = { new: [], matched: [], ignored: [] };
  const counts = { new: 0, matched: 0, ignored: 0, played: 0, never: 0 };
  for (const app of info?.apps || []) {
    const game = app.game_id != null ? byId.get(app.game_id) || null : null;
    if (game) { lists.matched.push({ app, game }); continue; }
    if (app.ignored) { lists.ignored.push({ app, game: null }); continue; }
    lists.new.push({ app, game: null });
    if (app.playtime_min > 0) counts.played++; else counts.never++;
  }
  for (const k of Object.keys(lists)) counts[k] = lists[k].length;
  let rows = lists[tab] || lists.new;
  if (tab === 'new') {
    if (filter === 'played') rows = rows.filter((r) => r.app.playtime_min > 0);
    else if (filter === 'never') rows = rows.filter((r) => !(r.app.playtime_min > 0));
  }
  const q = fold(query);
  if (q) rows = rows.filter((r) => fold(r.app.name).includes(q) || (r.game && fold(r.game.title).includes(q)));
  rows = [...rows].sort(tab === 'matched' ? byName : byHours);
  return { rows, counts };
}

/**
 * Library games for the "Match with a library game" picker: those whose title contains the search words, the ones that
 * start with them first, then by title. `limit` keeps the list short on a phone.
 */
export function matchCandidates(games, query, limit = 30) {
  const q = fold(query);
  if (!q) return [];
  const scored = [];
  for (const g of games) {
    const t = fold(g.title);
    const at = t.indexOf(q);
    if (at < 0) continue;
    scored.push({ g, rank: at === 0 ? 0 : 1 });
  }
  scored.sort((a, b) => a.rank - b.rank || a.g.title.localeCompare(b.g.title, 'en', { sensitivity: 'base' }));
  return scored.slice(0, limit).map((s) => s.g);
}

/** The line under the sync button. */
export function syncNote(info) {
  if (!info?.configured && !info?.synced_at) return 'Steam is not set up: add STEAM_API_KEY and STEAM_ID to .env and restart the app (see the user manual, Steam sync).';
  if (!info.synced_at) return 'Press Sync with Steam to read your Steam library and the hours you played.';
  const t = new Date(info.synced_at);
  return Number.isNaN(t.getTime()) ? '' : `Last synced ${t.toLocaleString(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })}.`;
}
