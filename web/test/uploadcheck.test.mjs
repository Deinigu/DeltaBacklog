import test from 'node:test';
import assert from 'node:assert/strict';
import { uploadProblem, MAX_UPLOAD_BYTES } from '../static/js/uploadcheck.js';

test('uploadProblem accepts JPEG, PNG and WebP up to 10 MB', () => {
  for (const type of ['image/jpeg', 'image/png', 'image/webp']) assert.equal(uploadProblem({ type, size: 1000 }), '');
  assert.equal(uploadProblem({ type: 'image/png', size: MAX_UPLOAD_BYTES }), '');
});

test('uploadProblem refuses other types, big and empty files', () => {
  assert.match(uploadProblem({ type: 'image/gif', size: 10 }), /JPEG, PNG or WebP/);
  assert.match(uploadProblem({ type: 'application/pdf', size: 10 }), /JPEG, PNG or WebP/);
  assert.match(uploadProblem({ type: 'image/png', size: MAX_UPLOAD_BYTES + 1 }), /10 MB/);
  assert.match(uploadProblem({ type: 'image/png', size: 0 }), /empty/);
  assert.match(uploadProblem(null), /No file/);
});

test('uploadProblem leaves an unknown type to the server', () => {
  assert.equal(uploadProblem({ type: '', size: 500 }), '');
});
