import test from 'node:test';
import assert from 'node:assert/strict';
import { createSaveQueue } from '../static/js/savequeue.js';

// A fake clock and a save whose completion the test controls.
function rig(opts = {}) {
  let now = 0, id = 0;
  const pending = new Map();
  const timers = { set: (fn, ms) => { pending.set(++id, { fn, at: now + ms }); return id; }, clear: (t) => pending.delete(t) };
  const saves = [];
  const gates = [];
  const q = createSaveQueue({
    timers, ...opts,
    save: () => new Promise((resolve, reject) => { saves.push(1); gates.push({ resolve, reject }); }),
  });
  const tick = async (ms) => {
    now += ms;
    for (const [k, t] of [...pending]) if (t.at <= now) { pending.delete(k); t.fn(); }
    await Promise.resolve(); await Promise.resolve();
  };
  const settle = async (i, err) => { err ? gates[i].reject(err) : gates[i].resolve(); await new Promise((r) => setImmediate(r)); };
  return { q, tick, settle, saves, pending };
}

test('changes are debounced into one save', async () => {
  const r = rig();
  r.q.change(); await r.tick(200); r.q.change(); await r.tick(300);
  assert.equal(r.saves.length, 0);
  await r.tick(200);
  assert.equal(r.saves.length, 1);
  await r.settle(0);
  assert.equal(r.q.dirty, false);
});

test('a change made during a save is saved right after it (the bug: it was dropped)', async () => {
  const r = rig();
  r.q.change(); await r.tick(400);
  assert.equal(r.saves.length, 1);
  r.q.change(); await r.tick(400); // its debounce fires while the first save is still in flight
  assert.equal(r.saves.length, 1);
  await r.settle(0);
  assert.equal(r.saves.length, 2);
  await r.settle(1);
  assert.equal(r.q.dirty, false);
});

test('a change during a save with its debounce still pending waits for the debounce', async () => {
  const r = rig();
  r.q.change(); await r.tick(400);
  r.q.change(); // debounce pending
  await r.settle(0);
  assert.equal(r.saves.length, 1);
  await r.tick(400);
  assert.equal(r.saves.length, 2);
});

test('a failed save is retried once per retry delay, and keeps one timer', async () => {
  const errors = [];
  const r = rig({ onError: (e) => errors.push(e) });
  r.q.change(); await r.tick(400);
  await r.settle(0, new Error('offline'));
  assert.equal(errors.length, 1);
  assert.equal(r.pending.size, 1);
  await r.tick(4000);
  assert.equal(r.saves.length, 2);
  await r.settle(1);
  assert.equal(r.q.dirty, false);
});

test('a fatal error is not retried with the same body', async () => {
  const fatal = [];
  const r = rig({ isFatal: (e) => e.status === 400, onFatal: (e) => fatal.push(e.status) });
  r.q.change(); await r.tick(400);
  const e = new Error('bad'); e.status = 400;
  await r.settle(0, e);
  assert.deepEqual(fatal, [400]);
  assert.equal(r.pending.size, 0);
  await r.tick(10000);
  assert.equal(r.saves.length, 1);
  assert.equal(r.q.dirty, false);
});

test('finish sends unsaved state at once and cancels the debounce', async () => {
  const r = rig();
  let sent = 0;
  r.q.change();
  r.q.finish(() => sent++);
  assert.equal(sent, 1);
  assert.equal(r.pending.size, 0);
  r.q.finish(() => sent++); // nothing unsaved any more
  assert.equal(sent, 1);
});

test('finish while saving sends the last state once the save ends', async () => {
  const r = rig();
  let sent = 0;
  r.q.change(); await r.tick(400);
  r.q.change();
  r.q.finish(() => sent++);
  assert.equal(sent, 0);
  await r.settle(0);
  assert.equal(sent, 1);
  assert.equal(r.saves.length, 1);
});

test('finish while saving with nothing newer sends nothing', async () => {
  const r = rig();
  let sent = 0;
  r.q.change(); await r.tick(400);
  r.q.finish(() => sent++);
  await r.settle(0);
  assert.equal(sent, 0);
});

test('flush saves immediately', async () => {
  const r = rig();
  r.q.change();
  const p = r.q.flush();
  await Promise.resolve();
  assert.equal(r.saves.length, 1);
  await r.settle(0); await p;
});

test('flush waits for a save that is already running, then saves what changed meanwhile', async () => {
  const r = rig();
  r.q.change(); await r.tick(500);
  assert.equal(r.saves.length, 1); // a save is in flight
  r.q.change();                    // and the state changes again
  let done = false;
  const f = r.q.flush().then(() => { done = true; });
  await new Promise((x) => setImmediate(x));
  assert.equal(done, false, 'flush must not resolve while the first save is still running');
  await r.settle(0);
  assert.equal(r.saves.length, 2); // the change made meanwhile is saved next
  assert.equal(done, false);
  await r.settle(1);
  await f;
  assert.equal(done, true);
  assert.equal(r.q.dirty, false);
});

test('flush with nothing to save resolves at once, and after a failed save it does not loop', async () => {
  const r = rig();
  await r.q.flush();
  assert.equal(r.saves.length, 0);
  r.q.change(); await r.tick(500);
  const f = r.q.flush();
  await r.settle(0, Object.assign(new Error('offline'), { status: 503 }));
  await f; // resolved although the save failed; the retry stays scheduled
  assert.equal(r.saves.length, 1);
  assert.equal(r.q.dirty, true);
});
