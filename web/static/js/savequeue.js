// Debounced, single-flight save queue for state that is saved whole (the tier list). Pure (no DOM): unit-tested.
//
// `change()` marks the state dirty and saves it after `delay` ms of quiet. Only one save runs at a time; a change
// made while a save is in flight is saved right after it (or after its own debounce), so the last state always wins.
// `save()` reads the latest state itself. A failed save is retried after `retryDelay` ms, unless `isFatal(err)`:
// then it is dropped and `onFatal(err)` is called instead of retrying the same rejected body forever.

export function createSaveQueue({ save, delay = 400, retryDelay = 4000, isFatal = () => false, onError = () => {}, onFatal = () => {}, timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (t) => clearTimeout(t) } }) {
  let timer = null, saving = false, dirty = false, onSettled = null, inflight = null;

  const arm = (ms) => { timers.clear(timer); timer = timers.set(run, ms); };

  function run() {
    timer = null;
    if (saving || !dirty) return inflight || Promise.resolve(false);
    inflight = doSave();
    return inflight;
  }

  /** One save; resolves true when it failed. */
  async function doSave() {
    saving = true; dirty = false;
    let failed = null;
    try { await save(); } catch (err) { failed = err; }
    saving = false;
    if (failed) {
      if (isFatal(failed)) { dirty = false; onFatal(failed); } else { dirty = true; onError(failed); if (!timer) arm(retryDelay); }
    }
    if (onSettled && dirty) { const f = onSettled; onSettled = null; dirty = false; f(); return !!failed; }
    onSettled = null;
    if (dirty && !timer && !failed) run(); // changed during the save and no debounce pending: save it now
    return !!failed;
  }

  return {
    change() { dirty = true; arm(delay); },
    /**
     * Save now and resolve when everything changed so far is saved: it waits for a save that is already running, then
     * saves what changed meanwhile. A failed save ends it (the retry stays scheduled), so it never loops on an outage.
     */
    async flush() {
      timers.clear(timer); timer = null;
      for (;;) {
        if (saving) { if (await inflight) return; continue; }
        if (!dirty) return;
        if (await run()) return;
      }
    },
    /**
     * The page is going away: `send()` the unsaved state by another route (a keepalive request). If a save is in
     * flight, it is called when that save ends, if anything is still unsaved, so the last change is never dropped.
     */
    finish(send) {
      timers.clear(timer); timer = null;
      if (saving) { onSettled = send; return; }
      if (dirty) { dirty = false; send(); }
    },
    get dirty() { return dirty; },
    get saving() { return saving; },
  };
}
