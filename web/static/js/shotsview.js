// The "Screenshots" section of the game drawer: thumbnail grid, upload with progress, and the full-screen viewer.
import { h, toast, toastAction } from './util.js';
import { listShots, uploadShot, setShotCaption, deleteShot, deleteShotKeepalive, orderShots } from './api.js';
import { moveShot, stepIndex, formatBytes, formatTaken, countLabel, pickImages, uploadSummary, thumbFit, pendingIds, withoutPending, clampIndex } from './shots.js';

const UNDO_MS = 6000;
const STORAGE_DOWN = 'Screenshot storage is not available (is the drive connected?). Uploads will fail until it is back.';

// Screenshots deleted in the UI whose DELETE is still waiting out the undo time, by id. Module level on purpose: a
// drawer that is closed and reopened (a new section) must still hide them, and leaving the page must still delete them.
const pending = new Map(); // id -> { s, gameId, at, timer }
const THUMB_CACHE = 'screenshot-thumbs';

/** Drop a deleted picture's thumbnail from the service worker's cache so it cannot show as a ghost. */
function forgetThumb(url) {
  try { caches?.open(THUMB_CACHE).then((c) => c.delete(url)).catch(() => {}); } catch { /* no Cache API */ }
}

function flushPending() {
  for (const [id, e] of pending) {
    clearTimeout(e.timer);
    deleteShotKeepalive(id);
    forgetThumb(e.s.thumb);
  }
  pending.clear();
}
document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') flushPending(); });
window.addEventListener('pagehide', flushPending);

/** A small JPEG of the picture (at most 480x960), made in the browser from a downscaled decode. null when it cannot. */
async function makeThumb(file) {
  if (typeof createImageBitmap !== 'function') return null;
  let bmp;
  try {
    bmp = await createImageBitmap(file, { resizeWidth: 480, resizeQuality: 'medium' });
    const { w, h } = thumbFit(bmp.width, bmp.height);
    if (!w) return null;
    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#000';
    ctx.fillRect(0, 0, w, h);
    ctx.drawImage(bmp, 0, 0, w, h);
    return await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.8));
  } catch {
    return null;
  } finally {
    bmp?.close?.();
  }
}

