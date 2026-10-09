// Regenerates the screenshots used by the README and the manuals (docs/img/*.jpg).
//
//   cd docs/tools && npm install puppeteer        (node_modules is git-ignored)
//   node screenshots.mjs
//
// By default it builds the demo library (docs/tools/demo-data: 40 well-known games, no personal data), starts a
// throwaway server on a free port and shoots that. Go must be on PATH.
//
// Covers: without keys the demo uses generated placeholder covers. With IGDB / SteamGridDB keys (in the environment,
// or in a .env file: ENV_FILE=/path/to/.env, default the repo's .env) the real covers are downloaded into the
// throwaway data directory, and the add-a-game and cover-picker screens are shot too. The cover files are never
// committed; the keys are only passed to the child processes. The images show cover art from IGDB and SteamGridDB,
// (c) their respective owners, for illustration.
//
// To shoot a server you started yourself instead: BASE_URL=http://127.0.0.1:18100 node screenshots.mjs
// (a TEST server, never your real one: the script saves nothing, but keep it away from live data anyway).
import puppeteer from 'puppeteer';
import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, '..', '..');
const OUT = join(HERE, '..', 'img');
mkdirSync(OUT, { recursive: true });

const freePort = () => new Promise((res, rej) => { const s = createServer(); s.on('error', rej); s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => res(port)); }); });

// Keys from ENV_FILE (or the repo's .env); real environment variables win.
const childEnv = { ...process.env };
const envFile = process.env.ENV_FILE || join(ROOT, '.env');
if (existsSync(envFile)) {
  for (const line of readFileSync(envFile, 'utf8').split(/\r?\n/)) {
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*["']?(.*?)["']?\s*$/);
    if (m && !childEnv[m[1]]) childEnv[m[1]] = m[2];
  }
}
let lookups = !!(childEnv.IGDB_CLIENT_ID && childEnv.IGDB_CLIENT_SECRET); // add-a-game and cover-picker need IGDB

let server = null;
let tmp = null;
let BASE = (process.env.BASE_URL || '').replace(/\/$/, '');
if (!BASE) {
  tmp = mkdtempSync(join(tmpdir(), 'deltabacklog-demo-'));
  const data = join(tmp, 'data');
  const exe = join(tmp, process.platform === 'win32' ? 'deltabacklog.exe' : 'deltabacklog');
  for (const args of [['run', './docs/tools/demo-data', '-data', data], ['build', '-o', exe, './cmd/deltabacklog']]) {
    if (spawnSync('go', args, { cwd: ROOT, env: childEnv, stdio: 'inherit' }).status !== 0) { console.error('go failed (is Go on PATH?)'); process.exit(1); }
  }
  const port = await freePort();
  BASE = `http://127.0.0.1:${port}`;
  // Started in the repo root; the keys (if any) reach it through the environment.
  server = spawn(exe, [], { cwd: ROOT, env: { ...childEnv, LISTEN_ADDR: `127.0.0.1:${port}`, DATA_DIR: data }, stdio: 'inherit' });
  let up = false;
  for (let i = 0; i < 60; i++) { try { if ((await fetch(`${BASE}/healthz`)).ok) { up = true; break; } } catch {} await new Promise((r) => setTimeout(r, 500)); }
  if (!up) { console.error(`the demo server did not answer at ${BASE}/healthz within 30 s`); process.exit(1); }
}
process.on('exit', () => { if (server) server.kill(); if (tmp) { try { rmSync(tmp, { recursive: true, force: true }); } catch {} } });

if (process.env.BASE_URL) lookups = true; // your own server: assume it has keys
const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
const wait = (ms = 600) => new Promise((r) => setTimeout(r, ms));

async function page(width, height, mobile) {
  const p = await browser.newPage();
  await p.setViewport({ width, height, deviceScaleFactor: mobile ? 2 : 1, isMobile: mobile, hasTouch: mobile });
  await p.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'light' }]); // the manuals show the light theme
  return p;
}
const shot = (p, name) => p.screenshot({ path: join(OUT, `${name}.jpg`), type: 'jpeg', quality: 78 });
const go = async (p, hash) => { await p.goto('about:blank'); await p.goto(`${BASE}/${hash}`, { waitUntil: 'networkidle0' }); await wait(900); };
const clickText = (p, selector, text) => p.$$eval(selector, (els, t) => { const e = els.find((x) => x.textContent.trim().includes(t)); if (e) e.click(); return !!e; }, text);

// ---- phone ----
let p = await page(390, 844, true);
await go(p, '#/library?sort=status:asc,last_year:desc,title:asc&q=');
await shot(p, 'library-phone');

