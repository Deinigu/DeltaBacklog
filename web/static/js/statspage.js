// The #/stats page: backlog size, finished per year, time waiting, top genres, finished vs dropped.
// All numbers come from stats.js (pure); this file only draws them.
import { h } from './util.js';
import { store } from './api.js';
import { backlogSize, finishedPerYear, timeInStatus, topGenres, finishedVsDropped, formatDays, niceScale } from './stats.js';

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

/** Vertical bar chart (finished per year) as inline SVG with labelled axes and a screen-reader table. */
function yearChart({ bars, unknown, total }) {
  if (!bars.length) return h('p.hint', {}, 'No finished game has a year played yet.');
  const W = 360, H = 220, L = 42, R = 8, T = 10, B = 44;
  const pw = W - L - R, ph = H - T - B;
  const { max, ticks } = niceScale(Math.max(...bars.map((b) => b.count)));
  const slot = pw / bars.length, bw = Math.min(36, slot * 0.7);
  const y = (v) => T + ph - (v / max) * ph;
  const every = Math.max(1, Math.ceil(34 / slot)); // keep year labels from overlapping
  const g = svg('svg', {
    class: 'chart', viewBox: `0 0 ${W} ${H}`, role: 'img',
    'aria-label': `Games finished per year, ${bars[0].year} to ${bars[bars.length - 1].year}: ${bars.map((b) => `${b.year} ${b.count}`).join(', ')}`,
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
  return h('div', {}, g,
    unknown > 0 && h('p.hint', {}, `${plural(unknown, 'finished game has', 'finished games have')} no year recorded and ${unknown === 1 ? 'is' : 'are'} not in the chart.`),
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

function waitingTable(time) {
  const rows = time.rows.filter((r) => r.count > 0);
  if (!rows.length) return h('p.hint', {}, 'No games are waiting.');
  if (!time.hasHistory) return h('p.hint', {}, 'Every waiting game was added or changed within the last week (a fresh import, perhaps), so there is nothing to compare yet. Waiting times appear as time passes.');
  return h('div', {},
    h('div.tscroll', {}, h('table.wait', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'Status'), h('th.num', {}, 'Games'), h('th.num', {}, 'Typical'), h('th.num', {}, 'Longest'))),
      h('tbody', {}, rows.map((r) => h('tr', {}, h('th', { scope: 'row' }, statusLabel(r.key)), h('td.num', {}, r.count), h('td.num', {}, formatDays(r.median)), h('td.num', {}, formatDays(r.longest))))))),
    h('p.hint', {}, `The games on the backlog or up next have been in your library for ${formatDays(time.backlogAgeMedian)} (typical).`),
    h('h3', {}, 'Waiting the longest'),
    h('ol.oldest', {}, rows.flatMap((r) => r.oldest.slice(0, 3).map((o) => ({ ...o, key: r.key })))
      .sort((a, b) => b.days - a.days).slice(0, 5)
      .map((o) => h('li', {}, h('a', { href: `#/library?q=${encodeURIComponent(o.title)}` }, o.title), h('small', {}, ` ${statusLabel(o.key)}, ${formatDays(o.days)}`)))));
}

export function renderStats(root) {
  const games = store.games;
  const size = backlogSize(games);
  root.replaceChildren(h('div.stats', {},
    h('h1', {}, 'Stats'),
    h('p.hint', {}, `Computed from the ${games.length} games in your library.`),
    h('div.stat-grid', {},
      card('Backlog', 'Games on the backlog or up next.',
        h('p.big', {}, size.total, h('small', {}, ` waiting (${size.byStatus.map((s) => `${s.count} ${statusLabel(s.key).toLowerCase()}`).join(', ')})`)),
        h('h3', {}, 'By platform'), barList(size.byPlatform, { empty: 'The backlog is empty.' })),
      card('Finished per year', 'Each finished game counts in the last year you played it.', yearChart(finishedPerYear(games))),
      card('Time waiting', 'Since a game was last changed, so a rough guide.', waitingTable(timeInStatus(games))),
      card('Most played genres', 'Games you have played, by genre.', barList(topGenres(games))),
      card('Finished or dropped', null, ratioBar(finishedVsDropped(games))))));
}