const DOWNLOAD_ICON = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3v12m0 0l-4.5-4.5M12 15l4.5-4.5M4 20h16"/></svg>';

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
        const s = await uploadShot(game.id, f, (p) => { bar.value = p; }, await makeThumb(f));
        shots.push(s);
        added++;
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

  // ---------------------------------------------------------------- viewer
  function openViewer(start) {
    let cur = start;
    const img = h('img.shot-img', { alt: '' });
    const counter = h('span.shot-counter');
    const caption = h('input.shot-caption', { type: 'text', maxlength: '500', placeholder: 'Add a caption', 'aria-label': 'Caption' });
    const meta = h('p.shot-meta');
    const dl = h('a.btn', { 'aria-label': 'Download the original', title: 'Download the original', download: '' });
    dl.innerHTML = DOWNLOAD_ICON;
    const earlier = h('button.btn', { type: 'button', onclick: () => move(-1) }, '◀ Move earlier');
    const later = h('button.btn', { type: 'button', onclick: () => move(1) }, 'Move later ▶');
    const prev = h('button.btn.shot-nav.prev', { type: 'button', 'aria-label': 'Previous screenshot', onclick: () => go(-1) }, '‹');
    const next = h('button.btn.shot-nav.next', { type: 'button', 'aria-label': 'Next screenshot', onclick: () => go(1) }, '›');
    const stage = h('div.shot-stage', {}, prev, img, next);
    const view = h('div.shot-viewer', { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Screenshot viewer', tabindex: '-1' },
      h('div.shot-bar', {},
        h('button.btn', { type: 'button', 'aria-label': 'Close', onclick: close }, '✕'), counter, h('span.spacer'),
        dl, h('button.btn.danger-text', { type: 'button', onclick: remove }, 'Delete')),
      stage,
      h('div.shot-foot', {}, caption, meta, h('div.shot-move', {}, earlier, later)));

    let savedCaption = '';
    caption.addEventListener('change', saveCaption);
    async function saveCaption() {
      const s = shots[cur];
      const v = caption.value.trim();
      if (!s || v === savedCaption) return;
      try {
        const fresh = await setShotCaption(s.id, v);
        s.caption = fresh.caption || '';
        savedCaption = v;
      } catch { toast('Could not save the caption'); }
    }

    function show() {
      const s = shots[cur];
      if (!s) { close(); return; }
      img.src = s.src;
      img.alt = s.caption || `Screenshot ${cur + 1}`;
      counter.textContent = `${cur + 1} / ${shots.length}`;
      caption.value = savedCaption = s.caption || '';
      meta.textContent = [formatTaken(s.taken_at), `${s.width}×${s.height}`, formatBytes(s.bytes)].filter(Boolean).join(' · ');
      dl.href = `${s.src}?download=1`;
      earlier.disabled = cur === 0;
      later.disabled = cur === shots.length - 1;
      prev.hidden = next.hidden = shots.length < 2;
    }

    async function go(dir) {
      await saveCaption();
      cur = stepIndex(cur, shots.length, dir);
      show();
    }

    async function move(dir) {
      await saveCaption();
      const s = shots[cur];
      const ids = moveShot(shots.map((x) => x.id), s.id, dir);
      if (!ids) return;
      const before = shots.slice();
      shots = ids.map((id) => shots.find((x) => x.id === id));
      cur += dir;
      show();
      draw();
      try {
        await orderShots(game.id, [...ids, ...pendingIds(pending, game.id)]);
      } catch {
        shots = before;
        cur -= dir;
        show();
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
        } catch (e) {
          pending.delete(s.id);
          if (e.status === 404) { forgetThumb(s.thumb); return; } // already gone
          shots.splice(Math.min(at, shots.length), 0, s); // the server still has it
          draw();
          toast(e.status === 503 ? STORAGE_DOWN : 'Could not delete the screenshot');
        }
      }, UNDO_MS);
      toastAction('Screenshot deleted', 'Undo', () => {
        if (pending.get(s.id) !== entry) return;
        clearTimeout(entry.timer);
        pending.delete(s.id);
        shots.splice(Math.min(at, shots.length), 0, s);
        draw();
      }, UNDO_MS);
      draw();
      if (!shots.length) { close(); return; }
      cur = Math.min(cur, shots.length - 1);
      savedCaption = '';
      show();
    }

    async function close() {
      await saveCaption();
      document.removeEventListener('keydown', onKey);
      view.remove();
      document.body.classList.remove('no-scroll');
      // draw() rebuilt the grid since the viewer opened, so the old thumb is gone: focus the current one
      const i = clampIndex(cur, shots.length);
      (i < 0 ? addBtn : grid.children[i])?.focus?.({ preventScroll: true });
    }

    const focusables = () => [...view.querySelectorAll('button, a[href], input')].filter((el) => !el.disabled && !el.hidden && el.offsetParent !== null);
    const onKey = (e) => {
      if (e.key === 'Tab') { // keep Tab inside the viewer: everything behind it is out of reach
        const list = focusables();
        if (!list.length) { e.preventDefault(); return; }
        const first = list[0];
        const last = list[list.length - 1];
        if (e.shiftKey && (document.activeElement === first || document.activeElement === view)) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
        else if (!view.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
        return;
      }
      if (e.target === caption) { if (e.key === 'Enter') { e.preventDefault(); caption.blur(); } else if (e.key !== 'Escape') return; }
      if (e.key === 'Escape') close();
      else if (e.key === 'ArrowLeft') go(-1);
      else if (e.key === 'ArrowRight') go(1);
    };

    // Swipe to change picture, only at normal zoom (a pinch-zoomed picture is panned instead).
    let x0 = null;
    let y0 = 0;
    stage.addEventListener('touchstart', (e) => {
      const zoomed = (window.visualViewport?.scale || 1) > 1.05;
      if (e.touches.length === 1 && !zoomed) { x0 = e.touches[0].clientX; y0 = e.touches[0].clientY; } else x0 = null;
    }, { passive: true });
    stage.addEventListener('touchend', (e) => {
      if (x0 == null) return;
      const dx = e.changedTouches[0].clientX - x0;
      const dy = e.changedTouches[0].clientY - y0;
      x0 = null;
      if (Math.abs(dx) > 60 && Math.abs(dx) > 2 * Math.abs(dy)) go(dx < 0 ? 1 : -1);
    }, { passive: true });

    document.addEventListener('keydown', onKey);
    document.body.classList.add('no-scroll');
    document.body.append(view);
    show();
    view.focus({ preventScroll: true });
  }

  load();
  return root;
}
