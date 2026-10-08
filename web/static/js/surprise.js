// "Surprise me": a sheet that narrows the games a Library or view shows by mood, then deals one at random.
// The rules and the randomness live in pick.js (pure, tested); this file is only the sheet.
import { h, toast, gradientFor, initials, lazyImg } from './util.js';
import { store, coverURL, patchGame } from './api.js';
import { makeSheet } from './add.js';
import { playedBadges, playedSummary } from './years.js';
import { badgeStyle } from './colors.js';
import { hasNoFilters, buildPool, applyMood, moodCounts, emptyMood, pickOne } from './pick.js';

// Games skipped for the rest of the browser session (they survive a reload, not a closed tab).
const SKIP_KEY = 'surprise-skipped';
function loadSkipped() {
  try { return new Set(JSON.parse(sessionStorage.getItem(SKIP_KEY) || '[]')); } catch { return new Set(); }
}
function saveSkipped(set) {
  try { sessionStorage.setItem(SKIP_KEY, JSON.stringify([...set])); } catch { /* private mode: skips last until the page closes */ }
}
const skipped = loadSkipped();

const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

/** What the pool is, in words: "the 12 Up next games", "the 40 games this view shows". */
function poolLabel(state, pool) {
  if (hasNoFilters(state) && pool.length) {
    const key = pool[0].status;
    return `the ${plural(pool.length, 'game')} in ${store.statusByCode.get(key)?.label || key}`;
  }
  return `the ${plural(pool.length, 'game')} shown here`;
}

