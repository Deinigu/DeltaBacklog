// Browser smoke test: opens every page of the demo app in headless Chrome, at phone and PC width, clicks the
// controls on each page, and fails on any JavaScript error. It catches what the node tests cannot: a page or
// sheet that throws when it is used (a deal that did nothing when tapped slipped through review that way).
//
//   cd docs/tools && npm install puppeteer        (node_modules is git-ignored)
//   node smoke.mjs                                 (CI runs it too: .github/workflows/ci.yml)
//
// It builds the demo library (docs/tools/demo-data, no personal data) into a temporary directory and serves it
// on a free 127.0.0.1 port. No keys are read: the environment is cleared of them, and the price-check key is a
// made-up one so the Deals page shows the demo prices (Refresh and other buttons that call outside services are
// never clicked). SMOKE_CLICKS=n changes how many controls are tried per page (default 30); SMOKE_PAGES limits the
// pages (see below).
import puppeteer from 'puppeteer';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, '..', '..');
const CLICKS = Number(process.env.SMOKE_CLICKS || 30);

const ALL_PAGES = [
  '#/library', '#/library?layout=table', '#/views', '#/view/2', '#/series', '#/upcoming', '#/similar',
  '#/deals', '#/deals?view=wishlist', '#/hidden', '#/stats', '#/years', '#/rankings', '#/screenshots',
  '#/attention', '#/link',
];
// SMOKE_PAGES=deals,stats runs only the pages whose route contains one of the words (quicker while working on one page).
const only = (process.env.SMOKE_PAGES || '').split(',').map((s) => s.trim()).filter(Boolean);
const PAGES = only.length ? ALL_PAGES.filter((p) => only.some((w) => p.includes(w))) : ALL_PAGES;
const WIDTHS = [{ width: 390, height: 844, isMobile: true, hasTouch: true }, { width: 1280, height: 900 }];
// Controls that change data in ways that end the page, or call services outside the demo, are not clicked. (Add on a made-up IGDB game asks IGDB for it.)
const SKIP = /add .* to your library|delete|remove|refresh|send|sync|find times|fetch|export|download|import|upload|re-?link|unlink|test message|check now/i;

const freePort = () => new Promise((res, rej) => { const s = createServer(); s.on('error', rej); s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => res(port)); }); });
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !/IGDB|STEAMGRIDDB|ITAD|STEAM_|TELEGRAM|HLTB|_KEY|_TOKEN|SECRET/i.test(k)));
const tmp = mkdtempSync(join(tmpdir(), 'deltabacklog-smoke-'));
const data = join(tmp, 'data');
const exe = join(tmp, process.platform === 'win32' ? 'deltabacklog-smoke.exe' : 'deltabacklog-smoke');
for (const args of [['run', './docs/tools/demo-data', '-fake-igdb', '-data', data], ['build', '-buildvcs=false', '-o', exe, './cmd/deltabacklog']]) {
  if (spawnSync('go', args, { cwd: ROOT, env, stdio: 'inherit' }).status !== 0) { console.error('go failed (is Go on PATH?)'); process.exit(1); }
}
const port = await freePort();
const BASE = `http://127.0.0.1:${port}`;
const server = spawn(exe, [], {
  cwd: tmp,
  env: { ...env, LISTEN_ADDR: `127.0.0.1:${port}`, DATA_DIR: data, ITAD_API_KEY: '0'.repeat(40), STEAM_ID: '76561190000000000' },
  stdio: ['ignore', 'ignore', 'inherit'],
});
let up = false;
for (let i = 0; i < 60 && !up; i++) { try { up = (await fetch(`${BASE}/healthz`)).ok; } catch {} if (!up) await wait(500); }

const failures = [];
let browser;
try {
  if (!up) throw new Error(`the demo server did not answer at ${BASE}/healthz within 30 s`);
  browser = await puppeteer.launch({ headless: true, executablePath: process.env.CHROME_PATH || undefined, args: ['--no-sandbox'] });
  for (const vp of WIDTHS) {
    const p = await browser.newPage();
    await p.setViewport(vp);
    let where = '';
    p.on('pageerror', (e) => failures.push(`${vp.width}px ${where}: ${e.message}`));
    p.on('console', (m) => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) failures.push(`${vp.width}px ${where}: console: ${m.text()}`); });
    p.on('response', (r) => { if (r.status() >= 500 && r.url().startsWith(BASE)) failures.push(`${vp.width}px ${where}: HTTP ${r.status()} ${r.url().slice(BASE.length)}`); });
    p.on('dialog', (d) => d.dismiss());
    for (const hash of PAGES) {
      const open = async () => { await p.goto('about:blank'); await p.goto(`${BASE}/${hash}`, { waitUntil: 'networkidle0' }); await wait(400); };
      where = hash;
      await open();
      let clicked = 0;
      for (let i = 0; clicked < CLICKS; i++) {
        // The i-th visible, safe control on the page, found again each time (clicks re-render).
        const label = await p.evaluate((i, skip) => {
          const re = new RegExp(skip, 'i');
          const els = [...document.querySelectorAll('#view button, #view [role="button"], #view [tabindex="0"], #view a[href^="#"], #view summary')]
            .filter((el) => { const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0 && !el.disabled && !re.test(el.textContent + ' ' + (el.getAttribute('aria-label') || '')); });
          const el = els[i];
          if (!el) return null;
          el.setAttribute('data-smoke', '1');
          return (el.getAttribute('aria-label') || el.textContent || el.tagName).trim().replace(/\s+/g, ' ').slice(0, 40);
        }, i, SKIP.source);
        if (label === null) break;
        where = `${hash} → "${label}"`;
        try { await p.click('[data-smoke="1"]'); } catch { /* covered or gone: not a page error */ }
        await p.evaluate(() => document.querySelector('[data-smoke]')?.removeAttribute('data-smoke'));
        clicked++;
        await wait(250);
        // Close what the click opened (sheet, drawer, menu) and go back to the page if it navigated away.
        await p.keyboard.press('Escape'); await wait(100); await p.keyboard.press('Escape'); await wait(100);
        if (p.url().split('?')[0] !== `${BASE}/${hash.split('?')[0]}`) { where = hash; await open(); } // a page rewrites its own query (filters): only leaving the page counts
      }
      console.log(`${vp.width}px ${hash}: ${clicked} controls clicked`);
    }
    await p.close();
  }
} catch (e) {
  failures.push(`smoke test crashed: ${e.stack || e}`);
} finally {
  await browser?.close();
  server.kill();
  await wait(300);
  try { rmSync(tmp, { recursive: true, force: true }); } catch {}
}
if (failures.length) {
  console.error(`\n${failures.length} problem(s):\n` + [...new Set(failures)].map((f) => `  - ${f}`).join('\n'));
  process.exit(1);
}
console.log('\nsmoke test passed: no page errors');
