// Small DOM and string helpers shared by all views.
import { openLayer } from './layers.js';

/** h('div.card', {onclick}, child, 'text') -> HTMLElement */
/** An empty state: one short sentence and, next to it, the button that fixes it (never a bare sentence). */
export function emptyBox(text, ...actions) {
  const acts = actions.filter(Boolean);
  return h('div.empty', {}, h('p', {}, text), acts.length ? h('div.empty-actions', {}, ...acts) : null);
}

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
  rankings: '<path d="M3 4h4v4H3zM10 6h11M3 10h4v4H3zM10 12h11M3 16h4v4H3zM10 18h11"/>',
  screenshots: '<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="m21 16-5-5-9 8"/>',
  years: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>',
  table: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M3 15h18M9 4v16"/>',
  views: '<path d="M4 6h16M4 12h16M4 18h10"/><path d="m18 16 2 2 2-2" transform="translate(-2 -1)"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  attention: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 16.5v.5"/>',
  more: '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/>',
  similar: '<rect x="3" y="4" width="8" height="11" rx="1.5"/><rect x="13" y="9" width="8" height="11" rx="1.5"/><path d="M7 18v2M17 4v2"/>',
  deals: '<path d="M3 12.5V4h8.5L21 13.5 13.5 21z"/><circle cx="7.5" cy="8.5" r="1.3"/>',
  hidden: '<path d="M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6z"/><circle cx="12" cy="12" r="2.5"/><path d="M4 20 20 4"/>',
  upcoming: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4M12 13v5M9.5 15.5h5"/>',
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

/** For inputs that search game titles: a phone must not "correct" Celeste or Hades. */
export const NO_AUTOCORRECT = { spellcheck: 'false', autocapitalize: 'off', autocorrect: 'off' };

/** "1 game", "3 games". `many` defaults to `one` + "s". */
export const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

const ERROR_TOAST = /\b(could not|couldn't|can't|cannot|failed|went wrong|did not|didn't|not saved|not sent)\b/i;
/** How long a toast stays: a message that reports a failure never less than 5 s, so it can be read (and acted on). */
export const toastMs = (msg, ms) => (ERROR_TOAST.test(String(msg)) ? Math.max(ms ?? 0, 5000) : ms ?? 2600);

/** Show `t` in the toast box for `ms`; hovering or focusing it holds it, and it then gets a short fresh time to be read. */
function showToast(t, ms) {
  let timer = 0;
  const start = (n) => { clearTimeout(timer); timer = setTimeout(() => t.remove(), n); };
  const hold = () => clearTimeout(timer);
  t.addEventListener('mouseenter', hold);
  t.addEventListener('focusin', hold);
  t.addEventListener('mouseleave', () => start(Math.min(ms, 3000)));
  t.addEventListener('focusout', () => start(Math.min(ms, 3000)));
  document.getElementById('toasts').append(t);
  start(ms);
}

export function toast(msg, ms) {
  showToast(h('div.toast', {}, msg), toastMs(msg, ms));
}

/** A toast with one action button (for example Undo). onAction runs when it is tapped; the toast then closes. */
export function toastAction(msg, label, onAction, ms = 6000) {
  const t = h('div.toast', {}, msg, ' ', h('button.toast-action', { type: 'button', onclick: () => { t.remove(); onAction(); } }, label));
  showToast(t, toastMs(msg, ms));
  return t;
}

/** Modal asking for a name (and an emoji icon). Resolves to {name, icon} or null when cancelled. */
export function askNameDialog({ title, name = '', icon: ic = '⭐', confirm = 'Save' }) {
  return new Promise((resolve) => {
    const nameIn = h('input', { type: 'text', value: name, maxlength: '60', placeholder: 'e.g. Indie RPGs to play', 'aria-label': 'Name', required: true });
    const iconIn = h('input.icon-in', { type: 'text', value: ic, maxlength: '4', 'aria-label': 'Icon (emoji)' });
    const done = (v) => { scrim.remove(); box.remove(); document.removeEventListener('keydown', onKey); release(); resolve(v); };
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
    const release = openLayer(box, { requestClose: () => done(null) });
    nameIn.focus(); nameIn.select();
  });
}

/**
 * Modal asking for a text (one line, or several with `multiline`). Resolves to the trimmed text, or null when cancelled.
 * `allowEmpty` lets an empty text through (to clear a description). `extra` is an optional element shown under the field.
 */
export function promptDialog({ title, value = '', placeholder = '', confirm = 'Save', maxlength = 80, multiline = false, allowEmpty = false, extra = null }) {
  return new Promise((resolve) => {
    const input = multiline
      ? h('textarea', { maxlength: String(maxlength), placeholder, rows: '3', 'aria-label': title })
      : h('input', { type: 'text', maxlength: String(maxlength), placeholder, 'aria-label': title });
    input.value = value;
    input.classList.add('prompt-in');
    const done = (v) => { scrim.remove(); box.remove(); document.removeEventListener('keydown', onKey); release(); resolve(v); };
    const submit = () => { const t = input.value.trim(); if (t || allowEmpty) done(t); };
    const form = h('form', { onsubmit: (e) => { e.preventDefault(); submit(); } },
      h('h2', {}, title), input, extra,
      h('div.dialog-actions', {}, h('button.btn', { type: 'button', onclick: () => done(null) }, 'Cancel'), h('button.btn.primary', { type: 'submit' }, confirm)));
    const scrim = h('div.scrim.show', { onclick: () => done(null) });
    scrim.style.zIndex = 95;
    const box = h('div.dialog', { role: 'dialog', 'aria-modal': 'true', 'aria-label': title }, form);
    const onKey = (e) => { if (e.key === 'Escape') done(null); };
    document.addEventListener('keydown', onKey);
    document.body.append(scrim, box);
    const release = openLayer(box, { requestClose: () => done(null) });
    input.focus(); input.select();
  });
}

/** Yes/no modal. Resolves true when confirmed. */
export function confirmDialog({ title, message = '', confirm = 'Delete', danger = true }) {
  return new Promise((resolve) => {
    const done = (v) => { scrim.remove(); box.remove(); document.removeEventListener('keydown', onKey); release(); resolve(v); };
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
    const release = openLayer(box, { requestClose: () => done(false) });
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

/** Point a (lazy) cover <img> at a new URL; a picture not requested yet just gets its new src. Returns true when it changed. */
export function retargetImg(img, next) {
  if (!next || img.dataset.src === next) return false;
  img.dataset.src = next;
  if (img.hasAttribute('src')) img.src = next;
  return true;
}

/** An <img> whose picture is requested only once it is near the viewport. */
export function lazyImg(src, attrs = {}) {
  const img = h('img', { alt: '', decoding: 'async', ...attrs });
  if (coverObserver) { img.dataset.src = src; coverObserver.observe(img); } else img.src = src;
  return img;
}
