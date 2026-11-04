// The #/stats page: summary tiles, then cards for time spent, time left, years, outcomes, shortest wins, composition, series.
// All numbers come from stats.js (pure); this file only draws them. Every hour figure is an estimate and says how many games it rests on.
import { h, plural } from './util.js';
import { store } from './api.js';
import { formatNumber, formatCalendarDate } from './format.js';
import { openDrawer } from './drawer.js';
import { formatTime, ttbMinutes } from './ttb.js';
import {
  backlogSize, timeInStatus, topGenres, finishedVsDropped, formatDays, niceScale, MIN_GROUP, DEFAULT_HOURS_PER_WEEK,
  finishedTime, timeLeft, outcomesBy, shortestWins, composition, yearByYear, seriesProgress, cleanHoursPerWeek, monthsToClear, formatDuration, currentSection,
} from './stats.js';

const SVGNS = 'http://www.w3.org/2000/svg';
function svg(tag, attrs, ...children) {
  const n = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs || {})) n.setAttribute(k, v);
  for (const c of children) if (c != null) n.append(c.nodeType ? c : document.createTextNode(String(c)));
  return n;
}

const statusLabel = (key) => store.statusByCode.get(key)?.label ?? key;
const num = formatNumber;
const HOURS_KEY = 'deltabacklog.statsHoursPerWeek';

/** A card with a heading and an optional one-line explanation. `id` is the jump target; `wide` spans both columns on a desktop. */
const card = (id, title, hint, ...body) => h('section.stat-card', { 'data-sec': id, tabindex: '-1' }, h('h2', {}, title), hint && h('p.hint', {}, hint), ...body);
const wide = (c) => { c.classList.add('wide'); return c; };
const tall = (c) => { c.classList.add('tall'); return c; };

/** Horizontal bars as plain HTML: the label and the number are real text, so no text alternative is needed. `text` formats the number. */
function barList(rows, { empty = 'Nothing to show yet.', value = (r) => r.count, text = (r) => r.count, cls = '' } = {}) {
  if (!rows.length) return h('p.hint', {}, empty);
  const max = Math.max(...rows.map(value), 1);
  return h(`ul.hbars${cls ? '.' + cls : ''}`, {}, rows.map((r) => h('li', {},
    h('span.hb-label', { title: r.label }, r.label),
    h('span.hb-track', { 'aria-hidden': 'true' }, h('span.hb-fill', { style: `width:${Math.max(2, (value(r) / max) * 100)}%` })),
    h('span.hb-count', {}, text(r)))));
}

/** "≈ 1,240 h across 142 of 189 finished games (the rest have no time to beat)": what an hours figure rests on. */
function basis(e, noun) {
  if (!e.total) return `No ${noun} yet.`;
  if (!e.known) return `None of the ${plural(e.total, noun.replace(/s$/, ''), noun)} has a time to beat yet.`;
  if (e.known === e.total) return `Rests on the time to beat of all ${plural(e.total, noun.replace(/s$/, ''), noun)}.`;
  return `Rests on ${e.known} of ${e.total} ${noun}; the other ${e.total - e.known} ${e.total - e.known === 1 ? 'has' : 'have'} no time to beat, so the real figure is higher.`;
}
const approx = (e) => `≈ ${num(e.hours)} h`;

/** The hint under a chart about finished games that are not in it. */
function unplacedHint(periods, unknown) {
  const parts = [];
  if (periods > 0) parts.push(`${plural(periods, 'finished game was', 'finished games were')} played in childhood or always, so ${periods === 1 ? 'is' : 'are'} not in the chart`);
  if (unknown > 0) parts.push(`${plural(unknown, 'finished game has', 'finished games have')} nothing recorded about when ${unknown === 1 ? 'it was' : 'they were'} played`);
  return parts.length ? h('p.hint', {}, parts.join('; ') + '.') : null;
}

/**
 * A grouped vertical bar chart `W` units wide: rows = [{label, values: [..]}], one value per series ({name, cls}).
 * Drawn at the width it is shown at, so its 12px text is never scaled down. A 2px gap separates bars of one group.
 */
