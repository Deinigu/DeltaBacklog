// Readable text on a coloured badge (WCAG contrast). Pure (no DOM): unit-tested.

const channel = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };

/** Relative luminance of "#rrggbb". */
export function luminance(hex) {
  const m = /^#?([0-9a-f]{6})$/i.exec(String(hex).trim());
  if (!m) return 0;
  const n = parseInt(m[1], 16);
  return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255);
}

/** WCAG contrast ratio between two colours, 1 to 21. */
export function contrast(a, b) {
  const la = luminance(a), lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** "#111111" or "#ffffff", whichever reads better on the background. */
export function readableTextOn(hex) {
  return contrast(hex, '#ffffff') >= contrast(hex, '#111111') ? '#ffffff' : '#111111';
}

/** Inline style for a status badge: the status colour as background, text chosen for contrast. */
export const badgeStyle = (hex) => `background:${hex};color:${readableTextOn(hex)};text-shadow:none`;
