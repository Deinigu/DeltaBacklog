// Deals page: the current best price and the historical low of the games you don't own and haven't played yet, from
// IsThereAnyDeal. Nothing here asks ITAD on its own: the prices are read when the owner presses Refresh prices.
import { h, gradientFor, initials, lazyImg, toast, toastAction } from './util.js';
import { store, coverURL, posterURL, dealsList, dealsRefresh, dealsSetCountry, dealsHide, dealsUnhide, dealsMute, dealsAlertsSeen, dealsTelegramTest,
  dealsWishHide, dealsWishUnhide, dealsWishMute, dealsWishUnmute } from './api.js';
import { openDrawer } from './drawer.js';
import { openAdd } from './add.js';
import { DEAL_STATUSES, SORT_KEYS, COLUMNS, BADGE_LABEL, ALERT_LABEL, buildDeals, buildWishlist, hiddenGames, scopeOf, steamStoreURL, formatMoney, formatDate, oldestCheck, safeLink, priceLine, lowLine, tagsOf, checkedLabel } from './deals.js';

export function renderDeals(root) {
  let info = { configured: false, country: 'ES', matches: [], prices: [], refresh: { running: false } };
  let statuses = new Set(); // empty = all three
  let sort = 'cut';
  let dir = 1; // 1 = the sort's natural order, -1 = reversed (a click on the active column header)
  let infoOpen = false; // the one-line explanation behind the (i) button
  let alertsOnly = false; // the Alerts chip: only the games with a standing price alert
  let view = 'library'; // 'library' (your games) or 'wishlist' (your Steam wishlist; only when the server has a STEAM_ID)
  let newIds = new Set(); // the alerts that were new when this visit began (they are marked seen at once)
  let seenSent = false;
  let alive = true;
  let timer = null;
  const live = h('div.sr-only', { role: 'status', 'aria-live': 'polite' });
  const page = h('div.manage.series-page.up-page.deals-page');
  root.replaceChildren(page, live);
  const say = (t) => { live.textContent = t; };

  async function load() {
    try { info = await dealsList(); } catch { /* offline or an old server: the page says nothing is cached */ }
    if (!alive) return;
    if (!seenSent && info.unseen > 0) { // opening Deals is seeing the alerts: remember which ones were new, then tell the server
      seenSent = true;
      newIds = new Set([...(info.alerts || []).filter((a) => !a.seen).map((a) => a.game_id),
        ...(info.wishlist?.items || []).filter((i) => i.alert && !i.alert.seen).map((i) => `w${i.appid}`)]);
      dealsAlertsSeen().then((r) => { store.dealsUnseen = r.unseen || 0; window.dispatchEvent(new Event('deals-unseen')); }).catch(() => { seenSent = false; });
    }
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

  async function setWishHidden(item, hide) {
    try {
      const refresh = info.refresh;
      info = { ...(hide ? await dealsWishHide(item.appid) : await dealsWishUnhide(item.appid)), refresh };
    } catch (e) { toast(e.message || (hide ? 'Could not hide the game' : 'Could not show the game again')); return; }
    draw();
    if (hide) {
      say(`${item.title} hidden.`);
      toastAction(`Hidden “${item.title}”`, 'Undo', () => setWishHidden(item, false));
    } else say(`${item.title} shown again.`);
  }
  async function wishMute(item, off) {
    try {
      const refresh = info.refresh;
      info = { ...(off ? await dealsWishMute(item.appid) : await dealsWishUnmute(item.appid)), refresh };
    } catch (e) { toast(e.message || 'Could not change the alerts'); return; }
    store.dealsUnseen = info.unseen || 0;
    window.dispatchEvent(new Event('deals-unseen'));
    draw();
    say(`Alerts for ${item.title} are ${off ? 'off' : 'on'}.`);
  }

  async function mute(game) {
    try {
      const refresh = info.refresh;
      info = { ...(await dealsMute(game.id)), refresh };
    } catch (e) { toast(e.message || 'Could not turn the alerts off'); return; }
    store.dealsUnseen = info.unseen || 0; // a muted game's alert leaves the count
    window.dispatchEvent(new Event('deals-unseen'));
    draw();
    say(`Alerts for ${game.title} are off.`);
    toast(`Alerts off for “${game.title}”. Turn them on again in the game’s Price section.`, 4000);
  }
  async function telegramTest(btn) {
    btn.disabled = true;
    try {
      await dealsTelegramTest();
      toast('Test message sent to Telegram.', 3000);
      say('Test message sent.');
    } catch (e) { toast(e.message || 'The test message was not sent', 5000); }
    btn.disabled = false;
  }

  // ---- the page ----
  // One compact row per game (SteamDB style): capsule image, name with small tags, discount, price, and on a desktop the
  // table columns. Tapping a row opens an action sheet (see openSheet): one place for View deal / Open / Add / Mute / Hide, so
  // a row never grows button rows and the tap never navigates away by accident.
  const closeSheet = () => { if (sheetEl) { sheetEl.close(); } };
  let sheetEl = null;
  function openSheet(o) {
    closeSheet();
    const p = o.p;
    const link = p && p.price != null ? safeLink(p.url) : null;
    const act = (label, fn, cls = '') => h('button.dl-act' + cls, { type: 'button', onclick: () => { closeSheet(); fn(); } }, label);
    const actions = [
      link ? h('a.dl-act', { href: link, target: '_blank', rel: 'noopener noreferrer', onclick: closeSheet }, 'View deal ↗') : null,
      ...o.actions.map((a) => (a.href ? h('a.dl-act', { href: a.href, target: '_blank', rel: 'noopener noreferrer', onclick: closeSheet }, a.label) : act(a.label, a.run))),
    ];
    const lines = p ? [priceLine(p), lowLine(p) + (p.low_at && p.low_price != null ? `, ${formatDate(p.low_at)}` : ''),
      p.price != null && p.expiry && formatDate(p.expiry) ? `Ends ${formatDate(p.expiry)}` : null] : [o.note];
    const dlg = h('dialog.dl-sheet', { 'aria-label': `${o.title}: actions` },
      h('div.dl-sheet-head', {}, imageCell(o.image, o.title),
        h('div', {}, h('b.dl-sheet-title', {}, o.title), ...lines.filter(Boolean).map((l) => h('div.dl-sub', {}, l)),
          ...(o.tags || []).length ? [h('div.dl-tags', {}, o.tags.map((t) => h('span.dl-tag.' + t.cls, {}, t.text)))] : [])),
      h('div.dl-acts', {}, ...actions),
      h('button.dl-act.dl-close', { type: 'button', onclick: closeSheet }, 'Close'));
    dlg.addEventListener('click', (e) => { if (e.target === dlg) closeSheet(); }); // a tap on the backdrop
    dlg.addEventListener('close', () => { dlg.remove(); if (sheetEl === dlg) sheetEl = null; });
    document.body.append(dlg);
    sheetEl = dlg;
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
  }

  function imageCell(url, title) {
    return h('span.dl-img', { role: 'cell' }, url ? lazyImg(url) : h('span.ph', { style: `background:${gradientFor(title)}` }, initials(title)));
  }

  /** One row. o: {title, image, p (the price or null), badge, alert, isNew, added, note, aria, actions: [{label, run | href}]}. */
  function dlRow(o) {
    const p = o.p;
    const onSale = p && p.price != null && p.cut > 0;
    const tags = tagsOf(o.badge, o.alert, o.isNew);
    const ends = p && p.price != null && p.expiry && formatDate(p.expiry) ? formatDate(p.expiry) : '';
    const sub = p ? lowLine(p) + (ends ? ` · Ends ${ends}` : '') : o.note;
    const open = () => openSheet({ ...o, tags });
    const cls = 'li.dl-row' + (o.badge === 'low' ? '.is-low' : '') + (o.alert ? '.has-alert' : '') + (p ? '' : '.no-price');
    return h(cls, { role: 'row' },
      imageCell(o.image, o.title),
      h('span.dl-name', { role: 'cell' },
        h('button.dl-open', { type: 'button', 'aria-haspopup': 'dialog', 'aria-label': o.aria, onclick: open }, h('span.dl-title', {}, o.title)),
        tags.length ? h('span.dl-tags', {}, tags.map((t) => h('span.dl-tag.' + t.cls, {}, t.text))) : null,
        h('span.dl-sub', {}, sub)),
      h('span.dl-cut', { role: 'cell' }, onSale ? h('span.dl-pct', {}, `-${p.cut}%`) : null),
      h('span.dl-price', { role: 'cell' }, p && p.price != null ? [h('b', {}, formatMoney(p.price, p.currency)), onSale && p.regular > p.price ? h('s', {}, formatMoney(p.regular, p.currency)) : null]
        : p ? h('span.dl-none', {}, 'Not sold') : null),
      h('span.dl-low', { role: 'cell' }, p && p.low_price != null ? formatMoney(p.low_price, p.currency) : ''),
      h('span.dl-shop', { role: 'cell' }, p && p.price != null ? p.shop || '' : ''),
      h('span.dl-ends', { role: 'cell' }, ends),
      view === 'wishlist' ? h('span.dl-added', { role: 'cell' }, o.added ? formatDate(o.added) : '') : null,
      h('button.dl-more', { type: 'button', 'aria-haspopup': 'dialog', 'aria-label': `Actions for ${o.title}`, onclick: open }, '⋯'));
  }

  const ariaOf = (title, kind, p, badge, alert, isNew) => `${title}, ${kind}, ${!p || p.price == null ? 'not on sale' : formatMoney(p.price, p.currency)}${badge ? `, ${BADGE_LABEL[badge]}` : ''}${alert ? `, price alert: ${ALERT_LABEL[alert.kind] || 'price'}${isNew ? ', new' : ''}` : ''}. Opens the actions`;

  function gameActions(game, alert, withHide = true) {
    return [{ label: 'Open game', run: () => openDrawer(game) },
      alert ? { label: 'Mute price alerts', run: () => mute(game) } : null,
      withHide ? { label: 'Hide from Deals', run: () => setHidden(game, true) } : null].filter(Boolean);
  }
  const libImage = (game) => coverURL(game) || posterURL(game) || (info.lib_images?.[game.id] ? `covers/${info.lib_images[game.id]}` : null);
  function libRow({ game, p, alert }, badge) {
    const status = DEAL_STATUSES.find((s) => s.code === game.status)?.label || '';
    return dlRow({ title: game.title, image: libImage(game), p, badge, alert, isNew: alert && newIds.has(game.id),
      aria: ariaOf(game.title, status, p, badge, alert, alert && newIds.has(game.id)), actions: gameActions(game, alert) });
  }
  function libPlainRow({ game }, note) {
    return dlRow({ title: game.title, image: libImage(game), p: null, note, aria: `${game.title}, ${note}. Opens the actions`, actions: gameActions(game, null) });
  }
  function wishActions(item) {
    return [{ label: 'Steam page ↗', href: steamStoreURL(item.appid) },
      { label: 'Add to library', run: () => openAdd({ query: item.title }) },
      item.alert ? { label: 'Mute price alerts', run: () => wishMute(item, true) }
        : item.muted ? { label: 'Turn price alerts on', run: () => wishMute(item, false) } : null,
      { label: 'Hide from Deals', run: () => setWishHidden(item, true) }].filter(Boolean);
  }
  const wishImage = (item) => (item.image ? `covers/${item.image}` : null);
  function wishRow({ item, p, alert }, badge) {
    const isNew = alert && newIds.has(`w${item.appid}`);
    return dlRow({ title: item.title, image: wishImage(item), p, badge, alert, isNew, added: item.added_at,
      aria: ariaOf(item.title, 'wishlist', p, badge, alert, isNew), actions: wishActions(item) });
  }
  function wishPlainRow({ item }, note) {
    return dlRow({ title: item.title, image: wishImage(item), p: null, note, added: item.added_at, aria: `${item.title}, ${note}. Opens the actions`, actions: wishActions(item) });
  }

  // The list: a header row with sortable columns (desktop only) and the rows.
  function list(rows, make) {
    const keys = SORT_KEYS.filter((s) => !s.wish || view === 'wishlist');
    if (!keys.some((s) => s.key === sort)) { sort = 'cut'; dir = 1; }
    const cols = COLUMNS.filter((c) => !c.wish || view === 'wishlist');
    const head = h('li.dl-row.dl-headrow', { role: 'row' },
      h('span', { role: 'presentation' }), // image
      ...cols.map((c) => h('span.dl-h.dl-h-' + c.key, { role: 'columnheader', 'aria-sort': sort === c.key ? (dir === 1 ? 'ascending' : 'descending') : 'none' },
        h('button', { type: 'button', onclick: () => { if (sort === c.key) dir = -dir; else { sort = c.key; dir = 1; } draw(); } },
          c.label, sort === c.key ? h('span.dl-arrow', { 'aria-hidden': 'true' }, dir === 1 ? ' ↓' : ' ↑') : null))),
      h('span', { role: 'presentation' }));
    return h('ul.dl-list' + (view === 'wishlist' ? '.dl-wish' : ''), { role: 'table', 'aria-label': 'Deals' }, head, rows.map(make));
  }
  const plainList = (rows, make) => h('ul.dl-list.dl-plain' + (view === 'wishlist' ? '.dl-wish' : ''), { role: 'table' }, rows.map(make));

  function toolbar() {
    const scope = scopeOf(info);
    const r = info.refresh || {};
    const old = oldestCheck(info);
    const views = info.wishlist ? h('div.dl-seg', { role: 'group', 'aria-label': 'What to list' },
      h('button', { type: 'button', 'aria-pressed': String(view === 'library'), onclick: () => { view = 'library'; closeSheet(); draw(); } }, 'Library ', h('span.c', {}, String(scope.size))),
      h('button', { type: 'button', 'aria-pressed': String(view === 'wishlist'), onclick: () => { view = 'wishlist'; closeSheet(); draw(); } }, 'Wishlist ', h('span.c', {}, String(info.wishlist.items.length)))) : null;
    const btn = h('button.dl-refresh', { type: 'button', disabled: !info.configured || r.running || null, onclick: refresh,
      'aria-label': r.running ? 'Reading prices' : 'Refresh prices',
      title: info.configured ? 'Refresh prices' : 'IsThereAnyDeal is not set up (ITAD_API_KEY is missing from .env)' },
    h('span' + (r.running ? '.spin' : ''), { 'aria-hidden': 'true' }, '↻'));
    const checked = r.running ? `Reading prices… ${r.total ? `${r.done}/${r.total}` : ''}` : old ? `checked ${checkedLabel(old)}` : 'not checked yet';
    const country = info.configured ? h('label.dl-country', {}, h('span.sr-only', {}, 'Country for prices'),
      h('input', { value: info.country || 'ES', maxlength: 2, list: 'dl-countries', 'aria-label': 'Country code for prices, for example ES', autocomplete: 'off', title: 'Country for prices',
        onchange: (e) => saveCountry(e.target.value) }),
      h('datalist', { id: 'dl-countries' }, ['ES', 'US', 'GB', 'DE', 'FR', 'IT', 'PT', 'NL', 'PL', 'SE', 'BR', 'MX', 'CA', 'AU', 'JP'].map((c) => h('option', { value: c })))) : null;
    return h('div.dl-toolbar', {}, views, h('span.dl-check', {}, btn, h('span.dl-checked', {}, checked)), country,
      null);
  }

  function filters() {
    const scope = scopeOf(info);
    const chips = view === 'wishlist' ? [] : DEAL_STATUSES.map((s) => {
      const count = store.games.filter((g) => g.status === s.code && scope.has(g.id)).length;
      return h('button.chip', { type: 'button', 'aria-pressed': String(statuses.has(s.code)), onclick: () => {
        if (statuses.has(s.code)) statuses.delete(s.code); else statuses.add(s.code);
        draw();
      } }, s.label, ' ', h('span.c', {}, String(count)));
    });
    const alertCount = view === 'wishlist' ? buildWishlist(info, { alertsOnly: true }).rows.length : buildDeals(store.games, info, { alertsOnly: true }).rows.length;
    const alertChip = h('button.chip' + (alertCount ? '' : '.zero'), { type: 'button', 'aria-pressed': String(alertsOnly), title: 'Games at their historical low or their lowest price in 2 years',
      onclick: () => { alertsOnly = !alertsOnly; draw(); } }, 'Alerts ', h('span.c', {}, String(alertCount)));
    const select = h('select.dl-sortsel', { 'aria-label': 'Sort by', onchange: (e) => { sort = e.target.value; dir = 1; draw(); } },
      SORT_KEYS.filter((s) => !s.wish || view === 'wishlist').map((s) => h('option', { value: s.key, selected: s.key === sort || null }, s.label)));
    return h('div.dl-filters', {}, select, ...chips, alertChip);
  }

  const sectionNote = (rows, nameOf) => {
    const unsold = rows.filter((r) => r.p.price == null).map(nameOf);
    return unsold.length ? `No shop lists ${unsold.slice(0, 5).join(', ')}${unsold.length > 5 ? ` and ${unsold.length - 5} more` : ''}.` : '';
  };
  const waitingNote = (n) => `${n} ${n === 1 ? 'game is' : 'games are'} waiting for their prices: press Refresh prices.`;

  function header(extra) {
    const wl = view === 'wishlist';
    const text = wl ? 'Your Steam wishlist, not part of your library. A game leaves this list when you add it to your library or take it off your wishlist (for example by buying it); it is read from Steam when you press Refresh prices.'
      : 'The best price right now and the historical low of the games you don’t own and haven’t played yet, from IsThereAnyDeal. A game at its historical low or its lowest price in 2 years gets an alert.';
    const setup = !info.configured && !oldestCheck(info) ? 'IsThereAnyDeal is not set up: add ITAD_API_KEY to .env and restart the app (see the user manual, Deals).' : '';
    return [
      h('div.dl-title-row', {}, h('h1', {}, 'Deals'),
        h('button.dl-info', { type: 'button', 'aria-expanded': String(infoOpen), 'aria-label': 'About this page', onclick: () => { infoOpen = !infoOpen; draw(); } }, 'i')),
      infoOpen ? h('div.dl-info-panel', {}, h('p.hint', {}, text),
        info.telegram ? h('button.dl-tg', { type: 'button', onclick: (e) => telegramTest(e.currentTarget) }, 'Send test message') : null) : null,
      setup ? h('p.hint', {}, setup) : null,
      toolbar(),
      extra,
      filters(),
    ].filter(Boolean);
  }

  // The Wishlist view: the Steam wishlist games, with the same rows.
  function drawWishlist() {
    const w = info.wishlist;
    const { rows, unmatched, waiting } = buildWishlist(info, { sort, dir, alertsOnly });
    const empty = alertsOnly ? 'No price alerts right now. A game gets one when its price reaches its historical low or its lowest in 2 years.'
      : w.items.length ? 'No games match these filters.'
      : w.hidden.length ? 'All your wishlist games are hidden. Show them again below.'
      : w.status ? '' : 'Press Refresh prices to read your Steam wishlist.';
    const unsold = sectionNote(rows, (r) => r.item.title);
    page.replaceChildren(...[
      ...header(w.status ? h('p.hint.deal-note', { role: 'status' }, w.status) : null),
      rows.length ? list(rows, (r) => wishRow(r, r.badge)) : null,
      unsold ? h('p.hint', {}, unsold) : null,
      !rows.length && empty ? h('div.empty', {}, empty) : null,
      !alertsOnly && waiting.length ? h('p.hint', {}, waitingNote(waiting.length)) : null,
      !alertsOnly && unmatched.length ? h('section.up-group', {}, h('h2', {}, 'No match on IsThereAnyDeal', h('small', {}, ` ${unmatched.length}`)),
        h('p.hint', {}, 'IsThereAnyDeal does not know these Steam apps (it is tried again after a week).'),
        plainList(unmatched, (u) => wishPlainRow(u, 'No match on IsThereAnyDeal'))) : null,
      w.hidden.length ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${w.hidden.length})`),
        h('ul', {}, w.hidden.map((i) => h('li', {}, h('span', {}, i.title),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${i.title} again`, onclick: () => setWishHidden(i, false) }, 'Show again'))))) : null,
    ].filter(Boolean));
  }

  function draw() {
    if (!info.wishlist) view = 'library';
    if (view === 'wishlist') { drawWishlist(); return; }
    const { rows, unmatched, waiting } = buildDeals(store.games, info, { statuses, sort, dir, alertsOnly });
    const scope = rows.length + unmatched.length + waiting.length;
    const hiddenList = hiddenGames(store.games, info);
    const empty = alertsOnly ? 'No price alerts right now. A game gets one when its price reaches its historical low or its lowest in 2 years.'
      : scopeOf(info).size ? 'No games match these filters.'
      : hiddenList.length ? 'All your deals games are hidden. Show them again below.'
      : 'Deals shows games you don’t own and haven’t played yet: in the backlog or up next, with no years played, and ownership Borrowed, Family Share, Nope or empty.';
    // Matched, but no shop sells it: often the match is an old or delisted entry (a game and its "Final Cut"), so say where to look.
    const unsold = sectionNote(rows, (r) => r.game.title);
    page.replaceChildren(...[
      ...header(null),
      rows.length ? list(rows, (r) => libRow(r, r.badge)) : null,
      unsold ? h('p.hint', {}, `${unsold} Wrong game? Open one and tap “Wrong game?” to pick another.`) : null,
      !rows.length && info.configured ? h('div.empty', {}, scope && !alertsOnly ? 'No prices yet. Press Refresh prices.' : empty) : null,
      !alertsOnly && waiting.length ? h('p.hint', {}, waitingNote(waiting.length)) : null,
      !alertsOnly && unmatched.length ? h('section.up-group', {}, h('h2', {}, 'No match on IsThereAnyDeal', h('small', {}, ` ${unmatched.length}`)),
        h('p.hint', {}, 'Open a game to pick the right one by hand.'),
        plainList(unmatched, (u) => libPlainRow(u, u.match.source === 'user' ? 'You said there is no match' : 'No exact match found'))) : null,
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
  return () => { alive = false; clearTimeout(timer); clearTimeout(reloadTimer); closeSheet(); window.removeEventListener('games-changed', onGames); window.removeEventListener('deals-changed', load); };
}
