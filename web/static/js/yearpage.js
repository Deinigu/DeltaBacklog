// Year in review page (#/years/<year>): what was played, a breakdown, and a way into that year's ranking.
import { h, toast, gradientFor, initials, lazyImg, retargetImg } from './util.js';
import { store, coverURL, listRankings, createRanking } from './api.js';
import { openDrawer } from './drawer.js';
import { groupYear, gamesOfYear, breakdown, yearCounts, neighbours, yearRankingName, findYearRanking } from './yearreview.js';

const thisYear = () => new Date().getFullYear();

/** Latest year with games (or this year when nothing has been played yet): where "#/years" lands. */
export function defaultYear() {
  const counts = yearCounts(store.games);
  return counts.length ? counts[0].year : thisYear();
}

export function renderYear(root, year) {
  let alive = true;
  const counts = yearCounts(store.games);
  const nb = neighbours(counts, year, thisYear());
  const groups = groupYear(store.games, year);
  const pool = gamesOfYear(store.games, year);
  const total = pool.length;

  const nav = (to, label, glyph) => to
    ? h('a.btn.year-step', { href: `#/years/${to}`, 'aria-label': `${label}: ${to}` }, glyph)
    : h('span.btn.year-step.off', { 'aria-hidden': 'true' }, glyph);
  const stripYears = counts.some((c) => c.year === year) ? counts : [{ year, count: 0 }, ...counts].sort((a, b) => b.year - a.year);
  const strip = h('nav.year-strip', { 'aria-label': 'Years' },
    ...stripYears.map((c) => h('a.chip', { href: `#/years/${c.year}`, 'aria-current': c.year === year ? 'page' : null }, String(c.year), h('span.c', {}, c.count))));

  const head = h('header.year-head', {},
    nav(nb.prev, 'Previous year', '‹'),
    h('div.year-title', {}, h('h1', {}, String(year)), h('p.hint', {}, total ? `${total} game${total === 1 ? '' : 's'} played` : 'No games played')),
    nav(nb.next, 'Next year', '›'));

  const body = h('div.year-body');
  root.replaceChildren(h('div.year-page', {}, head, strip, body));

  if (!total) {
    body.append(h('div.empty', {}, `Nothing is recorded for ${year}.`, h('br'), 'A game counts for a year when it has that year in its “Years played” (moving a game to Playing or Finished adds the current year).'));
    return () => { alive = false; };
  }

  // ---- summary ----
  const tile = (g) => h('div.year-stat', {}, h('b', {}, String(g.games.length)), h('span', {}, g.label));
  const bars = (title, rows) => {
    const top = rows.slice(0, 6);
    const max = top[0]?.count || 1;
    return h('section.year-box', {}, h('h2', {}, title),
      top.length ? h('ul.year-bars', {}, ...top.map((r) => h('li', {}, h('span.year-bar-name', {}, r.name), h('span.year-bar', {}, h('i', { style: `width:${Math.max(6, (r.count / max) * 100)}%` })), h('span.year-bar-n', {}, String(r.count))))) : h('p.hint', {}, 'None recorded'));
  };
  body.append(
    h('div.year-stats', {}, ...groups.map(tile)),
    h('div.year-two', {}, bars('Platforms', breakdown(pool, (g) => g.platforms)), bars('Genres', breakdown(pool, (g) => g.genres))));

  // ---- ranking: opens the ranking of this year, or makes it with the year's games ----
  const rankBtn = h('button.btn.primary', { type: 'button', onclick: rankThisYear }, 'Rank this year');
  body.append(h('section.year-box.tiers-box', {},
    h('h2', {}, 'Ranking'),
    h('p.hint', {}, `Put the ${total} game${total === 1 ? '' : 's'} of ${year} in your own tiers, with big posters, and export the result as a picture.`),
    rankBtn));
  async function rankThisYear() {
    rankBtn.disabled = true;
    try {
      const existing = findYearRanking(await listRankings(), year);
      const r = existing || await createRanking({ name: yearRankingName(year), preset: 'sabcdf', game_ids: pool.map((g) => g.id) });
      if (alive) location.hash = `#/rankings/${r.id}`;
    } catch { toast('Could not open the ranking'); rankBtn.disabled = false; }
  }

  // ---- what was played, by outcome ----
  const gameRow = (g) => {
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    return h('button.yrow', { type: 'button', 'data-id': g.id, onclick: () => openDrawer(g), style: st ? `--st:${st.color}` : null },
      h('span.ythumb', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title))),
      h('span.yinfo', {}, h('span.yname', {}, g.title), h('span.ysub', {}, [st?.label, g.platforms.slice(0, 2).join(', ')].filter(Boolean).join(' · '))));
  };
  body.append(h('div.ygroups', {}, ...groups.filter((g) => g.games.length).map((grp) =>
    h('section.year-box', {}, h('h2', {}, grp.label, h('span.c', {}, String(grp.games.length))), h('div.ylist', {}, ...grp.games.map(gameRow))))));

  // A game got a new cover: its row follows (see retargetImg).
  const onGameChanged = (e) => {
    const g = e?.detail?.id && store.byId.get(e.detail.id);
    if (g) for (const img of body.querySelectorAll(`[data-id="${g.id}"] img`)) retargetImg(img, coverURL(g));
  };
  window.addEventListener('games-changed', onGameChanged);

  return () => { alive = false; window.removeEventListener('games-changed', onGameChanged); };
}
