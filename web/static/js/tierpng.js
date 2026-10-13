// PNG export of a ranking, drawn on a canvas (no library) in the TierMaker look: a dark board, a coloured label cell
// per tier with wrapping text, big poster tiles. Posters are same-origin, so the canvas stays clean.
import { posterURL, coverURL } from './api.js';
import { gradientFor, initials } from './util.js';
import { wrapText, exportLayout, tileHeight, textOn } from './rankboard.js';

const TILE_W = 90, GAP = 6, LABEL_W = 150, PAD = 16, HEAD = 64, PER_ROW = 10;
const FONT = 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

function loadImage(src) {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = src;
  });
}

/** Draw a poster, or a coloured placeholder with the game's title, filling the w x h box at (x, y). */
function drawTile(c, g, img, x, y, w, h) {
  c.save();
  c.beginPath(); c.rect(x, y, w, h); c.clip();
  if (img) {
    const s = Math.max(w / img.width, h / img.height);
    c.drawImage(img, x + (w - img.width * s) / 2, y + (h - img.height * s) / 2, img.width * s, img.height * s);
  } else {
    c.fillStyle = gradientFor(g.title).match(/hsl\([^)]*\)/)?.[0] || '#5558e6';
    c.fillRect(x, y, w, h);
    c.fillStyle = '#fff'; c.textAlign = 'center'; c.textBaseline = 'middle';
    c.font = `700 ${Math.round(w * 0.3)}px ${FONT}`;
    c.fillText(initials(g.title), x + w / 2, y + h * 0.36);
    c.font = `600 ${Math.round(w * 0.12)}px ${FONT}`;
    const lines = wrapText((t) => c.measureText(t).width, g.title, w - 8).slice(0, 4);
    lines.forEach((line, i) => c.fillText(line, x + w / 2, y + h * 0.58 + i * w * 0.14));
  }
  c.restore();
}

/**
 * Draw the ranking and download it as <name>.png. `board` is the editor's state ({tiers: [{label, color, games}]}),
 * `byId` maps game ids to games. The pool (games not ranked) is not drawn. Tiles are always full size (90x135).
 */
export async function exportRankingPNG({ name, board, byId }) {
  const tileH = tileHeight(TILE_W);
  const layout = exportLayout({ counts: board.tiers.map((t) => t.games.length), perRow: PER_ROW, tileW: TILE_W, tileH, gap: GAP, labelW: LABEL_W, pad: PAD, headH: HEAD });

  const ids = [...new Set(board.tiers.flatMap((t) => t.games))];
  const images = new Map();
  for (let i = 0; i < ids.length; i += 8) { // a few at a time: a phone should not hold 200 requests at once
    await Promise.all(ids.slice(i, i + 8).map(async (id) => {
      const g = byId.get(id);
      const url = g && (posterURL(g) || coverURL(g));
      images.set(id, url ? await loadImage(url) : null);
    }));
  }

  const canvas = document.createElement('canvas');
  canvas.width = layout.width; canvas.height = layout.height;
  const c = canvas.getContext('2d');
  c.fillStyle = '#14161a'; c.fillRect(0, 0, layout.width, layout.height);
  c.fillStyle = '#eceef2'; c.font = `700 28px ${FONT}`; c.textBaseline = 'middle'; c.textAlign = 'left';
  c.fillText(name, PAD, HEAD / 2 + 2, layout.width - PAD * 2);

  board.tiers.forEach((t, i) => {
    const row = layout.rows[i];
    c.fillStyle = '#232730'; c.fillRect(PAD, row.y, layout.width - PAD * 2, row.h);
    c.fillStyle = t.color; c.fillRect(PAD, row.y, LABEL_W, row.h);
    // The label wraps and shrinks until it fits the cell.
    let size = 22, lines;
    for (; size >= 11; size -= 1) {
      c.font = `700 ${size}px ${FONT}`;
      lines = wrapText((s) => c.measureText(s).width, t.label, LABEL_W - 20);
      if (lines.length * size * 1.2 <= row.h - 12) break;
    }
    c.fillStyle = textOn(t.color); c.textAlign = 'center'; c.textBaseline = 'middle';
    lines.forEach((line, k) => c.fillText(line, PAD + LABEL_W / 2, row.y + row.h / 2 + (k - (lines.length - 1) / 2) * size * 1.2));
    c.textAlign = 'left';
    t.games.forEach((id, k) => {
      const { x, y } = row.tile(k);
      drawTile(c, byId.get(id) || { title: '?' }, images.get(id), x, y, TILE_W, tileH);
    });
  });

  const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/png'));
  if (!blob) throw new Error('no image');
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${(name || 'ranking').replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-|-$/g, '').toLowerCase() || 'ranking'}.png`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 10000);
}
