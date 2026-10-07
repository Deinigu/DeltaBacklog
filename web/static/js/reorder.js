// Touch-friendly drag list for manual ordering (order maths lives in order.js).
import { h, gradientFor, initials, lazyImg } from './util.js';
import { store, coverURL } from './api.js';
import { moveItem } from './order.js';

/**
 * A reorderable list of games. `onCommit(ids)` is called with the new order after every move.
 * Drag with the handle (works with touch: the handle disables scrolling), or use the arrow buttons.
 */
export function createReorderList(games, onCommit, onOpen) {
  const list = h('div.reorder');
  let order = games.map((g) => g.id);
  const byId = new Map(games.map((g) => [g.id, g]));

  function row(g, i) {
    const st = store.statusByCode.get(g.status);
    const url = coverURL(g);
    const handle = h('button.handle', { type: 'button', 'aria-label': `Drag to move ${g.title}` }, '⠿');
    const el = h('div.rrow', { 'data-id': g.id },
      handle,
      h('div.thumb', {}, url ? lazyImg(url) : h('div.ph', { style: `background:${gradientFor(g.title)}` }, initials(g.title))),
      h('button.rtitle', { type: 'button', onclick: () => onOpen && onOpen(g) }, h('span', {}, g.title), st && h('small', { style: `color:${st.color}` }, st.label)),
      h('div.rbtns', {},
        h('button.btn', { type: 'button', 'aria-label': 'Move up', onclick: () => step(g.id, -1) }, '▲'),
        h('button.btn', { type: 'button', 'aria-label': 'Move down', onclick: () => step(g.id, 1) }, '▼')));
    handle.addEventListener('pointerdown', (e) => startDrag(e, el));
    return el;
  }

  function draw() {
    list.replaceChildren(...order.map((id, i) => row(byId.get(id), i)));
    syncButtons();
  }
  function syncButtons() {
    [...list.children].forEach((el, i, all) => {
      const [up, down] = el.querySelectorAll('.rbtns .btn');
      up.disabled = i === 0;
      down.disabled = i === all.length - 1;
    });
  }
  function domOrder() { return [...list.children].map((el) => Number(el.dataset.id)); }
  function commit(next) {
    if (next.length === order.length && next.every((id, i) => id === order[i])) return;
    order = next;
    onCommit(next.slice());
  }

  function step(id, d) {
    const i = order.indexOf(id);
    const next = moveItem(order, i, i + d);
    if (next === order) return;
    commit(next);
    draw();
    const moved = list.querySelector(`[data-id="${id}"]`);
    moved?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    moved?.querySelectorAll('.rbtns .btn')[d < 0 ? 0 : 1].focus({ preventScroll: true });
  }

  function startDrag(e, el) {
    if (e.button > 0) return;
    e.preventDefault();
    const handle = e.currentTarget;
    handle.setPointerCapture(e.pointerId);
    el.classList.add('dragging');
    let startY = e.clientY, lastY = e.clientY;
    const mid = (n) => { const r = n.getBoundingClientRect(); return r.top + r.height / 2; };

    const scroller = setInterval(() => {
      if (lastY < 90) window.scrollBy(0, -14);
      else if (lastY > innerHeight - 90) window.scrollBy(0, 14);
      else return;
      place();
    }, 16);

    function place() {
      el.style.transform = `translateY(${lastY - startY}px)`;
      for (;;) {
        const prev = el.previousElementSibling, next = el.nextElementSibling;
        const c = mid(el);
        if (prev && c < mid(prev)) {
          const hgt = prev.getBoundingClientRect().height;
          list.insertBefore(el, prev);
          startY -= hgt;
        } else if (next && c > mid(next)) {
          const hgt = next.getBoundingClientRect().height;
          list.insertBefore(next, el);
          startY += hgt;
        } else break;
        el.style.transform = `translateY(${lastY - startY}px)`;
      }
    }
    const move = (ev) => { lastY = ev.clientY; place(); };
    const end = () => {
      clearInterval(scroller);
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      el.classList.remove('dragging');
      el.style.transform = '';
      syncButtons();
      commit(domOrder());
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  }

  draw();
  return list;
}
