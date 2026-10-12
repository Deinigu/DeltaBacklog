// Pure helpers for the screenshot gallery (no DOM): ordering, formatting, picking the files to upload.

/** Ids in the new order after moving one screenshot a place earlier (dir -1) or later (dir 1); null when it cannot move. */
export function moveShot(ids, id, dir) {
  const i = ids.indexOf(id);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= ids.length) return null;
  const out = ids.slice();
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** The index to show after a step in the viewer, wrapping around; -1 for an empty gallery. */
export function stepIndex(i, len, dir) {
  if (len <= 0) return -1;
  return (i + dir + len) % len;
}

/** "340 KB", "2.4 MB". */
export function formatBytes(n) {
  if (!Number.isFinite(n) || n < 0) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / 1048576).toFixed(n < 10 * 1048576 ? 1 : 0)} MB`;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** "14 Nov 2023" from the API's taken_at (2006-01-02T15:04:05 with or without Z); '' when absent or unreadable. */
export function formatTaken(iso) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso || '');
  if (!m || +m[2] < 1 || +m[2] > 12) return '';
  return `${+m[3]} ${MONTHS[+m[2] - 1]} ${m[1]}`;
}

/** "Screenshots (3)" heading text. */
export const countLabel = (n) => (n ? `Screenshots (${n})` : 'Screenshots');

const IMAGE_EXT = /\.(jpe?g|png|webp)$/i;
const IMAGE_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp']);

/** Splits picked files into those worth uploading (JPEG, PNG, WebP) and the names of the others. */
export function pickImages(files) {
  const ok = [];
  const skipped = [];
  for (const f of files) (IMAGE_TYPES.has(f.type) || (!f.type && IMAGE_EXT.test(f.name)) ? ok : skipped).push(f);
  return { ok, skipped: skipped.map((f) => f.name) };
}

/** One line for the toast after an upload: "Added 3 screenshots", "Added 2, 1 failed". */
export function uploadSummary(added, failed) {
  const a = added === 1 ? 'Added 1 screenshot' : `Added ${added} screenshots`;
  if (!failed) return a;
  return added ? `${a}, ${failed} failed` : `${failed} failed`;
}

/** The ids of a game's screenshots deleted in the UI whose DELETE has not been answered yet, newest last. */
export function pendingIds(pending, gameId) {
  return [...pending.values()].filter((p) => p.gameId === gameId).map((p) => p.s.id);
}

/** A list without the screenshots whose deletion is pending. */
export function withoutPending(list, pending) {
  return list.filter((s) => !pending.has(s.id));
}

/** The index to focus after the list changed: the same place, clamped; -1 when empty. */
export function clampIndex(i, len) {
  return len <= 0 ? -1 : Math.max(0, Math.min(i, len - 1));
}

/**
 * How the browser makes the preview for a picture the server cannot decode: the same rule as the server's thumbPlan
 * (internal/shots): keep the centre part with a ratio between 1:2 and 4:1 (cw x ch, of w x h as shown), and draw it at
 * tw x th, inside 480x960 and never enlarged. Keep the two in sync.
 */
export function thumbPlan(w, h) {
  if (!(w > 0) || !(h > 0)) return { cw: 0, ch: 0, tw: 0, th: 0 };
  let cw = w;
  let ch = h;
  if (ch > 2 * cw) ch = 2 * cw;
  if (cw > 4 * ch) cw = 4 * ch;
  let tw = cw;
  let th = ch;
  if (tw > 480 || th > 960) {
    const f = Math.min(480 / cw, 960 / ch);
    tw = Math.max(1, Math.floor(cw * f));
    th = Math.max(1, Math.floor(ch * f));
  }
  return { cw, ch, tw, th };
}

/** Files above this size (bytes) get their preview made in the browser before the upload, so a big photo goes up once. */
export const BIG_UPLOAD = 8 * 1024 * 1024;
