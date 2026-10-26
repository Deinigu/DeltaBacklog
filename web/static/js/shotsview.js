// The "Screenshots" section of the game drawer: thumbnail grid, upload with progress, and the full-screen viewer.
import { h, toast, toastAction } from './util.js';
import { listShots, uploadShot, setShotCaption, deleteShot, deleteShotKeepalive, orderShots } from './api.js';
import { openShotViewer } from './shotviewer.js';
import { moveShot, countLabel, pickImages, uploadSummary, thumbPlan, BIG_UPLOAD, pendingIds, withoutPending, clampIndex } from './shots.js';

const UNDO_MS = 6000;
const STORAGE_DOWN = 'Screenshot storage is not available (is the drive connected?). Uploads will fail until it is back.';

// Screenshots deleted in the UI whose DELETE is still waiting out the undo time, by id. Module level on purpose: a
// drawer that is closed and reopened (a new section) must still hide them, and leaving the page must still delete them.
const pending = new Map(); // id -> { s, gameId, at, timer, toast }
// The service worker's cache of thumbnails (sw.js THUMBS): its name carries the app's base path when that is not '/'.
const APP_BASE = new URL('../', import.meta.url).pathname;
const THUMB_CACHE = `${APP_BASE === '/' ? '' : APP_BASE}screenshot-thumbs`;

/** Tell the other pages (the Screenshots page) that the pictures of some game changed on the server. */
const shotsChanged = () => window.dispatchEvent(new Event('screenshots-changed'));

/** Drop a deleted picture's thumbnail from the service worker's cache so it cannot show as a ghost. */
function forgetThumb(url) {
  try { caches?.open(THUMB_CACHE).then((c) => c.delete(url)).catch(() => {}); } catch { /* no Cache API */ }
}

function flushPending() {
  for (const [id, e] of pending) {
    clearTimeout(e.timer);
    e.toast?.remove(); // the delete is sent now: an Undo button would do nothing
    deleteShotKeepalive(id);
    forgetThumb(e.s.thumb);
  }
  pending.clear();
}
document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') flushPending(); });
window.addEventListener('pagehide', flushPending);

/**
 * A small JPEG of the picture for the server, which cannot decode it (it needs too much memory). It uses the server's
 * own crop rule (thumbPlan) on the size the server reported, and lets the browser crop and shrink while decoding
 * (createImageBitmap with a source rectangle and a target size), so no big bitmap is made here either. null when the
 * browser cannot.
 */
async function makeThumb(file, width, height) {
  const p = thumbPlan(width, height);
  if (typeof createImageBitmap !== 'function' || !p.tw) return null;
  let bmp;
  try {
    const sx = Math.floor((width - p.cw) / 2);
    const sy = Math.floor((height - p.ch) / 2);
    bmp = await createImageBitmap(file, sx, sy, p.cw, p.ch, { resizeWidth: p.tw, resizeHeight: p.th, resizeQuality: 'medium' });
    const canvas = document.createElement('canvas');
    canvas.width = p.tw;
    canvas.height = p.th;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#000';
    ctx.fillRect(0, 0, p.tw, p.th);
    ctx.drawImage(bmp, 0, 0, p.tw, p.th);
    return await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.8));
  } catch {
    return null;
  } finally {
    bmp?.close?.();
  }
}

/** The size of a picture as shown (EXIF applied), read from its header by the browser; null when it cannot. */
function imageSize(file) {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    const done = (v) => { URL.revokeObjectURL(url); img.src = ''; resolve(v); };
    img.onload = () => done(img.naturalWidth > 0 ? { width: img.naturalWidth, height: img.naturalHeight } : null);
    img.onerror = () => done(null);
    img.src = url;
  });
}


