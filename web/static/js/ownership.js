// One emoji per ownership value, shared by chips, cards, the table, the drawer and the add form. Pure (no DOM).

const ICONS = {
  'owned': '✅',
  'pirated': '🏴‍☠️',
  'free to play': '🆓',
  'family share': '👨‍👩‍👧',
  'borrowed': '🤝',
  'nope': '🚫',
};
export const FALLBACK_ICON = '🏷️'; // any ownership value that has no emoji yet

const norm = (s) => String(s ?? '').trim().toLowerCase().replace(/\s+/g, ' ');

/** The emoji for an ownership value ('' when the game has none). */
export function ownershipIcon(name) {
  if (!norm(name)) return '';
  return ICONS[norm(name)] ?? FALLBACK_ICON;
}

/** "✅ Owned": the emoji and the name, for chips, options and labels. */
export function ownershipLabel(name) {
  const icon = ownershipIcon(name);
  return icon ? `${icon} ${name}` : String(name ?? '');
}

/** Plain "Owned" is the normal case; only the unusual values earn a badge on a card. */
export const isUsualOwnership = (name) => norm(name) === 'owned';
