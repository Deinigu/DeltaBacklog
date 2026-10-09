// The #/stats page: backlog size, finished per year, time waiting, top genres, finished vs dropped.
// All numbers come from stats.js (pure); this file only draws them.
import { h } from './util.js';
import { store } from './api.js';
import { backlogSize, backlogTime, finishedPerYear, timeInStatus, topGenres, finishedVsDropped, formatDays, niceScale } from './stats.js';

const SVGNS = 'http://www.w3.org/2000/svg';
function svg(tag, attrs, ...children) {
  const n = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs || {})) n.setAttribute(k, v);
  for (const c of children) if (c != null) n.append(c.nodeType ? c : document.createTextNode(String(c)));
  return n;
}

const statusLabel = (key) => store.statusByCode.get(key)?.label ?? key;
const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** A card with a heading and an optional one-line explanation. */
const card = (title, hint, ...body) => h('section.stat-card', {}, h('h2', {}, title), hint && h('p.hint', {}, hint), ...body);

/** Horizontal bars as plain HTML: the label and the number are real text, so no text alternative is needed. */
function barList(rows, { empty = 'Nothing to show yet.' } = {}) {
  if (!rows.length) return h('p.hint', {}, empty);
  const max = Math.max(...rows.map((r) => r.count));
  return h('ul.hbars', {}, rows.map((r) => h('li', {},
    h('span.hb-label', {}, r.label),
    h('span.hb-track', { 'aria-hidden': 'true' }, h('span.hb-fill', { style: `width:${Math.max(2, (r.count / max) * 100)}%` })),
    h('span.hb-count', {}, r.count))));
}

/** The hint under the chart about finished games that are not in it. */
function unplacedHint(periods, unknown) {
  const parts = [];
  if (periods > 0) parts.push(`${plural(periods, 'finished game was', 'finished games were')} played in childhood or always, so ${periods === 1 ? 'is' : 'are'} not in the chart`);
  if (unknown > 0) parts.push(`${plural(unknown, 'finished game has', 'finished games have')} nothing recorded about when ${unknown === 1 ? 'it was' : 'they were'} played`);
  return parts.length ? h('p.hint', {}, parts.join('; ') + '.') : null;
}

/** The SVG for a chart `W` units wide. Drawn at the width it is shown at, so its 12px text is never scaled down. */
function chartSvg(bars, W) {
  const H = 220, L = 42, R = 8, T = 10, B = 44;
  const pw = W - L - R, ph = H - T - B;
  const { max, ticks } = niceScale(Math.max(...bars.map((b) => b.count)));
  const slot = pw / bars.length, bw = Math.min(36, slot * 0.7);
  const y = (v) => T + ph - (v / max) * ph;
  const every = Math.max(1, Math.ceil(34 / slot)); // keep year labels from overlapping
  const g = svg('svg', {
    class: 'chart', viewBox: `0 0 ${W} ${H}`, width: W, height: H, 'aria-hidden': 'true', // the table below carries the numbers
  });
  for (const t of ticks) {
    g.append(svg('line', { class: 'grid', x1: L, x2: W - R, y1: y(t), y2: y(t) }));
    g.append(svg('text', { class: 'tick', x: L - 6, y: y(t) + 4, 'text-anchor': 'end' }, t));
  }
  bars.forEach((b, i) => {
    const cx = L + slot * i + slot / 2;
    const top = y(b.count);
    g.append(svg('rect', { class: 'bar', x: cx - bw / 2, y: top, width: bw, height: T + ph - top, rx: 3 }, svg('title', {}, `${b.year}: ${plural(b.count, 'game', 'games')}`)));
    if (b.count > 0 && bw >= 18) g.append(svg('text', { class: 'val', x: cx, y: top - 4, 'text-anchor': 'middle' }, b.count));
    if (i % every === 0) g.append(svg('text', { class: 'tick', x: cx, y: H - B + 16, 'text-anchor': 'middle' }, b.year));
  });
  g.append(svg('line', { class: 'axis', x1: L, x2: W - R, y1: T + ph, y2: T + ph }));
  g.append(svg('text', { class: 'axis-title', x: L + pw / 2, y: H - 6, 'text-anchor': 'middle' }, 'Year last played'));
  g.append(svg('text', { class: 'axis-title', transform: `translate(10 ${T + ph / 2}) rotate(-90)`, 'text-anchor': 'middle' }, 'Games finished'));
  return g;
}

/** Vertical bar chart (finished per year): inline SVG redrawn when its box changes width, plus a screen-reader table. */
function yearChart({ bars, periods, unknown, total }, cleanups) {
  if (!bars.length) return h('p.hint', {}, 'No finished game has a year played yet.');
  const box = h('div.chart-box', {});
  let width = 0;
  const fit = (w) => {
    w = Math.max(280, Math.min(720, Math.floor(w)));
    if (w === width) return;
    width = w;
    box.replaceChildren(chartSvg(bars, w));
  };
  fit(320);
  if (typeof ResizeObserver === 'function') {
    const ro = new ResizeObserver(() => { if (box.clientWidth) fit(box.clientWidth); });
    ro.observe(box);
    cleanups.push(() => ro.disconnect());
  }
  return h('div', {}, box, unplacedHint(periods, unknown),
    h('table.sr-only', {}, h('caption', {}, `Games finished per year (${total} in all)`),
      h('thead', {}, h('tr', {}, h('th', {}, 'Year'), h('th', {}, 'Games finished'))),
      h('tbody', {}, bars.map((b) => h('tr', {}, h('td', {}, b.year), h('td', {}, b.count))))));
}

