// Shrinks a picked photo in the browser before it is uploaded, so a 12-48 megapixel phone photo never reaches the
// Pi's decoder: a cover is at most COVER_WIDTH wide, a poster is the centre 2:3 part at most POSTER_WIDTH wide. The
// browser also turns the picture upright by its EXIF orientation (imageOrientation: 'from-image').
export const COVER_WIDTH = 640; // the server stores 480; this leaves headroom
export const POSTER_WIDTH = 600; // the server stores 300

/** Where to read from the picture and how big to draw it: {sx, sy, sw, sh, dw, dh}. Pure, so it can be tested. */
export function planPrep(w, h, kind) {
  let sx = 0, sy = 0, sw = w, sh = h;
  let max = COVER_WIDTH;
  if (kind === 'poster') {
    max = POSTER_WIDTH;
    if (w * 3 > h * 2) { sw = Math.max(1, Math.floor((h * 2) / 3)); sx = Math.floor((w - sw) / 2); } // wider than 2:3: cut the sides
    else { sh = Math.max(1, Math.min(h, Math.floor((w * 3) / 2))); sy = Math.floor((h - sh) / 2); } // taller: cut top and bottom
  }
  const dw = Math.max(1, Math.min(sw, max));
  const dh = Math.max(1, Math.round((sh * dw) / sw));
  return { sx, sy, sw, sh, dw, dh };
}

/**
 * The file as a smaller upright JPEG blob. When the browser cannot read the picture (or has no createImageBitmap) the
 * original file comes back and the server decides.
 */
export async function prepareImage(file, kind) {
  if (typeof createImageBitmap !== 'function' || typeof document === 'undefined') return file;
  let bmp;
  try { bmp = await createImageBitmap(file, { imageOrientation: 'from-image' }); } catch { return file; }
  try {
    const p = planPrep(bmp.width, bmp.height, kind);
    const canvas = document.createElement('canvas');
    canvas.width = p.dw; canvas.height = p.dh;
    const ctx = canvas.getContext('2d');
    if (!ctx) return file;
    ctx.fillStyle = '#000'; ctx.fillRect(0, 0, p.dw, p.dh); // JPEG has no alpha: transparent pixels go on black, as on the server
    ctx.drawImage(bmp, p.sx, p.sy, p.sw, p.sh, 0, 0, p.dw, p.dh);
    const blob = await new Promise((res) => canvas.toBlob(res, 'image/jpeg', 0.88));
    canvas.width = canvas.height = 0; // release the pixels now
    return blob || file;
  } catch { return file; } finally { bmp.close?.(); }
}
