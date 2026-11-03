// Deals page: the current best price and the historical low of the games you don't own and haven't played yet, from
// IsThereAnyDeal. Nothing here asks ITAD on its own: the prices are read when the owner presses Refresh prices.
import { h, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { store, coverURL, posterURL, dealsList, dealsRefresh, dealsSetCountry, dealsHide, dealsUnhide } from './api.js';
import { openDrawer } from './drawer.js';
import { DEAL_STATUSES, SORTS, BADGE_LABEL, buildDeals, hiddenGames, scopeOf, formatMoney, formatDate, oldestCheck, safeLink, LOCALE } from './deals.js';

const imageOf = (g) => posterURL(g) || coverURL(g);

function poster(game) {
  const url = imageOf(game);
  return h('span.pst', {}, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(game.title)}` }, initials(game.title)));
}

export function renderDeals(root) {
  let info = { configured: false, country: 'ES', matches: [], prices: [], refresh: { running: false } };
  let statuses = new Set(); // empty = all three
  let sort = 'cut';
  let alive = true;
  let timer = null;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.deals-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  async function load() {
    try { info = await dealsList(); } catch { /* offline or an old server: the page says nothing is cached */ }
    if (!alive) return;
    draw();
    if (info.refresh?.running) schedulePoll();
  }
  function schedulePoll() {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const was = info.refresh;
      await load();
      if (alive && was.running && !info.refresh.running) {
        const r = info.refresh;
        toast(r.failed ? `Price refresh: ${r.report || 'failed'}` : `Prices refreshed. ${r.report || ''}`, 5000);
        say(r.failed ? 'The price refresh stopped.' : 'Prices refreshed.');
      }
    }, 1500);
  }
  async function refresh() {
    try {
      info.refresh = await dealsRefresh();
      say('Reading prices from IsThereAnyDeal.');
    } catch (e) {
      toast(e.message || 'Could not start the refresh');
      return;
    }
    draw();
    schedulePoll();
  }
  async function saveCountry(value) {
    const code = value.trim().toUpperCase();
    if (code === info.country) return;
    try {
      info = await dealsSetCountry(code);
      toast(`Prices are read for ${info.country}. Press Refresh prices to read them.`, 4000);
    } catch (e) {
      toast(e.message || 'Could not change the country');
    }
    draw();
  }

  async function setHidden(game, hide) {
    try {
      const refresh = info.refresh;
      info = { ...(hide ? await dealsHide(game.id) : await dealsUnhide(game.id)), refresh };
    } catch (e) { toast(e.message || (hide ? 'Could not hide the game' : 'Could not show the game again')); return; }
    draw();
    if (hide) {
      say(`${game.title} hidden.`);
      toastAction(`Hidden “${game.title}”`, 'Undo', () => setHidden(game, false));
    } else say(`${game.title} shown again.`);
  }

  // ---- the page ----
  function row({ game, p }, badge) {
    const link = p.price != null ? safeLink(p.url) : null;
    const onSale = p.price != null && p.cut > 0;
    const price = p.price == null
      ? h('div.deal-price', {}, h('span', {}, 'No shop lists it right now'))
      : h('div.deal-price', {},
        h('b', {}, formatMoney(p.price, p.currency)),
        onSale && p.regular > p.price ? h('s', {}, formatMoney(p.regular, p.currency)) : null,
        onSale ? h('span.deal-cut', {}, `-${p.cut}%`) : null,
        p.shop ? h('span', {}, `at ${p.shop}`) : null);
    const low = p.low_price != null
      ? `Lowest ever ${formatMoney(p.low_price, p.currency)}${p.low_shop ? ` at ${p.low_shop}` : ''}${p.low_at ? `, ${formatDate(p.low_at)}` : ''}`
      : 'No price history';
    const expiry = p.price != null && p.expiry && formatDate(p.expiry) ? ` · Ends ${formatDate(p.expiry)}` : '';
    const body = h('span.up-body', {},
      h('span.up-title', {}, game.title),
      price,
      h('span.up-from', {}, low + expiry),
      badge ? h('span', {}, h('span.deal-badge' + (badge === 'low' ? '.low' : ''), {}, BADGE_LABEL[badge])) : null);
    const status = DEAL_STATUSES.find((s) => s.code === game.status)?.label || '';
    const aria = `${game.title}, ${status}, ${p.price == null ? 'not on sale' : formatMoney(p.price, p.currency)}${badge ? `, ${BADGE_LABEL[badge]}` : ''}`;
    return h('li.up-row', {},
      h('button.up-open', { type: 'button', onclick: () => openDrawer(game), 'aria-label': aria }, poster(game), body),
      h('span.si-actions', {},
        link ? h('a.add-chip.deal-link', { href: link, target: '_blank', rel: 'noopener noreferrer', 'aria-label': `Open the deal for ${game.title} in a new tab` }, `View deal ↗`) : null,
        hideBtn(game)));
  }
  const hideBtn = (game) => h('button.hide-chip', { type: 'button', 'aria-label': `Hide ${game.title}`, onclick: () => setHidden(game, true) }, 'Hide');
  function plainRow({ game, match }, note) {
    return h('li.up-row', {}, h('button.up-open', { type: 'button', onclick: () => openDrawer(game), 'aria-label': `${game.title}, ${note}` }, poster(game),
      h('span.up-body', {}, h('span.up-title', {}, game.title), h('span.up-from', {}, note))),
    h('span.si-actions', {}, hideBtn(game)));
  }

  function controls() {
    const scope = scopeOf(info);
    const chips = DEAL_STATUSES.map((s) => {
      const count = store.games.filter((g) => g.status === s.code && scope.has(g.id)).length;
      return h('button.chip', { type: 'button', 'aria-pressed': String(statuses.has(s.code)), onclick: () => {
        if (statuses.has(s.code)) statuses.delete(s.code); else statuses.add(s.code);
        draw();
      } }, s.label, ' ', h('span.c', {}, String(count)));
    });
    const select = h('select', { 'aria-label': 'Sort by', onchange: (e) => { sort = e.target.value; draw(); } },
      SORTS.map((s) => h('option', { value: s.key, selected: s.key === sort || null }, s.label)));
    return [h('div.chips', {}, ...chips), h('div.deals-bar', {}, select)];
  }

  function refreshBar() {
    const r = info.refresh || {};
    const btn = h('button.chip', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      title: info.configured ? '' : 'IsThereAnyDeal is not set up (ITAD_API_KEY is missing from .env)' },
    r.running ? `Reading prices… ${r.total ? `${r.done}/${r.total}` : ''}` : '↻ Refresh prices');
    const old = oldestCheck(info);
    const note = !info.configured && !old ? 'IsThereAnyDeal is not set up: add ITAD_API_KEY to .env and restart the app (see the user manual, Deals).'
      : old ? `Last checked ${new Date(old).toLocaleString(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })}.`
        : 'Press Refresh prices to read the prices of the games you do not own and have not played yet.';
    return [btn, note];
  }

  function draw() {
    const { rows, unmatched, waiting } = buildDeals(store.games, info, { statuses, sort });
    const [btn, note] = refreshBar();
    const country = h('label.deals-country', {}, 'Country',
      h('input', { value: info.country || 'ES', maxlength: 2, 'aria-label': 'Country code for prices, for example ES', autocomplete: 'off',
        onchange: (e) => saveCountry(e.target.value) }));
    const scope = rows.length + unmatched.length + waiting.length;
    const hiddenList = hiddenGames(store.games, info);
    const empty = scopeOf(info).size ? 'No games match these filters.'
      : hiddenList.length ? 'All your deals games are hidden. Show them again below.'
      : 'Deals shows games you don’t own and haven’t played yet: in the backlog or up next, with no years played, and ownership Pirated, Borrowed, Family Share, Nope or empty.';
    // Matched, but no shop sells it: often the match is an old or delisted entry (a game and its "Final Cut"), so say where to look.
    const unsold = rows.filter((r) => r.p.price == null).map((r) => r.game.title);
    const unsoldHint = unsold.length
      ? h('p.hint', {}, `No shop lists ${unsold.slice(0, 5).join(', ')}${unsold.length > 5 ? ` and ${unsold.length - 5} more` : ''}. Wrong game? Open one and tap “Wrong game?” to pick another.`)
      : null;
    page.replaceChildren(...[
      h('h1', {}, 'Deals'),
      h('p.hint', {}, 'The best price right now and the historical low of the games you don’t own and haven’t played yet, from IsThereAnyDeal.'),
      h('div.chips', {}, btn),
      h('p.hint', {}, note),
      info.configured ? country : null,
      ...controls(),
      rows.length ? h('ul.up-list', {}, rows.map((r) => row(r, r.badge))) : null,
      unsoldHint,
      !rows.length && info.configured ? h('div.empty', {}, scope ? 'No prices yet. Press Refresh prices.' : empty) : null,
      waiting.length ? h('p.hint', {}, `${waiting.length} ${waiting.length === 1 ? 'game is' : 'games are'} waiting for their prices: press Refresh prices.`) : null,
      unmatched.length ? h('section.up-group', {}, h('h2', {}, 'No match on IsThereAnyDeal', h('small', {}, ` ${unmatched.length}`)),
        h('p.hint', {}, 'Open a game to pick the right one by hand.'),
        h('ul.up-list', {}, unmatched.map((u) => plainRow(u, u.match.source === 'user' ? 'You said there is no match' : 'No exact match found')))) : null,
      hiddenList.length ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${hiddenList.length})`),
        h('ul', {}, hiddenList.map((g) => h('li', {}, h('span', {}, g.title),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${g.title} again`, onclick: () => setHidden(g, false) }, 'Show again'))))) : null,
    ].filter(Boolean));
  }

  // A game edited elsewhere may enter or leave the scope (ownership, years played): draw at once, then ask the server again (cache only, no ITAD).
  let reloadTimer = null;
  function onGames() { draw(); clearTimeout(reloadTimer); reloadTimer = setTimeout(load, 300); }
  window.addEventListener('deals-changed', load);
  draw();
  load();
  window.addEventListener('games-changed', onGames);
  return () => { alive = false; clearTimeout(timer); clearTimeout(reloadTimer); window.removeEventListener('games-changed', onGames); window.removeEventListener('deals-changed', load); };
}
