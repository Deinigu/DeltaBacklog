// Pointer-event dragging of ranking tiles: a mouse drags at once, a finger after a long press (a quick swipe still
// scrolls the page). The tile is dropped on a `.rk-cell` (data-tier = tier id, "" for the pool). Taps are left to the
// click handler of the tile.
import { dropIndex } from './rankboard.js';

const PRESS_MS = 380;

/**
 * attachDrag(tile, { onDrop(tierId | null, index) }): `index` is the position among the cell's other tiles.
 * `cellSelector` finds the drop targets; the tile must carry the `.rk-tile` class.
 */
export function attachDrag(el, { onDrop, cellSelector = '.rk-cell' }) {
  el.addEventListener('pointerdown', (e) => {
    if (e.button > 0) return;
    const sx = e.clientX, sy = e.clientY, touch = e.pointerType !== 'mouse';
    let dragging = false, ghost = null, hot = null, pressTimer = null, px = 0, py = 0, raf = 0;
    const onTouchMove = (ev) => { if (dragging) ev.preventDefault(); };

    const start = () => {
      dragging = true;
      clearTimeout(pressTimer);
      el.classList.add('dragging');
      ghost = el.cloneNode(true);
      ghost.classList.add('ghost');
      ghost.removeAttribute('id');
      ghost.style.width = `${el.offsetWidth}px`;
      ghost.style.height = `${el.offsetHeight}px`;
      document.body.append(ghost);
      navigator.vibrate?.(15);
    };
    // Near the top or bottom edge the page scrolls by itself, so a far-away tier can be reached mid-drag.
    const autoScroll = () => {
      raf = 0;
      if (!dragging) return;
      const edge = 80, dy = py < edge ? -(edge - py) / 3 : py > innerHeight - edge ? (py - (innerHeight - edge)) / 3 : 0;
      if (dy) { window.scrollBy(0, dy); move(px, py); }
    };
    const move = (x, y) => {
      px = x; py = y;
      if (!raf) raf = requestAnimationFrame(autoScroll);
      ghost.style.transform = `translate(${x - el.offsetWidth / 2}px, ${y - el.offsetHeight / 2}px)`;
      const cell = document.elementFromPoint(x, y)?.closest(cellSelector);
      if (cell !== hot) { hot?.classList.remove('hot'); hot = cell; hot?.classList.add('hot'); }
    };
    const finish = (ev, cancelled) => {
      clearTimeout(pressTimer);
      cancelAnimationFrame(raf);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onCancel);
      document.removeEventListener('touchmove', onTouchMove);
      if (!dragging) return; // a plain tap is the click listener's business
      ghost.remove();
      el.classList.remove('dragging');
      hot?.classList.remove('hot');
      // The pointer is usually released over the tile itself: swallow the click that follows.
      const block = (c) => c.stopImmediatePropagation();
      el.addEventListener('click', block, { once: true, capture: true });
      setTimeout(() => el.removeEventListener('click', block, true), 100);
      if (cancelled || !hot) return;
      const tierId = hot.dataset.tier === '' ? null : Number(hot.dataset.tier);
      const rects = [...hot.querySelectorAll('.rk-tile')].filter((t) => t !== el).map((t) => t.getBoundingClientRect());
      onDrop(tierId, dropIndex(rects, ev.clientX, ev.clientY));
    };
    const onMove = (ev) => {
      if (!dragging) {
        if (Math.hypot(ev.clientX - sx, ev.clientY - sy) <= (touch ? 10 : 5)) return;
        if (touch) { finish(null, true); return; } // the finger scrolls the page instead
        start();
      }
      ev.preventDefault();
      move(ev.clientX, ev.clientY);
    };
    const onUp = (ev) => finish(ev, false);
    const onCancel = (ev) => finish(ev, true);
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onCancel);
    if (touch) {
      document.addEventListener('touchmove', onTouchMove, { passive: false });
      pressTimer = setTimeout(() => { start(); move(sx, sy); }, PRESS_MS);
    }
  });
  el.addEventListener('contextmenu', (e) => { if (e.pointerType === 'touch' || el.classList.contains('dragging')) e.preventDefault(); });
}
