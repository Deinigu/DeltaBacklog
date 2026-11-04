// Deals page: the current best price and the historical low of the games you don't own and haven't played yet, from
// IsThereAnyDeal. Nothing here asks ITAD on its own: the prices are read when the owner presses Refresh prices.
import { h, gradientFor, initials, lazyImg, toast, toastAction, newTab, toastError, errorText, emptyBox, brand, debounce, norm, icon, plural } from './util.js';
import { openLayer, replaceUrl } from './layers.js';
import { createSearchBox, createFilterButton, createFilterShell, filterGroup, createSheet, pill } from './filtershell.js';
import { hashWithGame, gameIdFromHash } from './gameurl.js';
import { LOCALE } from './format.js';
import { store, coverURL, posterURL, dealsList, dealsRefresh, dealsSetCountry, dealsStores, dealsSetStores, dealsShops, dealsHide, dealsUnhide, dealsMute, dealsAlertsSeen, dealsTelegramTest,
  dealsWishHide, dealsWishUnhide, dealsWishMute, dealsWishUnmute } from './api.js';
import { openDrawer } from './drawer.js';
import { openAdd } from './add.js';
import { formatDate } from './format.js';
import { DEAL_STATUSES, SORT_KEYS, COLUMNS, BADGE_LABEL, ALERT_LABEL, MIN_CUTS, MAX_PRICES, LOW_TYPES, ENDS_DAYS, ADDED_DAYS, emptyDealsFilter, dealsFilterCount, rowPasses, storesOf, dealsStateFromQuery, dealsStateToQuery, buildDeals, buildWishlist, hiddenGames, scopeOf, steamStoreURL, formatMoney, oldestCheck, safeLink, priceLine, lowLine, tagsOf, checkedLabel, altLine, moveStore, addStore, removeStore, parseThreshold, unusedShops, DEFAULT_THRESHOLD, MAX_STORES } from './deals.js';