/** Build the section for one game; it loads its own list. */
export function screenshotsSection(game) {
  let shots = [];
  let available = true;
  let busy = 0;

  const title = h('h3', {}, countLabel(0));
  const input = h('input', { type: 'file', accept: 'image/*', multiple: true, hidden: true, 'aria-label': 'Add screenshots' });
  const addBtn = h('button.btn', { type: 'button', onclick: () => input.click() }, '＋ Add screenshots');
  const notice = h('p.shots-notice', { role: 'status', hidden: true });
  const progress = h('div.shots-up');
  const grid = h('div.shots-grid');
  const empty = h('p.hint', { hidden: true }, 'No screenshots yet. Add some from your phone or computer, or drop image files here.');
  const root = h('section.shots', { 'aria-label': 'Screenshots' },
    h('div.shots-head', {}, title, addBtn, input), notice, progress, empty, grid);

  function draw() {
    title.textContent = countLabel(shots.length);
    empty.hidden = shots.length > 0 || !available;
    notice.hidden = available;
    notice.textContent = STORAGE_DOWN;
    grid.replaceChildren(...shots.map((s, i) => {
      const img = h('img', { src: s.thumb, alt: s.caption || `Screenshot ${i + 1}`, loading: 'lazy', decoding: 'async', width: '240', height: '135' });
      const b = h('button.shot-thumb', { type: 'button', 'aria-label': `Open screenshot ${i + 1} of ${shots.length}`, onclick: () => openViewer(i) }, img);
      img.addEventListener('error', () => b.classList.add('missing'));
      return b;
    }));
  }

  async function load() {
    try {
      const r = await listShots(game.id);
      shots = withoutPending(r.screenshots, pending);
      available = r.available;
    } catch {
      notice.hidden = false;
      notice.textContent = 'Could not load the screenshots.';
      return;
    }
    draw();
  }

  async function upload(files) {
    const { ok, skipped } = pickImages(files);
    if (skipped.length) toast(`Skipped ${skipped.length} file${skipped.length === 1 ? '' : 's'} that ${skipped.length === 1 ? 'is' : 'are'} not JPEG, PNG or WebP`);
    let added = 0;
    let failed = 0;
    busy++;
    addBtn.disabled = true;
    for (const f of ok) {
      const bar = h('progress', { max: '1', value: '0', 'aria-label': `Uploading ${f.name}` });
      const row = h('div.shots-row', {}, h('span.name', {}, f.name), bar);
      progress.append(row);
      try {
        const onProgress = (p) => { bar.value = p; };
        let s;
        // A big photo is very likely too much for the server to decode: make the preview first, so it goes up once.
        let thumb = null;
        if (f.size > BIG_UPLOAD) {
          const size = await imageSize(f);
          if (size) thumb = await makeThumb(f, size.width, size.height);
        }
        try {
          s = await uploadShot(game.id, f, onProgress, thumb);
        } catch (e) {
          if (!e.needsThumb || thumb) throw e;
          // too big for the server to decode after all: it keeps the original untouched if we send a preview made here
          thumb = await makeThumb(f, e.width, e.height);
          if (!thumb) throw new Error('too big to make a preview, and this browser could not make one either');
          s = await uploadShot(game.id, f, onProgress, thumb);
        }
        shots.push(s);
        added++;
        shotsChanged();
        row.remove();
        draw();
      } catch (e) {
        failed++;
        if (/storage not available/.test(e.message)) { available = false; draw(); }
        row.replaceChildren(h('span.name', {}, f.name), h('span.err', {}, e.message),
          h('button.pill', { type: 'button', 'aria-label': `Dismiss ${f.name}`, onclick: () => row.remove() }, '✕'));
      }
    }
    busy--;
    if (!busy) addBtn.disabled = false;
    if (ok.length) toast(uploadSummary(added, failed));
  }

  input.addEventListener('change', () => { const files = [...input.files]; input.value = ''; upload(files); });
  root.addEventListener('dragover', (e) => { if (e.dataTransfer?.types?.includes('Files')) { e.preventDefault(); root.classList.add('drop'); } });
  root.addEventListener('dragleave', (e) => { if (!root.contains(e.relatedTarget)) root.classList.remove('drop'); });
  root.addEventListener('drop', (e) => {
    if (!e.dataTransfer?.files?.length) return;
    e.preventDefault();
    root.classList.remove('drop');
    upload([...e.dataTransfer.files]);
  });

  // ---------------------------------------------------------------- viewer (the frame is shotviewer.js)
  function openViewer(start) {
    let cur = start;
    let savedCaption = '';
    let viewer;
    const caption = h('input.shot-caption', { type: 'text', maxlength: '500', placeholder: 'Add a caption', 'aria-label': 'Caption' });
    const earlier = h('button.btn', { type: 'button', onclick: () => move(-1) }, '◀ Move earlier');
    const later = h('button.btn', { type: 'button', onclick: () => move(1) }, 'Move later ▶');
    const del = h('button.btn.danger-text', { type: 'button', onclick: remove }, 'Delete');

    caption.addEventListener('change', saveCaption);
    caption.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); caption.blur(); } });
    async function saveCaption() {
      const s = shots[cur];
      const v = caption.value.trim();
      if (!s || v === savedCaption) return;
      try {
        const fresh = await setShotCaption(s.id, v);
        s.caption = fresh.caption || '';
        savedCaption = v;
        shotsChanged();
      } catch { toast('Could not save the caption'); }
    }

    async function move(dir) {
      await saveCaption();
      const s = shots[cur];
      const ids = moveShot(shots.map((x) => x.id), s.id, dir);
      if (!ids) return;
      const before = shots.slice();
      shots = ids.map((id) => shots.find((x) => x.id === id));
      viewer.show(cur + dir);
      draw();
      try {
        await orderShots(game.id, [...ids, ...pendingIds(pending, game.id)]);
        shotsChanged();
      } catch {
        shots = before;
        viewer.show(cur - dir);
        draw();
        toast('Could not save the new order');
      }
    }

    function remove() {
      const s = shots[cur];
      const at = cur;
      shots.splice(at, 1);
      const entry = { s, gameId: game.id, at, timer: 0 };
      pending.set(s.id, entry);
      entry.timer = setTimeout(async () => {
        if (pending.get(s.id) !== entry) return; // undone, or already sent when the page was hidden
        try {
          await deleteShot(s.id);
          pending.delete(s.id);
          forgetThumb(s.thumb);
          shotsChanged();
        } catch (e) {
          pending.delete(s.id);
          if (e.status === 404) { forgetThumb(s.thumb); shotsChanged(); return; } // already gone
          shots.splice(Math.min(at, shots.length), 0, s); // the server still has it
          draw();
          toast(e.status === 503 ? STORAGE_DOWN : 'Could not delete the screenshot');
        }
      }, UNDO_MS);
      entry.toast = toastAction('Screenshot deleted', 'Undo', () => {
        if (pending.get(s.id) !== entry) return;
        clearTimeout(entry.timer);
        pending.delete(s.id);
        shots.splice(Math.min(at, shots.length), 0, s);
        draw();
      }, UNDO_MS);
      draw();
      if (!shots.length) { viewer.close(); return; }
      savedCaption = '';
      viewer.show(Math.min(cur, shots.length - 1));
    }

    viewer = openShotViewer({
      count: () => shots.length,
      get: (i) => shots[i],
      beforeStep: saveCaption,
      barEnd: del,
      foot: (meta) => [caption, meta, h('div.shot-move', {}, earlier, later)],
      ignoreKey: (e) => e.target === caption,
      onShow: (s, i) => {
        cur = i;
        caption.value = savedCaption = s.caption || '';
        earlier.disabled = i === 0;
        later.disabled = i === shots.length - 1;
      },
      onClose: () => {
        // draw() rebuilt the grid since the viewer opened, so the old thumb is gone: focus the current one
        const i = clampIndex(cur, shots.length);
        (i < 0 ? addBtn : grid.children[i])?.focus?.({ preventScroll: true });
      },
    }, start);
  }

  load();
  return root;
}
