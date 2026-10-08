// PNG export of a tier list, drawn on a canvas (no library). Covers are same-origin, so the canvas stays clean.
import { coverURL } from './api.js';
import { gradientFor, initials } from './util.js';

const TILE = 96, GAP = 6, LABEL = 84, PAD = 16, HEAD = 56;

function loadImage(src) {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = src;
  });
}

/** Draw `state` (see yearreview.tierState) for `year` and download it as deltabacklog-tierlist-<year>.png. */
export async function exportTierlistPNG({ year, state, byId }) {
  const perRow = 8;
  const rowsOf = (n) => Math.max(1, Math.ceil(n / perRow));
  const heights = state.tiers.map((t) => rowsOf(state.rows[t.id].length) * (TILE + GAP) + GAP);
  const width = PAD * 2 + LABEL + perRow * (TILE + GAP) + GAP;
  const height = HEAD + heights.reduce((a, b) => a + b, 0) + PAD;

  const ids = state.tiers.flatMap((t) => state.rows[t.id]);
  const images = new Map(await Promise.all(ids.map(async (id) => {
    const url = coverURL(byId.get(id));
    return [id, url ? await loadImage(url) : null];
  })));

  const canvas = document.createElement('canvas');
  canvas.width = width; canvas.height = height;
  const c = canvas.getContext('2d');
  c.fillStyle = '#14161a'; c.fillRect(0, 0, width, height);
  c.fillStyle = '#eceef2'; c.font = '700 26px system-ui, sans-serif'; c.textBaseline = 'middle';
  c.fillText(`Tier list ${year}`, PAD, HEAD / 2 + 4);

  let y = HEAD;
  state.tiers.forEach((t, i) => {
    c.fillStyle = '#232730'; c.fillRect(PAD, y, width - PAD * 2, heights[i] - 2);
    c.fillStyle = t.color; c.fillRect(PAD, y, LABEL, heights[i] - 2);
    c.fillStyle = '#14161a'; c.font = '700 40px system-ui, sans-serif'; c.textAlign = 'center';
    c.fillText(t.name, PAD + LABEL / 2, y + (heights[i] - 2) / 2);
    c.textAlign = 'left';
    state.rows[t.id].forEach((id, k) => {
      const x = PAD + LABEL + GAP + (k % perRow) * (TILE + GAP);
      const ty = y + GAP + Math.floor(k / perRow) * (TILE + GAP);
      const g = byId.get(id), img = images.get(id);
      c.save();
      c.beginPath(); c.rect(x, ty, TILE, TILE); c.clip();
      if (img) {
        const s = Math.max(TILE / img.width, TILE / img.height);
        c.drawImage(img, x + (TILE - img.width * s) / 2, ty + (TILE - img.height * s) / 2, img.width * s, img.height * s);
      } else {
        c.fillStyle = gradientFor(g.title).match(/hsl\([^)]*\)/)?.[0] || '#5558e6';
        c.fillRect(x, ty, TILE, TILE);
        c.fillStyle = '#fff'; c.font = '700 28px system-ui, sans-serif'; c.textAlign = 'center';
        c.fillText(initials(g.title), x + TILE / 2, ty + TILE / 2);
        c.textAlign = 'left';
      }
      c.restore();
    });
    y += heights[i];
  });

  const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/png'));
  if (!blob) throw new Error('no image');
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `deltabacklog-tierlist-${year}.png`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 10000);
}