export function renderDeals(root) {
  let info = { configured: false, country: 'ES', matches: [], prices: [], refresh: { running: false } };
  // The page state lives in the address (#/deals?view=wishlist&min=50...), so a reload and Back keep it.
  const start = dealsStateFromQuery(location.hash.split('?')[1]);
  const filter = start.filter; // see emptyDealsFilter in deals.js
  let sort = start.sort;
  let dir = start.dir; // 1 = the sort's natural order, -1 = reversed (a click on the active column header)
  let view = start.view; // 'library' (your games) or 'wishlist' (your Steam wishlist; only when the server has a STEAM_ID)
  let loaded = false; // the server has answered (or failed): until then a Wishlist address is not given up for lack of a wishlist
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
    loaded = true;
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
      toastError('start the refresh', e);
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
      toastError('change the country', e);
    }
    draw();
  }

  async function setHidden(game, hide) {
    try {
      const refresh = info.refresh;
      info = { ...(hide ? await dealsHide(game.id) : await dealsUnhide(game.id)), refresh };
    } catch (e) { toastError(hide ? 'hide the game' : 'show the game again', e); return; }
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
    } catch (e) { toastError(hide ? 'hide the game' : 'show the game again', e); return; }
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
    } catch (e) { toastError('change the alerts', e); return; }
    store.dealsUnseen = info.unseen || 0;
    window.dispatchEvent(new Event('deals-unseen'));
    draw();
    say(`Alerts for ${item.title} are ${off ? 'off' : 'on'}.`);
  }

  async function mute(game) {
    try {
      const refresh = info.refresh;
      info = { ...(await dealsMute(game.id)), refresh };
    } catch (e) { toastError('turn the alerts off', e); return; }
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
    const altLink = p && altLine(p) ? safeLink(p.alt_url) : null;
    const act = (label, fn, cls = '') => h('button.dl-act' + cls, { type: 'button', onclick: () => { closeSheet(); fn(); } }, label);
    const actions = [
      link ? h('a.dl-act', { href: link, target: '_blank', rel: 'noopener noreferrer', onclick: closeSheet }, newTab('View deal')) : null,
      altLink ? h('a.dl-act', { href: altLink, target: '_blank', rel: 'noopener noreferrer', onclick: closeSheet }, newTab(`View at ${p.alt_shop}`)) : null,
      ...o.actions.map((a) => (a.href ? h('a.dl-act', { href: a.href, target: '_blank', rel: 'noopener noreferrer', onclick: closeSheet }, newTab(a.label.replace(/ ↗$/, ''))) : act(a.label, a.run))),
    ];
    const lines = p ? [priceLine(p), altLine(p) ? `Cheaper: ${altLine(p)}` : null, lowLine(p) + (p.low_at && p.low_price != null ? `, ${formatDate(p.low_at)}` : ''),
      p.price != null && p.expiry && formatDate(p.expiry) ? `Ends ${formatDate(p.expiry)}` : null] : [o.note];
    const dlg = h('dialog.dl-sheet', { 'aria-label': `${o.title}: actions` },
      h('div.dl-sheet-head', {}, imageCell(o.image, o.title),
        h('div', {}, h('b.dl-sheet-title', {}, o.title), ...lines.filter(Boolean).map((l) => h('div.dl-sub', {}, l)),
          ...(o.tags || []).length ? [h('div.dl-tags', {}, o.tags.map((t) => h('span.dl-tag.' + t.cls, {}, t.text)))] : [])),
      h('div.dl-acts', {}, ...actions),
      h('button.dl-act.dl-close', { type: 'button', onclick: closeSheet }, 'Close'));
    dlg.addEventListener('click', (e) => { if (e.target === dlg) closeSheet(); }); // a tap on the backdrop
    document.body.append(dlg);
    sheetEl = dlg;
    // A native modal <dialog> traps Tab and handles Escape itself; the layer adds Back and the scroll lock.
    const release = openLayer(dlg, { requestClose: closeSheet, trap: false });
    dlg.addEventListener('close', () => { dlg.remove(); if (sheetEl === dlg) sheetEl = null; release(); });
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
  }

  // ---- preferred stores ----
  // A sheet to rank the shops (best first): the price of a game is the one at the highest-ranked shop that sells it, and a
  // clearly cheaper offer elsewhere is shown as a hint. Edits a draft; Save sends it. It applies at the next refresh.
  async function openStores() {
    closeSheet();
    let list = [...(info.stores?.stores || [])];
    let threshold = info.stores?.threshold ?? DEFAULT_THRESHOLD;
    let shops = [];
    const body = h('div.dl-stores-body');
    const err = h('p.hint.dl-stores-err', { role: 'alert' });
    const dlg = h('dialog.dl-sheet.dl-stores', { 'aria-label': 'Preferred stores' }, body);
    const paint = (focusKey) => {
      const typed = body.querySelector('.dl-store-add input')?.value ?? '';
      const rows = list.map((s, i) => h('li.dl-store', {},
        h('span.dl-store-n', {}, String(i + 1)), h('span.dl-store-name', {}, s),
        h('button.dl-store-b', { type: 'button', 'aria-label': `Move ${s} up`, disabled: i === 0 || null, 'data-fk': `up-${i}`, onclick: () => { list = moveStore(list, i, -1); paint(`up-${Math.max(i - 1, 0)}`); } }, '↑'),
        h('button.dl-store-b', { type: 'button', 'aria-label': `Move ${s} down`, disabled: i === list.length - 1 || null, 'data-fk': `down-${i}`, onclick: () => { list = moveStore(list, i, 1); paint(`down-${Math.min(i + 1, list.length - 1)}`); } }, '↓'),
        h('button.dl-store-b', { type: 'button', 'aria-label': `Remove ${s}`, 'data-fk': `rm-${i}`, onclick: () => { list = removeStore(list, i); paint(`rm-${Math.min(i, list.length - 1)}`); } }, '✕')));
      const input = h('input', { 'data-fk': 'add', value: typed, list: 'dl-shops', placeholder: 'Add a store…', 'aria-label': 'Store to add', maxlength: 60, autocomplete: 'off',
        onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } } });
      const add = () => { const next = addStore(list, input.value); if (next === list && input.value.trim()) err.textContent = list.length >= MAX_STORES ? `At most ${MAX_STORES} stores.` : 'That store is already in the list.'; else err.textContent = ''; if (next !== list) input.value = ''; list = next; paint('add'); };
      const th = h('input', { type: 'number', min: 0, max: 100, step: 1, value: String(threshold), inputmode: 'numeric', 'aria-label': 'Percent cheaper before another store is shown',
        oninput: (e) => { const n = parseThreshold(e.target.value); if (n != null) threshold = n; } });
      body.replaceChildren(
        h('b.dl-sheet-title', {}, 'Preferred stores'),
        h('p.hint', {}, 'The price shown is the one at the first store on this list that sells the game. Stores you do not list come after, cheapest first. Leave the list empty to always show the cheapest price. Changes apply when you press Refresh prices; price alerts follow the store you rank first.'),
        list.length ? h('ol.dl-stores-list', {}, ...rows) : h('p.hint', {}, 'No store ranked: the cheapest price is shown.'),
        h('div.dl-store-add', {}, input, h('button.dl-act', { type: 'button', onclick: add }, 'Add'),
          h('datalist', { id: 'dl-shops' }, unusedShops(shops, list).map((s) => h('option', { value: s })))),
        h('label.dl-store-th', {}, h('span', {}, 'Also show a cheaper offer elsewhere when it is more than'), th, h('span', {}, '% cheaper')),
        err,
        h('div.dl-acts', {}, h('button.dl-act.dl-save', { type: 'button', onclick: save }, 'Save'), h('button.dl-act.dl-close', { type: 'button', onclick: closeSheet }, 'Cancel')));
      if (focusKey) (body.querySelector(`[data-fk="${focusKey}"]:not(:disabled)`) || body.querySelector('[data-fk="add"]'))?.focus();
    };
    async function save() {
      try {
        const refresh = info.refresh;
        info = { ...(await dealsSetStores(list, threshold)), refresh };
        closeSheet();
        toast(list.length ? 'Preferred stores saved. Press Refresh prices to apply them.' : 'Preferred stores cleared. Press Refresh prices to apply.', 4000);
        draw();
      } catch (e) { err.textContent = errorText('save', e); }
    }
    dlg.addEventListener('click', (e) => { if (e.target === dlg) closeSheet(); });
    dlg.addEventListener('close', () => { dlg.remove(); if (sheetEl === dlg) sheetEl = null; });
    document.body.append(dlg);
    sheetEl = dlg;
    paint();
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
    if (info.configured) { try { shops = (await dealsShops()).shops || []; if (sheetEl === dlg) paint(); } catch { /* the box still takes a typed name */ } }
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
    const alt = p ? altLine(p) : '';
    const sub = p ? lowLine(p) + (ends ? ` · Ends ${ends}` : '') : o.note;
    const open = () => openSheet({ ...o, tags });
    const cls = 'li.dl-row' + (o.badge === 'low' ? '.is-low' : '') + (o.alert ? '.has-alert' : '') + (p ? '' : '.no-price');
    return h(cls, { role: 'row' },
      imageCell(o.image, o.title),
      h('span.dl-name', { role: 'cell' },
        h('button.dl-open', { type: 'button', 'aria-haspopup': 'dialog', 'aria-label': o.aria, onclick: open }, h('span.dl-title', {}, o.title)),
        tags.length ? h('span.dl-tags', {}, tags.map((t) => h('span.dl-tag.' + t.cls, {}, t.text))) : null,
        h('span.dl-sub', {}, sub),
        alt ? h('span.dl-altline', {}, alt) : null),
      h('span.dl-cut', { role: 'cell' }, onSale ? h('span.dl-pct', {}, `-${p.cut}%`) : null),
      h('span.dl-price', { role: 'cell' }, p && p.price != null ? [h('b', {}, formatMoney(p.price, p.currency)), onSale && p.regular > p.price ? h('s', {}, formatMoney(p.regular, p.currency)) : null]
        : p ? h('span.dl-none', {}, 'Not sold') : null),
      h('span.dl-low', { role: 'cell' }, p && p.low_price != null ? formatMoney(p.low_price, p.currency) : ''),
      h('span.dl-shop', { role: 'cell', title: alt ? `Cheaper: ${alt}` : null }, p && p.price != null ? p.shop || '' : ''),
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

  // ---- the toolbar: the Library's search, Filters and sort buttons, then the live count and the active filters ----
  const searchBox = createSearchBox({ value: filter.q, placeholder: 'Search deals…', label: 'Search deals', onClear: () => { filter.q = ''; draw(); } });
  const searchInput = searchBox.input;
  searchInput.addEventListener('input', debounce(() => { filter.q = norm(searchInput.value.trim()); draw(); }, 90));
  const filterBtn = createFilterButton(() => shell.setSheet(true));
  const sortBtn = h('button.btn.sort-btn', { type: 'button', 'aria-label': 'Sort', 'aria-haspopup': 'dialog', onclick: openSort }, icon('sort'), h('span.sort-label'));
  const meta = h('div.meta-row');
  const count = h('span.count', { role: 'status' }); // one live region for the count: "12 of 80 games" is announced as the filters change
  const toolbar = h('div.toolbar', {}, h('div.toolbar-row', {}, searchBox.el, filterBtn.btn, sortBtn), meta);

  // ---- the sort sheet ----
  const sortList = h('div.chips');
  const reverseBtn = h('button.chip', { type: 'button', onclick: () => { dir = -dir; draw(); paintSort(); reverseBtn.focus(); } }, '↕ Reverse the order');
  const sortSheet = createSheet({ label: 'Sort', title: 'Sort by',
    body: [h('p.hint', {}, 'On a PC the column headers sort too.'), sortList, h('div.chips.dl-reverse', {}, reverseBtn)],
    foot: (close) => [h('button.btn', { type: 'button', onclick: () => { sort = 'cut'; dir = 1; draw(); paintSort(); } }, 'Reset'), h('button.btn.primary', { type: 'button', onclick: close }, 'Done')] });
  function paintSort() {
    sortList.replaceChildren(...SORT_KEYS.filter((s) => !s.wish || view === 'wishlist').map((s) => h('button.chip', { type: 'button', 'data-key': s.key, 'aria-pressed': String(s.key === sort),
      onclick: () => { sort = s.key; dir = 1; draw(); paintSort(); sortList.querySelector(`[data-key="${s.key}"]`)?.focus(); } }, s.label)));
    reverseBtn.setAttribute('aria-pressed', String(dir === -1));
  }
  function openSort() { closeSheet(); paintSort(); sortSheet.toggle(true); }

  // ---- the filter panel: the same chips and groups as the Library's, built from the filters of deals.js ----
  const chipRefs = []; // {btn, label, text, count, on, none}
  function chip(text, { on, toggle, count }) {
    const label = h('span', {}, text), c = h('span.c');
    const btn = h('button.chip', { type: 'button', 'aria-pressed': 'false', onclick: () => { toggle(); draw(); } }, label, c);
    const ref = { btn, label, text, c, on, count };
    chipRefs.push(ref);
    return ref;
  }
  const flip = (key) => () => { filter[key] = !filter[key]; };
  const setOne = (key, v) => () => { filter[key] = filter[key] === v ? 0 : v; };
  const toggleIn = (set, v) => () => { if (set.has(v)) set.delete(v); else set.add(v); };
  const group = (label, refs, o = {}) => ({ ...filterGroup(label, { open: true, chips: h('div.chips', {}, refs.map((r) => r.btn)), ...o }), refs });

  const statusRefs = DEAL_STATUSES.map((s) => chip(s.label, { on: () => filter.statuses.has(s.code), toggle: toggleIn(filter.statuses, s.code),
    count: () => { const scope = scopeOf(info); return store.games.filter((g) => g.status === s.code && scope.has(g.id)).length; } }));
  const showRefs = [
    chip('On sale', { on: () => filter.sale, toggle: flip('sale'), count: () => ({ sale: true }) }),
    chip('Alerts', { on: () => filter.alerts, toggle: flip('alerts'), count: () => ({ alerts: true }) }),
    chip(`Ends within ${ENDS_DAYS} days`, { on: () => filter.ends, toggle: flip('ends'), count: () => ({ ends: true }) }),
    chip(`Added in the last ${ADDED_DAYS} days`, { on: () => filter.added, toggle: flip('added'), count: () => ({ added: true }) })];
  const [saleChip, alertsChip, endsChip, addedChip] = showRefs;
  alertsChip.btn.title = 'Games at their historical low or their lowest price in 2 years';
  const cutRefs = [chip('Any', { on: () => !filter.minCut, toggle: () => { filter.minCut = 0; }, count: () => ({ minCut: 0 }) }),
    ...MIN_CUTS.map((v) => chip(`${v}%+`, { on: () => filter.minCut === v, toggle: setOne('minCut', v), count: () => ({ minCut: v }) }))];
  const priceRefs = [chip('Any', { on: () => !filter.maxPrice, toggle: () => { filter.maxPrice = 0; }, count: () => ({ maxPrice: 0 }) }),
    ...MAX_PRICES.map((v) => chip('', { on: () => filter.maxPrice === v, toggle: setOne('maxPrice', v), count: () => ({ maxPrice: v }) }))];
  priceRefs.slice(1).forEach((r, i) => { r.money = MAX_PRICES[i]; });
  const lowRefs = LOW_TYPES.map((l) => chip(l.label, { on: () => filter.lows.has(l.key), toggle: toggleIn(filter.lows, l.key), count: () => ({ lows: new Set([l.key]) }) }));
  const storeChips = h('div.chips');
  let storeRefs = [], storeSig = '';

  const groups = {
    status: group('Status', statusRefs),
    show: group('Show', showRefs),
    cut: group('Minimum discount', cutRefs),
    price: group('Maximum price', priceRefs),
    low: group('Low price', lowRefs),
    store: filterGroup('Store', { open: true, chips: storeChips }),
  };
  const countryInput = h('input', { value: info.country || 'ES', maxlength: 2, autocapitalize: 'characters', list: 'dl-countries', 'aria-label': 'Country code for prices, for example ES', autocomplete: 'off',
    onchange: (e) => saveCountry(e.target.value) });
  const countryRow = h('label.dl-country', {}, h('span', {}, 'Prices for'), countryInput,
    h('datalist', { id: 'dl-countries' }, ['ES', 'US', 'GB', 'DE', 'FR', 'IT', 'PT', 'NL', 'PL', 'SE', 'BR', 'MX', 'CA', 'AU', 'JP'].map((c) => h('option', { value: c }))));
  const resetBtn = h('button.btn', { type: 'button', onclick: resetFilters }, 'Reset');
  const showBtn = h('button.btn.primary', { type: 'button', onclick: () => shell.setSheet(false) });
  const shell = createFilterShell({
    body: [groups.status.el, groups.show.el, groups.cut.el, groups.price.el, groups.low.el, groups.store.el, countryRow],
    foot: [resetBtn, showBtn] });
  const results = h('section.dl-results', { 'aria-label': 'Deals' });

  function resetFilters() {
    const fresh = emptyDealsFilter();
    Object.assign(filter, fresh);
    searchInput.value = '';
    draw();
  }
  function setView(v) {
    if (v === view) return;
    view = v;
    // What only one list has (a status, an "added" date) or what differs between the lists (the shops) does not carry over.
    filter.statuses.clear(); filter.stores.clear(); filter.added = false;
    closeSheet();
    draw();
  }

  // ---- the page head: title, (i), refresh and when it was checked, then the Library / Wishlist switch ----
  let infoOpen = false; // the one-line explanation behind the (i) button
  const infoBtn = h('button.dl-info', { type: 'button', 'aria-expanded': 'false', 'aria-label': 'About this page', onclick: () => { infoOpen = !infoOpen; paintHead(); } }, 'i');
  const refreshBtn = h('button.dl-refresh', { type: 'button', onclick: refresh }, h('span', { 'aria-hidden': 'true' }, '↻'));
  const checkedEl = h('span.dl-checked');
  const infoPanel = h('div.dl-info-panel', { hidden: true });
  const setupEl = h('p.hint', { hidden: true });
  const libCount = h('span.c'), wishCount = h('span.c');
  const libBtn = h('button', { type: 'button', onclick: () => setView('library') }, 'Library ', libCount);
  const wishBtn = h('button', { type: 'button', onclick: () => setView('wishlist') }, 'Wishlist ', wishCount);
  const seg = h('div.mode.dl-view', { role: 'group', 'aria-label': 'What to list', hidden: true }, libBtn, wishBtn);
  const titleRow = h('div.dl-title-row', {}, h('h1', {}, 'Deals'), infoBtn, h('span.dl-spacer'), h('span.dl-check', {}, checkedEl, refreshBtn));
  let infoKey = '';
  function paintHead() {
    const wl = view === 'wishlist';
    const r = info.refresh || {};
    const old = oldestCheck(info);
    infoBtn.setAttribute('aria-expanded', String(infoOpen));
    infoPanel.hidden = !infoOpen;
    const nStores = info.stores?.stores?.length || 0;
    const key = [infoOpen, wl, nStores, !!info.telegram].join('|');
    if (infoOpen && key !== infoKey) { // rebuilt only when it changes, so a focused button in it is not lost on a redraw
      infoKey = key;
      const text = wl ? 'Your Steam wishlist, not part of your library. A game leaves this list when you add it to your library or take it off your wishlist (for example by buying it); it is read from Steam when you press Refresh prices.'
        : 'The price right now (at your preferred store when you set some, else the best price) and the historical low of the games you don’t own and haven’t played yet, from IsThereAnyDeal. A game at its historical low or its lowest price in 2 years gets an alert.';
      infoPanel.replaceChildren(h('p.hint', {}, text),
        h('div.dl-panel-btns', {}, h('button.dl-tg', { type: 'button', onclick: openStores }, nStores ? `Preferred stores (${nStores})` : 'Preferred stores'),
          info.telegram ? h('button.dl-tg', { type: 'button', onclick: (e) => telegramTest(e.currentTarget) }, 'Send test message') : null));
    } else if (!infoOpen) infoKey = '';
    const setup = !info.configured && !old ? 'IsThereAnyDeal is not set up: add ITAD_API_KEY to .env and restart the app (see the user manual, Deals).' : '';
    setupEl.hidden = !setup;
    setupEl.textContent = setup;
    refreshBtn.disabled = !info.configured || r.running;
    refreshBtn.setAttribute('aria-label', r.running ? 'Reading prices' : 'Refresh prices');
    refreshBtn.title = info.configured ? 'Refresh prices' : 'IsThereAnyDeal is not set up (ITAD_API_KEY is missing from .env)';
    refreshBtn.firstChild.className = r.running ? 'spin' : '';
    checkedEl.textContent = r.running ? `Reading prices… ${r.total ? `${r.done}/${r.total}` : ''}` : old ? `Checked ${checkedLabel(old)}` : 'Not checked yet';
    seg.hidden = !info.wishlist;
    libBtn.setAttribute('aria-pressed', String(!wl));
    wishBtn.setAttribute('aria-pressed', String(wl));
    libCount.textContent = String(scopeOf(info).size);
    wishCount.textContent = String(info.wishlist?.items?.length ?? 0);
    countryRow.hidden = !info.configured;
    if (document.activeElement !== countryInput) countryInput.value = info.country || 'ES';
  }

  // ---- filling the page ----
  const money = (units, currency) => {
    try { return new Intl.NumberFormat(LOCALE, { style: 'currency', currency, maximumFractionDigits: 0 }).format(units); } catch { return `${units} ${currency}`; }
  };
  const sectionNote = (rows, nameOf) => {
    const unsold = rows.filter((r) => r.p.price == null).map(nameOf);
    return unsold.length ? `No shop lists ${unsold.slice(0, 5).join(', ')}${unsold.length > 5 ? ` and ${unsold.length - 5} more` : ''}.` : '';
  };
  const waitingNote = (n) => `${n} ${n === 1 ? 'game is' : 'games are'} waiting for their prices: press Refresh prices.`;
  const clearBtn = () => h('button.btn', { type: 'button', onclick: resetFilters }, 'Clear filters');

  // The Wishlist view: the Steam wishlist games, with the same rows.
  function resultsWishlist(now) {
    const w = info.wishlist;
    const { rows, unmatched, waiting } = buildWishlist(info, { filter, sort, dir, now });
    const filtered = dealsFilterCount(filter, true) > 0 || filter.q;
    const onlyAlerts = filter.alerts && dealsFilterCount(filter, true) === 1 && !filter.q;
    const empty = onlyAlerts ? 'No price alerts right now. A game gets one when its price reaches its historical low or its lowest in 2 years.'
      : w.items.length ? 'No games match these filters.'
      : w.hidden.length ? 'All your wishlist games are hidden. Show them again below.'
      : w.status ? '' : 'Press Refresh prices to read your Steam wishlist.';
    const unsold = sectionNote(rows, (r) => r.item.title);
    return { rowCount: rows.length, nodes: [
      w.status ? h('p.hint.deal-note', { role: 'status' }, w.status) : null,
      rows.length ? list(rows, (r) => wishRow(r, r.badge)) : null,
      unsold ? h('p.hint', {}, unsold) : null,
      !rows.length && empty ? emptyBox(empty, filtered && w.items.length ? clearBtn() : null) : null,
      waiting.length ? h('p.hint', {}, waitingNote(waiting.length)) : null,
      unmatched.length ? h('section.up-group', {}, h('h2', {}, 'No match on ', brand('IsThereAnyDeal'), h('small', {}, ` ${unmatched.length}`)),
        h('p.hint', {}, 'IsThereAnyDeal does not know these Steam apps (it is tried again after a week).'),
        plainList(unmatched, (u) => wishPlainRow(u, 'No match on IsThereAnyDeal'))) : null,
      w.hidden.length ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${w.hidden.length})`),
        h('ul', {}, w.hidden.map((i) => h('li', {}, h('span', {}, i.title),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${i.title} again`, onclick: () => setWishHidden(i, false) }, 'Show again'))))) : null,
    ].filter(Boolean) };
  }

  function resultsLibrary(now) {
    const { rows, unmatched, waiting } = buildDeals(store.games, info, { filter, sort, dir, now });
    const scope = rows.length + unmatched.length + waiting.length;
    const hiddenList = hiddenGames(store.games, info);
    const filtered = dealsFilterCount(filter) > 0 || filter.q;
    const onlyAlerts = filter.alerts && dealsFilterCount(filter) === 1 && !filter.q;
    const empty = onlyAlerts ? 'No price alerts right now. A game gets one when its price reaches its historical low or its lowest in 2 years.'
      : scopeOf(info).size ? 'No games match these filters.'
      : hiddenList.length ? 'All your deals games are hidden. Show them again below.'
      : 'Deals shows games you don’t own and haven’t played yet: in the backlog or up next, with no years played, and ownership Borrowed, Family Share, Nope or empty.';
    // Matched, but no shop sells it: often the match is an old or delisted entry (a game and its "Final Cut"), so say where to look.
    const unsold = sectionNote(rows, (r) => r.game.title);
    return { rowCount: rows.length, nodes: [
      rows.length ? list(rows, (r) => libRow(r, r.badge)) : null,
      unsold ? h('p.hint', {}, `${unsold} Wrong game? Open one and tap “Wrong game?” to pick another.`) : null,
      !rows.length && info.configured ? emptyBox(scope && !filtered ? 'No prices yet. Press Refresh prices.' : empty, filtered && scopeOf(info).size ? clearBtn() : null) : null,
      waiting.length ? h('p.hint', {}, waitingNote(waiting.length)) : null,
      unmatched.length ? h('section.up-group', {}, h('h2', {}, 'No match on ', brand('IsThereAnyDeal'), h('small', {}, ` ${unmatched.length}`)),
        h('p.hint', {}, 'Open a game to pick the right one by hand.'),
        plainList(unmatched, (u) => libPlainRow(u, u.match.source === 'user' ? 'You said there is no match' : 'No exact match found'))) : null,
      hiddenList.length ? h('details.hidden-list', {},
        h('summary', {}, `Hidden (${hiddenList.length})`),
        h('ul', {}, hiddenList.map((g) => h('li', {}, h('span', {}, g.title),
          h('button.hide-chip', { type: 'button', 'aria-label': `Show ${g.title} again`, onclick: () => setHidden(g, false) }, 'Show again'))))) : null,
    ].filter(Boolean) };
  }

  /** An active-filter pill: tapping it removes the filter (the Library's pills). */
  function paintPills(base, wl, rowCount) {
    const off = (fn) => () => { fn(); draw(); };
    const cur = base.find((r) => r.p?.currency)?.p.currency || 'EUR';
    const pills = [];
    if (!wl) for (const s of DEAL_STATUSES) if (filter.statuses.has(s.code)) pills.push(pill('', `Status: ${s.label}`, off(() => filter.statuses.delete(s.code)), h('b', {}, 'Status'), s.label));
    if (filter.sale) pills.push(pill('', 'On sale', off(() => { filter.sale = false; }), 'On sale'));
    if (filter.minCut) pills.push(pill('', `Discount ${filter.minCut}% or more`, off(() => { filter.minCut = 0; }), h('b', {}, 'Discount'), `${filter.minCut}%+`));
    if (filter.maxPrice) pills.push(pill('', `Price under ${money(filter.maxPrice, cur)}`, off(() => { filter.maxPrice = 0; }), h('b', {}, 'Price'), `under ${money(filter.maxPrice, cur)}`));
    for (const l of LOW_TYPES) if (filter.lows.has(l.key)) pills.push(pill('', l.label, off(() => filter.lows.delete(l.key)), l.label));
    if (filter.alerts) pills.push(pill('', 'Alerts', off(() => { filter.alerts = false; }), 'Alerts'));
    for (const s of filter.stores) pills.push(pill('', `Store: ${s}`, off(() => filter.stores.delete(s)), h('b', {}, 'Store'), s));
    if (filter.ends) pills.push(pill('', `Ends within ${ENDS_DAYS} days`, off(() => { filter.ends = false; }), `Ends within ${ENDS_DAYS} days`));
    if (wl && filter.added) pills.push(pill('', `Added in the last ${ADDED_DAYS} days`, off(() => { filter.added = false; }), `Added in the last ${ADDED_DAYS} days`));
    const revert = (pills.length || filter.q) && h('button.pill.reset', { type: 'button', onclick: resetFilters }, 'Reset');
    meta.replaceChildren(count, ...pills, revert);
    count.textContent = base.length ? `${rowCount} of ${plural(base.length, 'game')}` : '';
  }

  /** Brings the panel's chips in line with the filter: states, the counts they would give, the shops that exist. */
  function paintPanel(base, wl, now, cur) {
    // the shops present in this list (and the ones in the filter, so a saved link never loses a condition)
    const shops = storesOf(base);
    for (const s of filter.stores) if (!shops.some((x) => x.name === s)) shops.push({ name: s, count: 0 });
    const sig = shops.map((s) => s.name).join('\u0001');
    if (sig !== storeSig) {
      storeSig = sig;
      storeRefs = shops.map((s) => {
        const c = h('span.c');
        const btn = h('button.chip', { type: 'button', onclick: () => { toggleIn(filter.stores, s.name)(); draw(); } }, h('span', {}, s.name), c);
        return { btn, c, on: () => filter.stores.has(s.name), count: () => ({ stores: new Set([s.name]) }) };
      });
      storeChips.replaceChildren(...storeRefs.map((r) => r.btn));
    }
    for (const r of priceRefs) if (r.money) r.label.textContent = `Under ${money(r.money, cur)}`;
    for (const r of [...chipRefs, ...storeRefs]) {
      const selected = r.on();
      let n = r.count();
      if (typeof n === 'object') n = base.filter((row) => rowPasses({ ...filter, ...n }, row, now)).length;
      r.c.textContent = n;
      r.btn.setAttribute('aria-pressed', String(selected));
      r.btn.classList.toggle('zero', n === 0 && !selected);
    }
    addedChip.btn.hidden = !wl;
    groups.status.el.hidden = wl;
    groups.store.el.hidden = !shops.length;
    const n = (set) => set.size || '';
    groups.status.n.textContent = n(filter.statuses);
    groups.show.n.textContent = (filter.sale ? 1 : 0) + (filter.alerts ? 1 : 0) + (filter.ends ? 1 : 0) + (wl && filter.added ? 1 : 0) || '';
    groups.cut.n.textContent = filter.minCut ? 1 : '';
    groups.price.n.textContent = filter.maxPrice ? 1 : '';
    groups.low.n.textContent = n(filter.lows);
    groups.store.n.textContent = n(filter.stores);
  }

  function draw() {
    if (!alive) return;
    if (loaded && !info.wishlist) view = 'library';
    const wl = view === 'wishlist';
    const now = new Date();
    const nFilters = dealsFilterCount(filter, wl);
    let rowCount = 0;
    const base = wl ? (info.wishlist ? buildWishlist(info, { now }).rows : []) : buildDeals(store.games, info, { now }).rows;
    if (wl && !info.wishlist) results.replaceChildren(); // the list is not known yet (the page was opened on the Wishlist and the answer is still on its way)
    else { const r = wl ? resultsWishlist(now) : resultsLibrary(now); results.replaceChildren(...r.nodes); rowCount = r.rowCount; }
    const keys = SORT_KEYS.filter((s) => !s.wish || wl);
    if (!keys.some((s) => s.key === sort)) { sort = 'cut'; dir = 1; }
    sortBtn.querySelector('.sort-label').textContent = (SORT_KEYS.find((s) => s.key === sort)?.label || '') + (dir === -1 ? ' ↑' : '');
    paintHead();
    paintPanel(base, wl, now, base.find((r) => r.p?.currency)?.p.currency || 'EUR');
    paintPills(base, wl, rowCount);
    filterBtn.setCount(nFilters);
    showBtn.textContent = `Show ${plural(rowCount, 'game')}`;
    if (sortSheet.sheet.classList.contains('open')) paintSort();
    const qs = dealsStateToQuery({ view, filter, sort, dir });
    replaceUrl(hashWithGame('#/deals' + (qs ? '?' + qs : ''), gameIdFromHash(location.hash))); // an open game stays in the address
  }

  // The filter panel sticks below the toolbar, whose height varies (wrapped pills). On a phone the toolbar is pinned to the
  // top of the screen once the title has scrolled away; then it also clears the notch.
  const syncToolbar = () => {
    document.documentElement.style.setProperty('--toolbar-h', toolbar.offsetHeight + 'px');
    document.documentElement.style.setProperty('--head-h', '0px');
  };
  const syncStuck = () => toolbar.classList.toggle('stuck', toolbar.getBoundingClientRect().top <= 1);
  window.addEventListener('scroll', syncStuck, { passive: true });
  const ro = new ResizeObserver(syncToolbar);
  ro.observe(toolbar);
  const onKey = (e) => {
    if (e.key === '/' && document.activeElement !== searchInput && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName) && !document.querySelector('dialog[open]')) { e.preventDefault(); searchInput.focus(); }
    if (e.key === 'Escape') shell.setSheet(false);
  };
  document.addEventListener('keydown', onKey);

  page.replaceChildren(titleRow, infoPanel, setupEl, seg, toolbar, h('div.library', {}, shell.panel, shell.scrim, results), ...sortSheet.el);
  syncToolbar();

  // A game edited elsewhere may enter or leave the scope (ownership, years played): draw at once, then ask the server again (cache only, no ITAD).
  let reloadTimer = null;
  function onGames() { draw(); clearTimeout(reloadTimer); reloadTimer = setTimeout(load, 300); }
  window.addEventListener('deals-changed', load);
  draw();
  load();
  window.addEventListener('games-changed', onGames);
  return () => {
    alive = false; clearTimeout(timer); clearTimeout(reloadTimer); closeSheet();
    window.removeEventListener('games-changed', onGames); window.removeEventListener('deals-changed', load);
    window.removeEventListener('scroll', syncStuck); document.removeEventListener('keydown', onKey); ro.disconnect(); shell.destroy();
  };
}