function chartSvg(rows, series, W, xTitle, yTitle, fmt) {
  const H = 220, L = 46, R = 8, T = 12, B = 44;
  const pw = W - L - R, ph = H - T - B;
  const { max, ticks } = niceScale(Math.max(...rows.flatMap((r) => r.values)));
  const slot = pw / rows.length, gw = Math.min(40 * series.length, slot * 0.8), bw = (gw - 2 * (series.length - 1)) / series.length;
  const y = (v) => T + ph - (v / max) * ph;
  const every = Math.max(1, Math.ceil(34 / slot));
  const g = svg('svg', { class: 'chart', viewBox: `0 0 ${W} ${H}`, width: W, height: H, 'aria-hidden': 'true' }); // the table below carries the numbers
  for (const t of ticks) {
    g.append(svg('line', { class: 'grid', x1: L, x2: W - R, y1: y(t), y2: y(t) }));
    g.append(svg('text', { class: 'tick', x: L - 6, y: y(t) + 4, 'text-anchor': 'end' }, fmt(t)));
  }
  const first = rows.map((r) => r.values[0]);
  const topValue = Math.max(...first), lastShown = first.findLastIndex((v) => v > 0);
  rows.forEach((r, i) => {
    const cx = L + slot * i + slot / 2;
    r.values.forEach((v, k) => {
      const x = cx - gw / 2 + k * (bw + 2), top = y(v);
      if (v > 0) g.append(svg('rect', { class: `bar ${series[k].cls}`, x, y: top, width: bw, height: T + ph - top, rx: Math.min(3, bw / 2) }));
      // one series: only the highest bar and the latest year carry their value (the rest is in the axis, the tooltip and the table)
      if (series.length === 1 && v > 0 && bw >= 12 && (v === topValue || i === lastShown)) g.append(svg('text', { class: 'val', x: x + bw / 2, y: top - 4, 'text-anchor': 'middle' }, fmt(v)));
    });
    // the hover / touch target is the whole column, not just the (possibly 8px tall) bar
    g.append(svg('rect', { class: 'hit', x: cx - slot / 2, y: T, width: slot, height: ph, fill: 'transparent' },
      svg('title', {}, `${r.label}: ${r.values.map((v, k) => `${series[k].name} ${fmt(v)}`).join(', ')}`)));
    if (i % every === 0) g.append(svg('text', { class: 'tick', x: cx, y: H - B + 16, 'text-anchor': 'middle' }, r.label));
  });
  g.append(svg('line', { class: 'axis', x1: L, x2: W - R, y1: T + ph, y2: T + ph }));
  g.append(svg('text', { class: 'axis-title', x: L + pw / 2, y: H - 6, 'text-anchor': 'middle' }, xTitle));
  g.append(svg('text', { class: 'axis-title', transform: `translate(10 ${T + ph / 2}) rotate(-90)`, 'text-anchor': 'middle' }, yTitle));
  return g;
}