await p.click('.open-filters'); await wait(500);
await p.$$eval('details.group', (d) => d.forEach((x) => { x.open = x.textContent.trim().startsWith('Status') || x.textContent.trim().startsWith('Ownership'); }));
await wait(300);
await shot(p, 'filters-phone');

await go(p, '#/library');
await p.click('.sort-btn'); await wait(500);
await shot(p, 'sort-phone');

await go(p, '#/views');
await shot(p, 'views-phone');

await go(p, '#/series');
await p.$$eval('.scard', (ds) => { const d = ds.find((x) => x.textContent.includes('Ace Attorney')) || ds[0]; d.open = true; d.scrollIntoView(); });
await wait(300);
await shot(p, 'series-phone');

await go(p, '#/stats');
await shot(p, 'stats-phone');

await go(p, '#/years'); // the latest year with games
await shot(p, 'year-phone');
await p.$eval('.tiers-box', (e) => e.scrollIntoView()); await wait(500);
await shot(p, 'tierlist-phone');

await go(p, '#/link'); // needs at least one game waiting in the IGDB review queue (see the note at the top)
await shot(p, 'link-phone');

await go(p, '#/view/2'); // "Up next"
await clickText(p, '.view-head .btn', 'Reorder'); await wait(700);
await shot(p, 'reorder-phone');

await go(p, '#/view/2');
await p.click('.surprise-btn'); await wait(600);
await shot(p, 'surprise-mood-phone');
await clickText(p, '.sheet.open .btn', 'Pick one'); await wait(700);
await shot(p, 'surprise-result-phone');

await go(p, '#/library');
await p.click('.more-btn'); await wait(500);
await shot(p, 'more-phone');

await go(p, '#/library?q=outer+wilds');
await p.click('.card'); await wait(700);
await shot(p, 'game-phone');
await clickText(p, '.drawer-actions .btn', 'Edit'); await wait(700);
await shot(p, 'edit-phone');
await clickText(p, '.editbar .btn', 'Cancel'); await wait(400);
if (lookups) { // these need real IGDB / SteamGridDB keys
  await clickText(p, '.drawer-actions .btn', 'Change cover'); await wait(500);
  await clickText(p, '.sheet.open .mode button', 'IGDB'); await wait(4500);
  await shot(p, 'cover-picker-phone');

  await go(p, '');
  await p.click('.fab'); await wait(500);
  await p.type('.sheet.open .pick-search input', 'Sekiro');
  await p.waitForSelector('.cand', { timeout: 30000 }); await wait(2500);
  await shot(p, 'add-search-phone');
  // the first match that is not already in the library, so the form has no "already in your library" banner
  await p.$$eval('.cand', (c) => (c.find((x) => !x.querySelector('.inlib')) || c[0]).click()); await wait(5000);
  await shot(p, 'add-form-phone');
}
await p.close();

// ---- phone navigation: the bottom bar and the More sheet must fit (390px and 360px) ----
for (const width of [390, 360]) {
  const n = await page(width, 780, true);
  await go(n, '#/library');
  const bar = await n.$$eval('nav a:not(.extra), nav .more-btn', (els) => els.filter((e) => e.offsetParent).map((e) => ({ t: e.textContent.trim(), w: e.getBoundingClientRect().width, h: e.getBoundingClientRect().height, r: e.getBoundingClientRect().right })));
  await n.click('.more-btn'); await wait(500);
  const more = await n.$$eval('.more-link', (els) => els.map((e) => ({ t: e.textContent.trim(), h: e.getBoundingClientRect().height, r: e.getBoundingClientRect().right })));
  const scroll = await n.evaluate(() => document.scrollingElement.scrollWidth - window.innerWidth);
  const small = [...bar, ...more].filter((e) => e.h < 44 || e.r > width);
  console.log(`nav ${width}px: bar [${bar.map((e) => e.t).join(', ')}], More [${more.map((e) => e.t).join(', ')}], horizontal overflow ${scroll}px`);
  if (small.length || scroll > 0 || more.length < 5) { console.error('NAV CHECK FAILED', JSON.stringify({ small, scroll, more: more.length })); process.exitCode = 1; }
  await n.close();
}

// ---- desktop ----
p = await page(1280, 800, false);
await go(p, '#/library?sort=status:asc,last_year:desc,title:asc');
await shot(p, 'library-desktop');
await go(p, '#/library?layout=table&sort=title:asc');
await shot(p, 'table-desktop');

await browser.close();
console.log('screenshots written to', OUT);
