// The "Expansions (n)" section of the game drawer: each IGDB expansion of the game as a row (small poster, title, year, status,
// ownership, years played); tapping one opens its edit sheet. Opening the drawer never waits for IGDB: a linked game whose
// expansions were never read, or not within 30 days, is read in the background and the section updates when it lands.
import { h, gradientFor, initials, lazyImg, plural } from './util.js';
import { store, refreshExpansions } from './api.js';
import { badgeStyle } from './colors.js';
import { ownershipIcon } from './ownership.js';
import { playedBadges } from './years.js';
import { expansionCoverURL, withYear, expansionsOf, isStale } from './expansions.js';
import { openExpansionSheet } from './expansionsheet.js';

const asked = new Set(); // game ids asked this session: a failed read is not retried on every drawer open

export function expansionsSection(game) {
  const box = h('section.exp-drawer', { 'aria-label': 'Expansions' });
  let showHidden = false;
  let loading = false;

  function row(x) {
    const st = x.status && store.statusByCode.get(x.status);
    const url = expansionCoverURL(x);
    const played = playedBadges(x, 2);
    const icon = x.ownership && ownershipIcon(x.ownership);
    return h('li', {}, h('button.exp-row' + (x.hidden ? '.is-hidden' : ''), { type: 'button', onclick: () => openExpansionSheet(x.id), 'aria-label': `${withYear(x)}: edit` },
      h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(x.title)}` }, initials(x.title))),
      h('span.exp-body', {},
        h('span.exp-title', { translate: 'no' }, x.title, x.year ? h('small', {}, ` ${x.year}`) : null),
        h('span.exp-meta', {},
          st ? h('span.exp-st', { style: badgeStyle(st.color) }, st.label) : h('span.exp-st.none', {}, 'Not tracked'),
          icon ? h('span.exp-own', { title: x.ownership, role: 'img', 'aria-label': x.ownership }, icon) : null,
          ...played.periods.map((p) => h('span.yr.period', { title: p.label }, p.icon)),
          played.earlier ? h('span.yr.earlier', {}, `+${played.earlier}`) : null,
          ...played.years.map((y) => h('span.yr', {}, String(y.year)))))));
  }

  function draw() {
    if (!box.isConnected && box.dataset.drawn) return;
    box.dataset.drawn = '1';
    const all = expansionsOf(game);
    const shown = all.filter((x) => !x.hidden);
    const hidden = all.length - shown.length;
    if (!game.igdb_id) { // nothing to read: say why, once
      box.replaceChildren(h('h3', {}, 'Expansions'), h('p.hint', {}, 'Not linked to IGDB. Link the game (Edit, then Fill from IGDB) to see its expansions.'));
      return;
    }
    const fetched = !!game.expansions_fetched;
    box.replaceChildren(...[
      h('h3', {}, `Expansions (${shown.length})`),
      shown.length ? h('ul.exp-list', {}, shown.map(row)) : null,
      !shown.length && loading ? h('p.hint', { role: 'status' }, 'Looking for expansions on IGDB…') : null,
      !shown.length && !loading && fetched ? h('p.hint', {}, hidden ? 'All of its expansions are hidden.' : 'No expansions on IGDB.') : null,
      !shown.length && !loading && !fetched ? h('p.hint', {}, 'Not read from IGDB yet.') : null,
      hidden ? h('button.exp-link', { type: 'button', 'aria-expanded': String(showHidden), onclick: () => { showHidden = !showHidden; draw(); } }, showHidden ? 'Hide the hidden ones' : `Show ${plural(hidden, 'hidden one')}`) : null,
      showHidden && hidden ? h('ul.exp-list', {}, all.filter((x) => x.hidden).map(row)) : null,
      h('button.exp-link', { type: 'button', disabled: loading || null, onclick: () => read(false) }, loading ? 'Reading IGDB…' : '↻ Refresh from IGDB'),
    ].filter(Boolean)); // replaceChildren would print a null as the text "null"
  }

  async function read(onlyIfStale) {
    if (loading) return;
    loading = true;
    draw();
    try { await refreshExpansions(game.id, { onlyIfStale }); } catch { /* IGDB not set up or unreachable: the section keeps what it has */ }
    loading = false;
    if (box.isConnected) draw();
  }

  const onGames = (e) => {
    if (!box.isConnected) { window.removeEventListener('games-changed', onGames); return; }
    if (!e.detail?.id || e.detail.id === game.id) draw();
  };
  window.addEventListener('games-changed', onGames);
  draw();
  // The drawer opens at once; IGDB is asked afterwards, only for a linked game that is due.
  if (game.igdb_id && isStale(game) && !asked.has(game.id)) {
    asked.add(game.id);
    queueMicrotask(() => read(true));
  }
  return box;
}
