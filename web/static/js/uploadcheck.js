// Quick checks on a picked image file before it is sent (the server checks the real bytes again).
export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;
export const PICK_TYPES = 'image/jpeg,image/png,image/webp'; // the file input's accept: also makes iOS convert HEIC to JPEG
const TYPES = PICK_TYPES.split(',');

/** What is wrong with a picked file, or '' when it may be prepared. The type is the browser's guess; the server judges the bytes. The size is checked later (sizeProblem), after the picture was shrunk. */
export function uploadProblem(file) {
  if (!file) return 'No file chosen.';
  if (file.type && !TYPES.includes(file.type)) return 'That file is not a JPEG, PNG or WebP image.';
  if (file.size === 0) return 'That file is empty.';
  return '';
}

/** What is wrong with the file that will really be sent (the shrunk picture, or the original when it could not be shrunk). */
export function sizeProblem(blob) {
  return blob.size > MAX_UPLOAD_BYTES ? 'That image is larger than 10 MB.' : '';
}

/** "that image is too large" -> "That image is too large": server texts are lowercase sentences. */
export const sentence = (msg) => (msg ? msg[0].toUpperCase() + msg.slice(1) : msg);