/** A bar chart that redraws when its box changes width, with a legend (two series or more) and a screen-reader table. */
function barChart({ rows, series, xTitle, yTitle, caption, fmt = (v) => v }, cleanups) {
  const box = h('div.chart-box', {});
  let width = 0;
  const fit = (w) => {
    w = Math.max(280, Math.min(720, Math.floor(w)));
    if (w === width) return;
    width = w;
    box.replaceChildren(chartSvg(rows, series, w, xTitle, yTitle, fmt));
  };
  fit(320);
  if (typeof ResizeObserver === 'function') {
    const ro = new ResizeObserver(() => { if (box.clientWidth) fit(box.clientWidth); });
    ro.observe(box);
    cleanups.push(() => ro.disconnect());
  }
  return h('div', {},
    series.length > 1 && h('div.ratio-legend.chart-legend', {}, series.map((s) => h('span', {}, h('i', { class: `sw ${s.cls}` }), s.name))),
    box,
    h('table.sr-only', {}, h('caption', {}, caption),
      h('thead', {}, h('tr', {}, h('th', {}, xTitle), series.map((s) => h('th', {}, s.name)))),
      h('tbody', {}, rows.map((r) => h('tr', {}, h('td', {}, r.label), r.values.map((v) => h('td', {}, fmt(v))))))));
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

/** Finished against dropped for each genre / platform: a small stacked bar and the finish rate as text. */
function outcomeRows(rows, what) {
  if (!rows.length) return h('p.hint', {}, `No ${what} has ${MIN_GROUP} finished or dropped games yet.`);
  return h('ul.outcomes', {}, rows.map((r) => h('li', {},
    h('span.hb-label', { title: r.label }, r.label),
    h('span.ratio-bar.mini', { role: 'img', 'aria-label': `${r.finished} finished, ${r.dropped} dropped` },
      r.finished > 0 && h('span.r-fin', { style: `flex:${r.finished}` }), r.dropped > 0 && h('span.r-drop', { style: `flex:${r.dropped}` })),
    h('span.oc-rate', {}, `${Math.round(r.rate * 100)}%`, h('small', {}, ` ${r.finished}/${r.total}`)))));
}

function waitingTable(time) {
  const rows = time.rows.filter((r) => r.count > 0);
  if (!time.waiting) return h('p.hint', {}, 'No games are waiting.');
  const since = time.untouched > 0 ? `${plural(time.untouched, 'waiting game is', 'waiting games are')} untouched since the import on ${formatCalendarDate(time.importDate)}.` : '';
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

const loadHours = () => { try { return cleanHoursPerWeek(localStorage.getItem(HOURS_KEY) ?? DEFAULT_HOURS_PER_WEEK); } catch { return DEFAULT_HOURS_PER_WEEK; } };
const saveHours = (n) => { try { localStorage.setItem(HOURS_KEY, String(n)); } catch { /* private window: it just is not remembered */ } };

/** Hours left in Up next and Backlog, and how long that takes at N hours a week (N is remembered on this device). */
function timeLeftCard(left) {
  const line = (label, e) => h('li', {}, h('span.hb-label', {}, label), h('b', {}, e.known ? approx(e) : 'no times'), h('small', {}, ` ${e.known} of ${e.total}`));
  const out = h('p.big', {});
  const input = h('input', { id: 'stats-hpw', type: 'number', inputmode: 'numeric', min: '1', max: '168', step: '1', value: String(loadHours()) });
  const update = () => {
    const perWeek = cleanHoursPerWeek(input.value);
    const when = formatDuration(monthsToClear(left.both.hours, perWeek));
    out.replaceChildren(when || 'Nothing to clear', h('small', {}, when ? `to play the ≈ ${num(left.both.hours)} h of Up next and Backlog at ${perWeek} h a week` : 'No time to beat is known for the games waiting.'));
  };
  input.addEventListener('input', update);
  input.addEventListener('change', () => { const n = cleanHoursPerWeek(input.value); input.value = String(n); saveHours(n); update(); });
  update();
  return card('left', 'Time left', 'Hours to clear the games still waiting. An estimate from the time to beat.',
    h('ul.lines', {}, line('Up next', left.upNext), line('Backlog', left.backlog)),
    h('p.hint', {}, basis(left.both, 'waiting games')),
    h('div.hpw', {}, h('label', { for: 'stats-hpw' }, 'I play'), input, h('span', {}, 'hours a week')),
    out);
}

function tile(value, label, sub) {
  return h('div.tile-stat', {}, h('b', {}, value), h('span', {}, label), sub && h('small', {}, sub));
}

const SECTIONS = [['finished', 'Finished'], ['left', 'Time left'], ['backlog', 'Backlog'], ['years', 'Years'], ['outcomes', 'Outcomes'],
  ['short', 'Shortest'], ['series', 'Series'], ['library', 'Library'], ['waiting', 'Waiting']];

export function renderStats(root) {
  const games = store.games;
  const size = backlogSize(games);
  const cleanups = [];
  const fin = finishedTime(games);
  const left = timeLeft(games);
  const years = yearByYear(games);
  const comp = composition(games);
  const sp = seriesProgress(games);
  const short = shortestWins(games);
  const finishedCount = games.filter((g) => g.status === 'finished').length;

  const finishedCard = card('finished', 'Time spent finishing games', 'About how long the games you finished take to beat, from their time to beat (not the time you actually played).',
    fin.all.known ? h('p.big', {}, approx(fin.all), h('small', {}, `across ${fin.all.known} of ${fin.all.total} finished games`)) : null,
    h('p.hint', {}, basis(fin.all, 'finished games')),
    fin.all.known ? h('div', {},
      h('h3', {}, 'Hours by genre'), barList(fin.genres, { value: (r) => r.minutes, text: (r) => [`${num(r.hours)} h`, r.known < r.count && h('small', { title: `${r.known} of ${r.count} finished games have a time to beat` }, ` ${r.known}/${r.count}`)], cls: 'wide-count' }),
      h('h3', {}, 'Hours by platform'), barList(fin.platforms, { value: (r) => r.minutes, text: (r) => [`${num(r.hours)} h`, r.known < r.count && h('small', { title: `${r.known} of ${r.count} finished games have a time to beat` }, ` ${r.known}/${r.count}`)], cls: 'wide-count' }),
      h('p.hint', {}, 'The top six. A game with several genres or platforms counts for each, so the groups overlap. A small 9/11 means only 9 of the 11 finished games of the group have a time to beat, so that bar is low.')) : null);

  const under = fin.perYear.filter((r) => r.known < r.count).map((r) => r.year);
  const yearsCard = wide(card('years', 'Year by year',
    years.mostActive ? `Your most active year was ${years.mostActive.year}, with ${plural(years.mostActive.played, 'game', 'games')} played.` : 'Games played and finished in each year.',
    years.rows.length ? barChart({
      rows: years.rows.map((r) => ({ label: String(r.year), values: [r.played, r.finished] })),
      series: [{ name: 'Games played', cls: 'bar-1' }, { name: 'Games finished', cls: 'bar-2' }],
      xTitle: 'Year', yTitle: 'Games', caption: 'Games played and finished per year',
    }, cleanups) : h('p.hint', {}, 'No game has a year played yet.'),
    h('p.hint', {}, 'A game played in several years counts in each of them under played, but only once under finished, in the last year you played it.'),
    years.childhood || years.always ? h('p.hint', {}, [years.childhood && `${plural(years.childhood, 'game was', 'games were')} played in childhood`, years.always && `${plural(years.always, 'game is', 'games are')} played on and off (always)`].filter(Boolean).join('; ') + ': these are not a calendar year, so they are not in the chart.') : null,
    fin.perYear.length && fin.all.known ? h('div', {},
      h('h3', {}, 'Estimated hours of the games finished each year'),
      barChart({
        rows: fin.perYear.map((r) => ({ label: String(r.year), values: [r.hours] })),
        series: [{ name: 'Hours', cls: 'bar-1' }], xTitle: 'Year last played', yTitle: 'Hours (estimate)',
        caption: 'Estimated hours of the games finished per year', fmt: (v) => num(v),
      }, cleanups),
      unplacedHint(fin.periods, fin.unknown),
      h('p.hint', {}, 'Each finished game counts once, in the last year you played it.' + (under.length ? ` The years ${under.join(', ')} have finished games without a time to beat, so they are underestimated.` : ''))) : null));

  const outcomesCard = wide(card('outcomes', 'Finished or dropped', `Genres and platforms with at least ${MIN_GROUP} finished or dropped games. The percentage is the share finished.`,
    ratioBar(finishedVsDropped(games)),
    h('div.two-cols', {},
      h('div', {}, h('h3', {}, 'By genre'), outcomeRows(outcomesBy(games, 'genres'), 'genre')),
      h('div', {}, h('h3', {}, 'By platform'), outcomeRows(outcomesBy(games, 'platforms'), 'platform')))));

  const shortCard = card('short', 'Shortest wins', 'The 10 shortest games waiting in Backlog or Up next, by time to beat. Tap one to open it.',
    short.length ? h('ol.shortest', {}, short.map((g) => h('li', {}, h('button.short-item', { type: 'button', onclick: () => openDrawer(g) },
      h('span.s-title', {}, g.title), h('small', {}, `${statusLabel(g.status)} · ${formatTime(ttbMinutes(g))}`))))) : h('p.hint', {}, 'No waiting game has a time to beat yet.'));

  const libraryCard = card('library', 'Library', `The ${games.length} games in your library.`,
    h('h3', {}, 'By ownership'), barList(comp.ownership),
    h('h3', {}, 'By platform'), barList(comp.platforms.slice(0, 8)),
    comp.platforms.length > 8 && h('p.hint', {}, `+${comp.platforms.length - 8} more platforms. A game on two platforms counts for both.`),
    h('h3', {}, 'By release decade'), barList(comp.decades),
    comp.noYear > 0 && h('p.hint', {}, `${plural(comp.noYear, 'game has', 'games have')} no release year.`));

  const seriesCard = card('series', 'Series', 'Series with at least two games in your library. Games of a series that you do not have are not counted.',
    sp.total ? h('div', {},
      h('p.big', {}, `${sp.completed} of ${sp.total}`, h('small', {}, `series completed (every game finished); ${sp.inProgress} in progress, ${sp.notStarted} not started`)),
      sp.closest.length ? h('div', {}, h('h3', {}, 'Closest to done'),
        h('ul.lines', {}, sp.closest.map((s) => h('li', {}, h('span.hb-label', {}, s.name), h('b', {}, `${s.finished}/${s.total}`))))) : null,
      h('p.hint', {}, h('a', { href: '#/series' }, 'Open the Series page'))) : h('p.hint', {}, 'No series with two games yet.'));

  const backlogCard = card('backlog', 'Backlog', 'Games on the backlog or up next.',
    h('p.big', {}, size.total, h('small', {}, ` waiting (${size.byStatus.map((s) => `${s.count} ${statusLabel(s.key).toLowerCase()}`).join(', ')})`)),
    h('h3', {}, 'By platform'), barList(size.byPlatform, { empty: 'The backlog is empty.' }),
    size.morePlatforms > 0 && h('p.hint', {}, `+${size.morePlatforms} more ${size.morePlatforms === 1 ? 'platform' : 'platforms'}`));

  // The chips stick under the top of the screen on a phone (CSS); the one for the section being read is marked.
  let picked = () => {}; // set below: a tapped chip is the current one until the person scrolls by hand
  const jump = h('nav.jump', { 'aria-label': 'Jump to a section' }, SECTIONS.map(([id, label]) => h('button', {
    type: 'button', 'data-to': id,
    onclick: () => { picked(id); const s = root.querySelector(`[data-sec="${id}"]`); if (s) { s.scrollIntoView({ behavior: 'smooth', block: 'start' }); s.focus({ preventScroll: true }); } },
  }, label)));

  root.replaceChildren(h('div.stats-page', {},
    h('h1', {}, 'Stats'),
    h('p.hint', {}, `Computed from the ${games.length} games in your library. Hours are estimates: a game's time is its Main + extras time to beat, else the quickest, else the complete one; games with no time add nothing.`),
    h('div.stat-tiles', {},
      tile(num(games.length), 'games'),
      tile(num(finishedCount), 'finished'),
      tile(fin.all.known ? approx(fin.all) : '—', 'finished, estimated', fin.all.known ? `${fin.all.known} of ${fin.all.total} timed` : 'no times yet'),
      tile(left.both.known ? approx(left.both) : '—', 'left to play', left.both.known ? `${left.both.known} of ${left.both.total} timed` : 'no times yet')),
    jump,
    h('div.stat-grid', {},
      tall(finishedCard), timeLeftCard(left), backlogCard, yearsCard, outcomesCard, shortCard, seriesCard, tall(libraryCard),
      card('genres', 'Most played genres', 'Games you have played, by genre.', barList(topGenres(games))),
      card('waiting', 'Time waiting', 'Since a game was last changed, so a rough guide.', waitingTable(timeInStatus(games))))));

  {
    const secs = SECTIONS.map(([id]) => root.querySelector(`[data-sec="${id}"]`)).filter(Boolean);
    let shown = null; // what the chips show now: a section id, or null while they are not sticky
    let tapped = null; // a chip the person pressed: it stays the current one until they scroll by hand
    const mark = () => {
      // Only the phone layout has sticky chips and a highlight; elsewhere nothing is announced as current
      const sticky = getComputedStyle(jump).position === 'sticky';
      let id = null;
      if (sticky) {
        const line = jump.getBoundingClientRect().bottom + 16; // more than the cards' scroll margin, so a jumped-to section counts
        const atEnd = window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 2;
        id = tapped ?? currentSection(secs.map((el) => { const b = el.getBoundingClientRect(); return { id: el.dataset.sec, top: b.top, bottom: b.bottom }; }), line, atEnd);
      }
      if (id === shown) return;
      shown = id;
      for (const btn of jump.children) {
        const on = btn.dataset.to === id;
        if (on) btn.setAttribute('aria-current', 'true'); else btn.removeAttribute('aria-current');
        if (on) jump.scrollLeft = btn.offsetLeft - (jump.clientWidth - btn.offsetWidth) / 2; // keep the chip in view in the row
      }
    };
    let raf = 0;
    const soon = () => { if (!raf) raf = requestAnimationFrame(() => { raf = 0; mark(); }); }; // a scroll event per frame at most: 9 rects
    const free = () => { tapped = null; soon(); }; // a wheel, touch or key press ends a tapped chip's hold
    picked = (id) => { tapped = id; mark(); };
    window.addEventListener('scroll', soon, { passive: true });
    for (const ev of ['wheel', 'touchstart', 'keydown']) window.addEventListener(ev, free, { passive: true });
    const mq = matchMedia('(max-width: 899px)');
    mq.addEventListener('change', soon);
    cleanups.push(() => {
      cancelAnimationFrame(raf);
      window.removeEventListener('scroll', soon);
      for (const ev of ['wheel', 'touchstart', 'keydown']) window.removeEventListener(ev, free);
      mq.removeEventListener('change', soon);
    });
    mark();
  }
  return () => cleanups.forEach((f) => f());
}
