// The "Screenshots" section of the game drawer: thumbnail grid, upload with progress, and the full-screen viewer.
import { h, toast, toastAction } from './util.js';
import { listShots, uploadShot, setShotCaption, deleteShot, orderShots } from './api.js';
import { moveShot, stepIndex, formatBytes, formatTaken, countLabel, pickImages, uploadSummary } from './shots.js';

const UNDO_MS = 6000;
const STORAGE_DOWN = 'Screenshot storage is not available (is the drive connected?). Uploads will fail until it is back.';

/** Build the section for one game; it loads its own list. */
export function screenshotsSection(game) {
  let shots = [];
  let pending = []; // deleted in the UI, still on the server until the undo time is over
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
      shots = r.screenshots;
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
        const s = await uploadShot(game.id, f, (p) => { bar.value = p; });
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
    const dl = h('a.btn', { 'aria-label': 'Download the original', title: 'Download the original', download: '' }, '⬇');
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
        await orderShots(game.id, [...ids, ...pending.map((p) => p.s.id)]);
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
      const entry = { s, at, timer: 0 };
      pending.push(entry);
      entry.timer = setTimeout(async () => {
        pending = pending.filter((p) => p !== entry);
        try { await deleteShot(s.id); } catch {
          shots.splice(Math.min(at, shots.length), 0, s); // the server still has it
          draw();
          toast('Could not delete the screenshot');
        }
      }, UNDO_MS);
      toastAction('Screenshot deleted', 'Undo', () => {
        clearTimeout(entry.timer);
        pending = pending.filter((p) => p !== entry);
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
      lastFocus?.focus?.({ preventScroll: true });
    }

    const onKey = (e) => {
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

    const lastFocus = document.activeElement;
    document.addEventListener('keydown', onKey);
    document.body.classList.add('no-scroll');
    document.body.append(view);
    show();
    view.focus({ preventScroll: true });
  }

  load();
  return root;
}
