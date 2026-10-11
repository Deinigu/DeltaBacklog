// Small DOM and string helpers shared by all views.

/** h('div.card', {onclick}, child, 'text') -> HTMLElement */
export function h(tag, attrs, ...children) {
  const [name, ...classes] = tag.split('.');
  const node = document.createElement(name || 'div');
  if (classes.length) node.className = classes.join(' ');
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k.startsWith('on')) node.addEventListener(k.slice(2), v);
    else if (k === 'class') node.className += ' ' + v;
    else node.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    node.append(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return node;
}

/** Lowercase, strip diacritics: "Pokémon" matches "pokemon". */
export function norm(s) {
  return (s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
}

export function debounce(fn, ms) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

/** Stable pleasant gradient for games without a cover. */
export function gradientFor(title) {
  let n = 0;
  for (const ch of title) n = (n * 31 + ch.codePointAt(0)) >>> 0;
  const a = n % 360;
  return `linear-gradient(135deg, hsl(${a} 55% 42%), hsl(${(a + 40) % 360} 60% 32%))`;
}

export function initials(title) {
  const words = title.replace(/[^\p{L}\p{N} ]/gu, '').split(/\s+/).filter(Boolean);
  return (words.length > 1 ? words[0][0] + words[1][0] : (words[0] || '?').slice(0, 2)).toUpperCase();
}

const SVG = {
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  filter: '<path d="M4 6h16M7 12h10M10 18h4"/>',
  sort: '<path d="M7 4v16M7 20l-3-3M7 20l3-3M17 20V4M17 4l-3 3M17 4l3 3"/>',
  library: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  shelves: '<path d="M4 20V6M10 20V4M16 20V8M21 20l-3-12"/>',
  years: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>',
  table: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M3 15h18M9 4v16"/>',
  views: '<path d="M4 6h16M4 12h16M4 18h10"/><path d="m18 16 2 2 2-2" transform="translate(-2 -1)"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  more: '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/>',
  stats: '<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>',
  series: '<rect x="3" y="4" width="18" height="4" rx="1.5"/><rect x="3" y="10" width="13" height="4" rx="1.5"/><rect x="3" y="16" width="8" height="4" rx="1.5"/>',
  add: '<path d="M12 5v14M5 12h14"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
};
export function icon(name) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('aria-hidden', 'true');
  s.innerHTML = SVG[name] || '';
  return s;
}

export function toast(msg, ms = 2600) {
  const box = document.getElementById('toasts');
  const t = h('div.toast', {}, msg);
  box.append(t);
  setTimeout(() => t.remove(), ms);
}

/** A toast with one action button (for example Undo). onAction runs when it is tapped; the toast then closes. */
export function toastAction(msg, label, onAction, ms = 6000) {
  const box = document.getElementById('toasts');
  const t = h('div.toast', {}, msg, ' ', h('button.toast-action', { type: 'button', onclick: () => { t.remove(); onAction(); } }, label));
  box.append(t);
  setTimeout(() => t.remove(), ms);
  return t;
}

/** Modal asking for a name (and an emoji icon). Resolves to {name, icon} or null when cancelled. */
export function askNameDialog({ title, name = '', icon: ic = '⭐', confirm = 'Save' }) {
  return new Promise((resolve) => {
    const nameIn = h('input', { type: 'text', value: name, maxlength: '60', placeholder: 'e.g. Indie RPGs to play', 'aria-label': 'Name', required: true });
    const iconIn = h('input.icon-in', { type: 'text', value: ic, maxlength: '4', 'aria-label': 'Icon (emoji)' });
    const done = (v) => { scrim.remove(); box.remove(); document.removeEventListener('keydown', onKey); resolve(v); };
    const form = h('form', { onsubmit: (e) => { e.preventDefault(); if (nameIn.value.trim()) done({ name: nameIn.value.trim(), icon: iconIn.value.trim() }); } },
      h('h2', {}, title),
      h('div.field-row', {}, iconIn, nameIn),
      h('div.dialog-actions', {}, h('button.btn', { type: 'button', onclick: () => done(null) }, 'Cancel'), h('button.btn.primary', { type: 'submit' }, confirm)));
    const scrim = h('div.scrim.show', { onclick: () => done(null) });
    scrim.style.zIndex = 95;
    const box = h('div.dialog', { role: 'dialog', 'aria-modal': 'true', 'aria-label': title }, form);
    const onKey = (e) => { if (e.key === 'Escape') done(null); };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, box);
    nameIn.focus(); nameIn.select();
  });
}

/** Yes/no modal. Resolves true when confirmed. */
export function confirmDialog({ title, message = '', confirm = 'Delete', danger = true }) {
  return new Promise((resolve) => {
    const done = (v) => { scrim.remove(); box.remove(); document.removeEventListener('keydown', onKey); resolve(v); };
    const scrim = h('div.scrim.show', { onclick: () => done(false) });
    scrim.style.zIndex = 95;
    const box = h('div.dialog', { role: 'alertdialog', 'aria-modal': 'true', 'aria-label': title },
      h('h2', {}, title),
      message && h('p', { style: 'margin:0;color:var(--muted)' }, message),
      h('div.dialog-actions', {}, h('button.btn', { type: 'button', onclick: () => done(false) }, 'Cancel'),
        h('button.btn.primary', { type: 'button', class: danger ? 'danger' : '', onclick: () => done(true) }, confirm)));
    const onKey = (e) => { if (e.key === 'Escape') done(false); };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, box);
    box.querySelector('.btn').focus();
  });
}

// Covers load only when they are about to scroll into view. The browser's own loading="lazy" is not enough:
// Chrome fetches every lazy image again on a reload or a repeat visit, which for a 600-game library is about
// 600 requests and 15MB, a lot to spend on a phone. This observer asks for a cover 600px before it is seen.
const coverObserver = typeof IntersectionObserver === 'function'
  ? new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      coverObserver.unobserve(e.target);
      e.target.src = e.target.dataset.src;
    }
  }, { rootMargin: '600px 0px' })
  : null;

/** An <img> whose picture is requested only once it is near the viewport. */
export function lazyImg(src, attrs = {}) {
  const img = h('img', { alt: '', decoding: 'async', ...attrs });
  if (coverObserver) { img.dataset.src = src; coverObserver.observe(img); } else img.src = src;
  return img;
}
