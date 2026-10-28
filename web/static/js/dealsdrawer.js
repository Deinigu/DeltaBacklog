// The "Price" row of the game drawer, for a Backlog or Up next game: the best price now and the historical low,
// as the server has cached them (never ITAD on open). Also where a wrong or missing match is fixed. It stays out of the way:
// nothing is drawn until the cache answers, when ITAD is not set up, or for other statuses.
import { h, toast } from './util.js';
import { dealsList, dealsSearch, dealsSetMatch } from './api.js';
import { badgeOf, BADGE_LABEL, formatMoney, formatDate, isWatched, matchOf, priceOf, priceLine, safeLink } from './deals.js';

export function dealsSection(game) {
  const box = h('div.deal-drawer', { hidden: true });
  if (!isWatched(game)) return box;
  let info = null;

  function draw() {
    const p = priceOf(info, game.id), m = matchOf(info, game.id);
    const matched = m && m.itad_id;
    const badge = badgeOf(p);
    const link = p && p.price != null ? safeLink(p.url) : null;
    const parts = [h('h3', {}, 'Price')];
    if (p) {
      parts.push(
        h('div', {}, h('b', {}, priceLine(p))),
        badge ? h('div', {}, h('span.deal-badge' + (badge === 'low' ? '.low' : ''), {}, BADGE_LABEL[badge])) : null,
        p.low_price != null ? h('p.hint', {}, `Lowest ever ${formatMoney(p.low_price, p.currency)}${p.low_shop ? ` at ${p.low_shop}` : ''}${p.low_at ? `, ${formatDate(p.low_at)}` : ''}.`) : null,
        h('p.hint', {}, `Checked ${formatDate(p.fetched_at)}${matched && m.title ? ` · ${m.title}` : ''}.`));
    } else {
      parts.push(h('p.hint', {}, matched ? 'Not read yet. Press Refresh prices on the Deals page.'
        : m ? (m.source === 'user' ? 'You said IsThereAnyDeal has no such game.' : 'No exact match on IsThereAnyDeal.') : 'Not checked yet. Press Refresh prices on the Deals page.'));
    }
    const actions = h('div.deal-actions', {},
      link ? h('a.btn', { href: link, target: '_blank', rel: 'noopener noreferrer' }, 'View deal ↗') : null,
      h('button.btn', { type: 'button', onclick: openFix }, matched ? 'Wrong game?' : 'Pick the game'),
      m && m.source === 'user' ? h('button.btn', { type: 'button', onclick: () => save({ reset: true }, 'Back to automatic matching.') }, 'Match automatically') : null);
    parts.push(actions, h('div.deal-fix', { hidden: true }));
    box.replaceChildren(...parts.filter(Boolean));
    box.hidden = false;
  }

  async function save(body, done) {
    try {
      info = await dealsSetMatch(game.id, body);
      toast(done, 3000);
      window.dispatchEvent(new Event('deals-changed'));
    } catch (e) {
      toast(e.message || 'Could not save the match');
    }
    draw();
  }

  function openFix() {
    const fix = box.querySelector('.deal-fix');
    if (!fix.hidden) { fix.hidden = true; return; }
    const input = h('input', { type: 'search', value: game.title, 'aria-label': 'Search IsThereAnyDeal by title', maxlength: 200 });
    const list = h('ul.deal-cands');
    const status = h('p.hint', { role: 'status' });
    async function search(ev) {
      ev?.preventDefault();
      const q = input.value.trim();
      if (!q) return;
      status.textContent = 'Searching…';
      list.replaceChildren();
      try {
        const { games } = await dealsSearch(q);
        status.textContent = games.length ? '' : 'Nothing found.';
        list.replaceChildren(...games.map((g) => h('li', {}, h('button', { type: 'button',
          onclick: () => save({ itad_id: g.id, title: g.title, slug: g.slug || '' }, `Matched with “${g.title}”.`) }, g.title))));
      } catch (e) {
        status.textContent = e.message || 'The search failed.';
      }
    }
    fix.replaceChildren(h('form', { onsubmit: search }, input, h('button.btn', { type: 'submit' }, 'Search')), status, list,
      h('button.btn', { type: 'button', onclick: () => save({ none: true }, 'Marked as having no match.') }, 'No match on IsThereAnyDeal'));
    fix.hidden = false;
    input.focus();
  }

  dealsList().then((resp) => {
    info = resp;
    if (!box.isConnected || (!resp.configured && !priceOf(info, game.id) && !matchOf(info, game.id))) return; // nothing cached and no way to read it
    draw();
  }).catch(() => { /* offline or an old server: no row */ });
  return box;
}
