// The full-screen viewer of the Screenshots page: the drawer viewer's look, but read-only and across every game.
import { h } from './util.js';
import { formatBytes, formatTaken } from './shots.js';
import { stepAcross } from './gallery.js';

/**
 * Open the viewer on `ctx.list()[start]`. ctx: list() the loaded screenshots, total() all matches, loadMore() appends
 * the next page (resolves when done), openGame(id) opens the game's drawer (after the viewer closed), onClose(index)
 * lets the page focus the thumbnail. Returns the function that closes it.
 */
export function openGalleryViewer(ctx, start) {
  let cur = start;
  let loading = false;
  const img = h('img.shot-img', { alt: '' });
  const counter = h('span.shot-counter');
  const title = h('p.shot-game');
  const caption = h('p.shot-cap');
  const meta = h('p.shot-meta');
  const dl = h('a.btn', { 'aria-label': 'Download the original', title: 'Download the original', download: '' }, '⬇');
  const orig = h('a.btn', { title: 'Open the original in a new tab', target: '_blank', rel: 'noopener' }, 'Original');
  const gameBtn = h('button.btn', { type: 'button', onclick: () => { const id = ctx.list()[cur]?.game_id; close(); if (id) ctx.openGame(id); } }, 'Open game');
  const prev = h('button.btn.shot-nav.prev', { type: 'button', 'aria-label': 'Previous screenshot', onclick: () => go(-1) }, '‹');
  const next = h('button.btn.shot-nav.next', { type: 'button', 'aria-label': 'Next screenshot', onclick: () => go(1) }, '›');
  const stage = h('div.shot-stage', {}, prev, img, next);
  const view = h('div.shot-viewer', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Screenshot viewer', tabindex: '-1' },
    h('div.shot-bar', {}, h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕'), counter, h('span.spacer'), orig, dl),
    stage,
    h('div.shot-foot', {}, title, caption, meta, h('div.shot-move', {}, gameBtn)));

  function show() {
    const s = ctx.list()[cur];
    if (!s) { close(); return; }
    img.src = s.src;
    img.alt = s.caption || `${s.game_title} screenshot`;
    counter.textContent = `${cur + 1} / ${ctx.total()}`;
    title.textContent = s.game_title;
    caption.textContent = s.caption || '';
    caption.hidden = !s.caption;
    meta.textContent = [formatTaken(s.date), `${s.width}×${s.height}`, formatBytes(s.bytes)].filter(Boolean).join(' · ');
    dl.href = `${s.src}?download=1`;
    orig.href = s.src;
    prev.hidden = next.hidden = ctx.total() < 2;
  }

  async function go(dir) {
    if (loading) return;
    const r = stepAcross(cur, ctx.list().length, ctx.total(), dir);
    if (r.more) {
      loading = true;
      try { await ctx.loadMore(); } catch { /* stay where we are */ }
      loading = false;
      cur = stepAcross(cur, ctx.list().length, ctx.total(), 1).index;
    } else cur = r.index;
    show();
  }

  function close() {
    document.removeEventListener('keydown', onKey);
    view.remove();
    document.body.classList.remove('no-scroll');
    ctx.onClose?.(cur);
  }

  const focusables = () => [...view.querySelectorAll('button, a[href]')].filter((el) => !el.hidden && el.offsetParent !== null);
  function onKey(e) {
    if (e.key === 'Tab') { // keep Tab inside the viewer
      const list = focusables();
      if (!list.length) { e.preventDefault(); return; }
      const first = list[0];
      const last = list[list.length - 1];
      if (e.shiftKey && (document.activeElement === first || document.activeElement === view)) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      else if (!view.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
      return;
    }
    if (e.key === 'Escape') close();
    else if (e.key === 'ArrowLeft') go(-1);
    else if (e.key === 'ArrowRight') go(1);
  }

  // Swipe to change picture, only at normal zoom (a pinch-zoomed picture is panned instead).
  let x0 = null;
  let y0 = 0;
  stage.addEventListener('touchstart', (e) => {
    const zoomed = (window.visualViewport?.scale || 1) > 1.05;
    if (e.touches.length === 1 && !zoomed) { x0 = e.touches[0].clientX; y0 = e.touches[0].clientY; } else x0 = null;
  }, { passive: true });
  stage.addEventListener('touchend', (e) => {
    if (x0 == null) return;
    const dx = e.changedTouches[0].clientX - x0;
    const dy = e.changedTouches[0].clientY - y0;
    x0 = null;
    if (Math.abs(dx) > 60 && Math.abs(dx) > 2 * Math.abs(dy)) go(dx < 0 ? 1 : -1);
  }, { passive: true });

  document.addEventListener('keydown', onKey);
  document.body.classList.add('no-scroll');
  document.body.append(view);
  show();
  view.focus({ preventScroll: true });
  return close;
}
