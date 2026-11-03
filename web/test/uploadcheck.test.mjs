import test from 'node:test';
import assert from 'node:assert/strict';
import { uploadProblem, sizeProblem, sentence, MAX_UPLOAD_BYTES, PICK_TYPES } from '../static/js/uploadcheck.js';
import { planPrep, COVER_WIDTH, POSTER_WIDTH } from '../static/js/imageprep.js';

test('uploadProblem accepts JPEG, PNG and WebP of any size (the picture is shrunk first)', () => {
  for (const type of ['image/jpeg', 'image/png', 'image/webp']) assert.equal(uploadProblem({ type, size: 1000 }), '');
  assert.equal(uploadProblem({ type: 'image/jpeg', size: 40 * 1024 * 1024 }), '');
  assert.equal(PICK_TYPES, 'image/jpeg,image/png,image/webp');
});

test('uploadProblem refuses other types and empty files', () => {
  assert.match(uploadProblem({ type: 'image/gif', size: 10 }), /JPEG, PNG or WebP/);
  assert.match(uploadProblem({ type: 'image/heic', size: 10 }), /JPEG, PNG or WebP/);
  assert.match(uploadProblem({ type: 'image/png', size: 0 }), /empty/);
  assert.match(uploadProblem(null), /No file/);
  assert.equal(uploadProblem({ type: '', size: 500 }), ''); // unknown type: the server judges the bytes
});

test('sizeProblem checks what will really be sent', () => {
  assert.equal(sizeProblem({ size: MAX_UPLOAD_BYTES }), '');
  assert.match(sizeProblem({ size: MAX_UPLOAD_BYTES + 1 }), /10 MB/);
});

test('sentence capitalises a server message', () => {
  assert.equal(sentence('that image is damaged'), 'That image is damaged');
  assert.equal(sentence(''), '');
});

test('planPrep shrinks a wide photo for a cover and never enlarges', () => {
  assert.deepEqual(planPrep(4000, 3000, 'cover'), { sx: 0, sy: 0, sw: 4000, sh: 3000, dw: COVER_WIDTH, dh: 480 });
  assert.deepEqual(planPrep(300, 200, 'cover'), { sx: 0, sy: 0, sw: 300, sh: 200, dw: 300, dh: 200 });
});

test('planPrep crops the centre 2:3 for a poster', () => {
  const wide = planPrep(4000, 3000, 'poster'); // landscape: the sides are cut
  assert.equal(wide.sh, 3000); assert.equal(wide.sw, 2000); assert.equal(wide.sx, 1000);
  assert.deepEqual([wide.dw, wide.dh], [POSTER_WIDTH, 900]);
  const tall = planPrep(3000, 6000, 'poster'); // taller than 2:3: top and bottom are cut
  assert.equal(tall.sw, 3000); assert.equal(tall.sh, 4500); assert.equal(tall.sy, 750);
  assert.deepEqual(planPrep(200, 300, 'poster'), { sx: 0, sy: 0, sw: 200, sh: 300, dw: 200, dh: 300 });
});

test('planPrep survives degenerate sizes', () => {
  const p = planPrep(1, 1, 'poster');
  assert.ok(p.dw >= 1 && p.dh >= 1 && p.sw >= 1 && p.sh >= 1);
});
