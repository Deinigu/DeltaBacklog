// Quick checks on a picked image file before it is sent (the server checks the real bytes again).
export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;
const TYPES = ['image/jpeg', 'image/png', 'image/webp'];

/** What is wrong with a picked file, or '' when it may be sent. The type is the browser's guess; the server judges the bytes. */
export function uploadProblem(file) {
  if (!file) return 'No file chosen.';
  if (file.type && !TYPES.includes(file.type)) return 'That file is not a JPEG, PNG or WebP image.';
  if (file.size > MAX_UPLOAD_BYTES) return 'That image is larger than 10 MB.';
  if (file.size === 0) return 'That file is empty.';
  return '';
}