/** One stacked bar: finished against dropped, with the numbers as text under it. */
function ratioBar({ finished, dropped, share }) {
  if (!finished && !dropped) return h('p.hint', {}, 'No finished or dropped games yet.');
  const pct = Math.round(share * 100);
  return h('div.ratio', {},
    h('div.ratio-bar', { role: 'img', 'aria-label': `${finished} finished, ${dropped} dropped: ${pct}% finished` },
      finished > 0 && h('span.r-fin', { style: `flex:${finished}` }),
      dropped > 0 && h('span.r-drop', { style: `flex:${dropped}` })),
    h('div.ratio-legend', {},
      h('span', {}, h('i.sw.r-fin'), `Finished ${finished}`),
      h('span', {}, h('i.sw.r-drop'), `Dropped ${dropped}`)),
    h('p.big', {}, `${pct}%`, h('small', {}, ` finished, ${100 - pct}% dropped`)));
}

/** "About 410 hours" plus how much of the backlog that covers; says how to get times when there are none. */
function backlogHours(t) {
  if (!t.known) return h('p.hint', {}, 'No time to beat yet. Run deltabacklog sync-igdb --enrich to read it from IGDB.');
  return h('div', {},
    h('p.big', {}, `${t.hours.toLocaleString()} hours`, h('small', {}, ' to beat the backlog')),
    h('p.hint', {}, t.unknown ? `Counted for ${t.known} of ${t.total} games; ${plural(t.unknown, 'game has', 'games have')} no time on IGDB.` : `Counted for all ${t.total} games.`));
}

function waitingTable(time) {
  const rows = time.rows.filter((r) => r.count > 0);
  if (!time.waiting) return h('p.hint', {}, 'No games are waiting.');
  const since = time.untouched > 0 ? `${plural(time.untouched, 'waiting game is', 'waiting games are')} untouched since the import on ${time.importDate}.` : '';
  if (!time.hasHistory) return h('p.hint', {}, `Not enough history to compare yet: the waiting games were all added or last changed around the same time (a fresh import, perhaps). Waiting times appear as you change games over the coming weeks. ${since}`.trim());
  return h('div', {},
    h('div.tscroll', {}, h('table.wait', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'Status'), h('th.num', {}, 'Games'), h('th.num', {}, 'Typical'), h('th.num', {}, 'Longest'))),
      h('tbody', {}, rows.map((r) => h('tr', {}, h('th', { scope: 'row' }, statusLabel(r.key)), h('td.num', {}, r.count), h('td.num', {}, formatDays(r.median)), h('td.num', {}, formatDays(r.longest))))))),
    h('p.hint', {}, `The games on the backlog or up next have been in your library for ${formatDays(time.backlogAgeMedian)} (typical).`),
    h('h3', {}, 'Waiting the longest'),
    h('ol.oldest', {}, rows.flatMap((r) => r.oldest.slice(0, 3).map((o) => ({ ...o, key: r.key })))
      .sort((a, b) => b.days - a.days).slice(0, 5)
      .map((o) => h('li', {}, h('a', { href: `#/library?q=${encodeURIComponent(o.title)}` }, o.title), h('small', {}, ` ${statusLabel(o.key)}, ${formatDays(o.days)}`)))),
    since && h('p.hint', {}, since));
}

export function renderStats(root) {
  const games = store.games;
  const size = backlogSize(games);
  const cleanups = [];
  root.replaceChildren(h('div.stats-page', {},
    h('h1', {}, 'Stats'),
    h('p.hint', {}, `Computed from the ${games.length} games in your library.`),
    h('div.stat-grid', {},
      card('Backlog', 'Games on the backlog or up next.',
        h('p.big', {}, size.total, h('small', {}, ` waiting (${size.byStatus.map((s) => `${s.count} ${statusLabel(s.key).toLowerCase()}`).join(', ')})`)),
        backlogHours(backlogTime(games)),
        h('h3', {}, 'By platform'), barList(size.byPlatform, { empty: 'The backlog is empty.' }),
          size.morePlatforms > 0 && h('p.hint', {}, `+${size.morePlatforms} more ${size.morePlatforms === 1 ? 'platform' : 'platforms'}`)),
      card('Finished per year', 'Each finished game counts in the last year you played it.', yearChart(finishedPerYear(games), cleanups)),
      card('Time waiting', 'Since a game was last changed, so a rough guide.', waitingTable(timeInStatus(games))),
      card('Most played genres', 'Games you have played, by genre.', barList(topGenres(games))),
      card('Finished or dropped', null, ratioBar(finishedVsDropped(games))))));
  return () => cleanups.forEach((f) => f());
}
