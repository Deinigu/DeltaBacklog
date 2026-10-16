// The full-screen screenshot viewer, shared by the game drawer (shotsview.js) and the Screenshots page (gallerypage.js).
// It owns the frame: bar, picture, previous/next, swipe, keys, focus trap, the date/size line and the download button.
// The callers add what differs: bar buttons, the foot (caption, move, game name...) and how to step.
import { h } from './util.js';
import { formatBytes, formatTaken, stepIndex } from './shots.js';

const DOWNLOAD_ICON = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3v12m0 0l-4.5-4.5M12 15l4.5-4.5M4 20h16"/></svg>';

/**
 * Open the viewer at `start`. Options:
 *  count()               how many pictures there are (shown as "n / count"; prev/next hide below 2)
 *  alt(s, i)             optional, the picture's alt text (default: its caption, else "Screenshot n")
 *  get(i)                the picture {src, width, height, bytes, date | taken_at, caption} at i, or undefined (the viewer closes)
 *  step(cur, dir)        optional, resolves the next index (default: wrap around count())
 *  beforeStep()          optional, awaited before a step and before closing (the drawer saves the caption)
 *  barActions, barEnd    elements in the bar before / after the download button
 *  foot(meta)            returns the children of the foot; `meta` is the date/size line, put it where it belongs
 *  onShow(s, i)          after the picture at i is shown (update your foot)
 *  ignoreKey(e)          true to leave a key alone (typing in the caption box)
 *  onClose(cur)          after the viewer is gone
 * Returns { close(), show(i?) (redraw, optionally at another index), index() }.
 */
export function openShotViewer(opts, start) {
  let cur = start;
  let busy = false;
  let closed = false;
  const img = h('img.shot-img', { alt: '' });
  const counter = h('span.shot-counter');
  const meta = h('p.shot-meta');
  const dl = h('a.btn', { 'aria-label': 'Download the original', title: 'Download the original', download: '' });
  dl.innerHTML = DOWNLOAD_ICON;
  const prev = h('button.btn.shot-nav.prev', { type: 'button', 'aria-label': 'Previous screenshot', onclick: () => go(-1) }, '‹');
  const next = h('button.btn.shot-nav.next', { type: 'button', 'aria-label': 'Next screenshot', onclick: () => go(1) }, '›');
  const stage = h('div.shot-stage', {}, prev, img, next);
  const view = h('div.shot-viewer', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Screenshot viewer', tabindex: '-1' },
    h('div.shot-bar', {}, h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕'), counter, h('span.spacer'),
      opts.barActions, dl, opts.barEnd),
    stage,
    h('div.shot-foot', {}, opts.foot(meta)));

  function show(i) {
    if (i !== undefined) cur = i;
    const s = opts.get(cur);
    if (!s) { close(); return; }
    const n = opts.count();
    img.src = s.src;
    img.alt = opts.alt ? opts.alt(s, cur) : s.caption || `Screenshot ${cur + 1}`;
    counter.textContent = `${cur + 1} / ${n}`;
    meta.textContent = [formatTaken(s.date || s.taken_at), `${s.width}×${s.height}`, formatBytes(s.bytes)].filter(Boolean).join(' · ');
    dl.href = `${s.src}?download=1`;
    prev.hidden = next.hidden = n < 2;
    opts.onShow?.(s, cur);
  }

  async function go(dir) {
    if (busy) return;
    busy = true;
    try {
      await opts.beforeStep?.();
      cur = opts.step ? await opts.step(cur, dir) : stepIndex(cur, opts.count(), dir);
    } finally { busy = false; }
    if (!closed) show();
  }

  async function close() {
    if (closed) return;
    await opts.beforeStep?.();
    if (closed) return;
    closed = true;
    document.removeEventListener('keydown', onKey);
    view.remove();
    document.body.classList.remove('no-scroll');
    opts.onClose?.(cur);
  }

  const focusables = () => [...view.querySelectorAll('button, a[href], input')].filter((el) => !el.disabled && !el.hidden && el.offsetParent !== null);
  function onKey(e) {
    if (e.key === 'Tab') { // keep Tab inside the viewer: everything behind it is out of reach
      const list = focusables();
      if (!list.length) { e.preventDefault(); return; }
      const first = list[0];
      const last = list[list.length - 1];
      if (e.shiftKey && (document.activeElement === first || document.activeElement === view)) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      else if (!view.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
      return;
    }
    if (opts.ignoreKey?.(e) && e.key !== 'Escape') return;
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
  return { close, show, index: () => cur };
}
