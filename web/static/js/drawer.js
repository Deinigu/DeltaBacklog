// Game detail drawer (read-only for now; editing arrives with the table view).
import { h, gradientFor, initials } from './util.js';
import { store, coverURL } from './api.js';

let open = null;

export function closeDrawer() {
  if (!open) return;
  open.drawer.remove();
  open.scrim.remove();
  document.removeEventListener('keydown', open.onKey);
  open = null;
}

export function openDrawer(game) {
  closeDrawer();
  const st = store.statusByCode.get(game.status);
  const url = coverURL(game);

  const fact = (label, value) => (value == null || value === '' || (Array.isArray(value) && !value.length)) ? null : [
    h('dt', {}, label),
    h('dd', {}, Array.isArray(value) ? h('div.tags', {}, value.map((v) => h('span.tag', {}, v))) : String(value)),
  ];
  const years = [...game.years.map(String), ...game.periods.map((p) => p[0].toUpperCase() + p.slice(1))];

  const scrim = h('div.scrim.show', { onclick: closeDrawer });
  scrim.style.zIndex = 55;
  const drawer = h('div.drawer', { role: 'dialog', 'aria-modal': 'true', 'aria-label': game.title },
    h('button.close', { onclick: closeDrawer, 'aria-label': 'Close' }, '✕'),
    h('div.cover', {},
      url ? h('img', { src: url, alt: '' }) : h('div.ph', { style: `background:${gradientFor(game.title)}` }, initials(game.title)),
      st && h('span.st', { style: `background:${st.color}` }, st.label)),
    h('div.drawer-body', {},
      h('h2', {}, game.title, game.favorite ? ' ★' : ''),
      h('dl.facts', {},
        fact('Platforms', game.platforms),
        fact('Genres', game.genres),
        fact('Themes', game.themes),
        fact('Series', game.series),
        fact('Played in', years),
        fact('Played with', game.players),
        fact('Ownership', game.ownership),
        fact('Rating', game.rating && `${game.rating}/10`),
        fact('VR', game.vr ? 'Yes' : null),
        fact('100% completed', game.completed ? 'Yes' : null)),
      game.notes && h('div.notes', {}, game.notes)));

  const onKey = (e) => { if (e.key === 'Escape') closeDrawer(); };
  document.addEventListener('keydown', onKey);
  document.body.append(scrim, drawer);
  open = { drawer, scrim, onKey };
}
