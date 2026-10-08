// "Link with IGDB": review the games the IGDB sync could not link on its own, one at a time.
// For each: accept a proposal, search IGDB by hand, mark it "not on IGDB", or skip it for now.
import { h, toast } from './util.js';
import { store, linkQueue, linkAccept, linkNone } from './api.js';
import { pickCandidate } from './add.js';
import { scoreTone, candidateLabel, remaining, without, progressText, gameMeta } from './linkqueue.js';

export function renderLink(root) {
  let items = null; // null while loading
  const skipped = new Set();
  let busy = false;
  let alive = true;

  const head = h('div.link-head');
  const card = h('div.link-card');
  root.replaceChildren(h('div.manage.link-page', {}, h('h1', {}, 'Link with IGDB'), head, card));

  async function refresh(keepFocus = false) {
    try { items = await linkQueue(); } catch { items = []; toast('Could not load the review queue'); }
    if (alive) draw(keepFocus);
  }

  async function act(fn, it) {
    if (busy) return;
    busy = true;
    let stale = false;
    try { await fn(); } catch (e) {
      if (e.status === 409 && e.data?.code === 'already_linked') { stale = true; toast('That game was already linked elsewhere. Nothing was changed.'); }
      else toast(e.status === 409 ? e.message : 'Something went wrong. Check the connection and try again');
    }
    busy = false;
    if (stale) { items = without(items, it.game_id); refresh(true); }
    if (alive) draw(true);
  }

  function accept(it, igdbId) {
    return act(async () => {
      await linkAccept(it.game_id, igdbId);
      items = without(items, it.game_id);
      toast(`Linked “${it.title}”`);
    }, it);
  }

  async function search(it) {
    const c = await pickCandidate({ query: it.title, excludeId: it.game_id });
    if (c) await accept(it, c.igdb_id);
  }

  function none(it) {
    return act(async () => {
      await linkNone(it.game_id);
      items = without(items, it.game_id);
      toast(`“${it.title}” marked as not on IGDB`);
    }, it);
  }

  function candidate(it, c) {
    return h('button.link-cand', { type: 'button', onclick: () => accept(it, c.igdb_id) },
      h('span.link-cand-main', {}, h('span.link-cand-name', {}, candidateLabel(c)),
        c.platforms?.length ? h('span.link-cand-sub', {}, c.platforms.join(', ')) : null),
      h('span.link-score', { class: scoreTone(c.score), title: 'How closely the title matches' }, `${c.score}%`),
      h('span.link-go', {}, 'Link'));
  }

  function draw(focus = false) {
    if (items === null) { card.replaceChildren(h('p.hint', {}, 'Loading…')); return; }
    if (store.linkPending !== items.length) { store.linkPending = items.length; window.dispatchEvent(new Event('views-changed')); } // nav link
    head.replaceChildren(h('p.hint', {}, progressText(items, skipped)));
    const left = remaining(items, skipped);
    if (items.length === 0) {
      card.replaceChildren(h('div.empty', {}, 'Every game that could be matched is linked to IGDB.',
        h('p.hint', {}, 'Games that are still unlinked are found by running ', h('code', {}, 'deltabacklog sync-igdb'), ' on the server.'),
        h('a.btn', { href: '#/library' }, 'Back to the Library')));
      return;
    }
    if (left.length === 0) {
      card.replaceChildren(h('div.empty', {}, 'You skipped the rest for now.',
        h('button.btn', { type: 'button', onclick: () => { skipped.clear(); draw(); } }, 'Review them again')));
      return;
    }
    const it = left[0];
    const st = store.statusByCode.get(it.status);
    card.replaceChildren(
      h('h2.link-title', { tabindex: '-1' }, it.title),
      h('p.hint', {}, [gameMeta(it), st && st.label].filter(Boolean).join(' · ')),
      it.candidates.length
        ? h('div.link-cands', {}, h('p.hint', {}, it.candidates[0].score < 60 ? 'No close match. These are the top results on IGDB, in case one is right:' : 'IGDB suggests:'),
          ...it.candidates.map((c) => h('div.link-row', {}, candidate(it, c),
            c.slug ? h('a.link-page-link', { href: `https://www.igdb.com/games/${encodeURIComponent(c.slug)}`, target: '_blank', rel: 'noopener', 'aria-label': `Open ${c.name} on IGDB` }, 'IGDB ↗') : null)))
        : h('p.hint', {}, 'IGDB did not find a close match for this title.'),
      h('div.link-actions', {},
        h('button.btn', { type: 'button', onclick: () => search(it) }, 'Search IGDB…'),
        h('button.btn', { type: 'button', onclick: () => none(it) }, 'Not on IGDB'),
        h('button.btn', { type: 'button', onclick: () => { skipped.add(it.game_id); draw(true); } }, 'Skip')));
    if (focus) card.querySelector('.link-title')?.focus({ preventScroll: true });
  }

  draw();
  refresh();
  return () => { alive = false; };
}