/** Open the Surprise me sheet for the games `state` (a Library or view filter state) shows. */
export function openSurprise(state) {
  const pool = buildPool(state, store.games);
  const year = new Date().getFullYear();
  const sheet = makeSheet('Surprise me', 'surprise');
  const mood = emptyMood();
  let shown = new Set();   // offered since the mood last changed

  if (!pool.length) { renderEmpty(); return; }
  renderMood();

  function renderEmpty() {
    sheet.back.hidden = true;
    sheet.foot.hidden = true;
    const none = hasNoFilters(state)
      ? 'You have no games in Up next or Backlog to pick from. Move a few there, or open a view or filter to choose from other games.'
      : 'None of the games shown here can be picked: Finished, Dropped, Not Interested and Evergreen games are left out unless you filter to them. Change the filters and try again.';
    sheet.body.replaceChildren(h('p.hint.surprise-msg', {}, none));
  }

  // ---------- step 1: the mood ----------
  function renderMood() {
    sheet.back.hidden = true;
    sheet.title.textContent = 'Surprise me';
    sheet.foot.classList.remove('three');
    const counts = moodCounts(pool, mood, year);
    const left = applyMood(pool, mood, year);

    const chip = (label, n, on, toggle) => {
      const b = h('button.chip', { type: 'button', 'aria-pressed': String(on), onclick: () => { toggle(); shown = new Set(); renderMood(); } }, label, h('span.c', {}, n));
      if (n === 0 && !on) b.classList.add('zero');
      return b;
    };
    const group = (title, set, map) => map.size ? h('section.mood-group', {}, h('h3', {}, title),
      h('div.chips', {}, [...map].map(([v, n]) => chip(v, n, set.has(v), () => (set.has(v) ? set.delete(v) : set.add(v)))))) : null;

    sheet.body.replaceChildren(...[
      h('p.hint', {}, `Picking from ${poolLabel(state, pool)}. Narrow it down if you like, or just pick.`),
      group('Platform', mood.platforms, counts.platform),
      group('Genre', mood.genres, counts.genre),
      h('section.mood-group', {}, h('h3', {}, 'Also'),
        h('div.chips', {},
          chip('VR only', counts.vr, mood.vr, () => { mood.vr = !mood.vr; }),
          chip('Not played recently', counts.stale, mood.stale, () => { mood.stale = !mood.stale; }))),
      h('p.hint', {}, 'Not played recently: never played, or last played two or more years ago.')].filter(Boolean));

    const go = h('button.btn.primary', { type: 'button', disabled: left.length === 0, onclick: () => deal(left) }, left.length ? `Pick one of ${left.length}` : 'Pick one');
    sheet.foot.replaceChildren(...[left.length ? null : h('span.surprise-msg', { role: 'status' }, 'No game fits this mood. Remove a choice.'), go].filter(Boolean));
    sheet.foot.hidden = false;
  }

  // ---------- step 2: the result ----------
  function deal(candidates) {
    const r = pickOne(candidates, shown, skipped);
    if (!r.game) { renderExhausted(candidates, r); return; }
    shown.add(r.game.id);
    renderCard(candidates, r.game, r.left);
  }

  function renderCard(candidates, g, left) {
    sheet.back.hidden = false;
    sheet.back.onclick = renderMood;
    sheet.title.textContent = 'How about…';
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    const played = playedBadges(g, 6);
    const hasPlayed = played.years.length || played.periods.length;
    sheet.body.replaceChildren(h('div.pick-card', { style: st ? `--st:${st.color}` : null },
      h('div.cover', {},
        url ? lazyImg(url) : h('div.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title)),
        st && h('span.st', { style: badgeStyle(st.color) }, st.label)),
      h('div.pick-body', {},
        h('h2.pick-title', {}, g.title),
        g.platforms.length ? h('div.tags-row', {}, ...g.platforms.map((p) => h('span.pf', {}, p))) : null,
        g.genres.length ? h('div.tags-row', {}, ...g.genres.map((x) => h('span.pf.genre', {}, x))) : null,
        h('div.years-row', { title: playedSummary(g), role: 'img', 'aria-label': playedSummary(g) },
          hasPlayed ? [
            ...played.periods.map((p) => h('span.yr.period', { title: p.label }, p.icon)),
            played.earlier ? h('span.yr.earlier', {}, `+${played.earlier}`) : null,
            ...played.years.map((y) => h('span.yr', {}, String(y.year))),
          ] : h('span.pick-never', {}, 'Not played yet')))),
      h('p.hint.pick-left', { role: 'status' }, left ? `${plural(left, 'other game')} left to see` : 'This is the last one'));

    sheet.foot.replaceChildren(
      h('button.btn.primary', { type: 'button', onclick: () => play(g) }, '▶ Play it'),
      h('button.btn', { type: 'button', onclick: () => deal(candidates) }, 'Another one'),
      h('button.btn', { type: 'button', onclick: () => { skipped.add(g.id); saveSkipped(skipped); deal(candidates); } }, 'Skip'));
    sheet.foot.hidden = false;
    sheet.foot.classList.add('three');
  }

  function renderExhausted(candidates, r) {
    sheet.back.hidden = false;
    sheet.back.onclick = renderMood;
    sheet.title.textContent = 'That is all';
    sheet.foot.classList.remove('three');
    const actions = [];
    if (r.allSkipped) {
      sheet.body.replaceChildren(h('p.hint.surprise-msg', {}, 'You skipped every game that fits. Forget the skips to see them again, or change the mood.'));
      actions.push(h('button.btn.primary', { type: 'button', onclick: () => { skipped.clear(); saveSkipped(skipped); shown = new Set(); deal(candidates); } }, 'Forget my skips'));
    } else {
      sheet.body.replaceChildren(h('p.hint.surprise-msg', {}, 'You have seen every game that fits. Start over to see them again, or change the mood.'));
      actions.push(h('button.btn.primary', { type: 'button', onclick: () => { shown = new Set(); deal(candidates); } }, 'Start over'));
    }
    sheet.foot.replaceChildren(h('button.btn', { type: 'button', onclick: renderMood }, 'Change mood'), ...actions);
    sheet.foot.hidden = false;
  }

  async function play(g) {
    sheet.close();
    try { await patchGame(g.id, { status: 'playing' }); toast(`Enjoy ${g.title}!`); }
    catch { toast('Could not mark it as Playing'); }
  }
}
