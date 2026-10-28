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
import { deflateSync, crc32 } from 'node:zlib';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, '..', '..');
const OUT = process.env.OUT_DIR || join(HERE, '..', 'img'); // OUT_DIR: write somewhere else (to keep the other images)
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
  // Portrait posters (the Series tiles and shelves show them); without keys the wide covers are used instead.
  if (lookups) spawnSync(exe, ['fetch-posters', '--db', join(data, 'app.db')], { cwd: ROOT, env: { ...childEnv, DATA_DIR: data }, stdio: 'inherit' }); // posters go next to the demo covers
  const port = await freePort();
  BASE = `http://127.0.0.1:${port}`;
  // Started in the repo root; the keys (if any) reach it through the environment.
  server = spawn(exe, [], { cwd: ROOT, env: { ...childEnv, LISTEN_ADDR: `127.0.0.1:${port}`, DATA_DIR: data }, stdio: 'inherit' });
  let up = false;
  for (let i = 0; i < 60; i++) { try { if ((await fetch(`${BASE}/healthz`)).ok) { up = true; break; } } catch {} await new Promise((r) => setTimeout(r, 500)); }
  if (!up) { console.error(`the demo server did not answer at ${BASE}/healthz within 30 s`); process.exit(1); }
}
// Placeholder pictures (generated here, nothing copyrighted) for the screenshot gallery of the demo's Outer Wilds.
function placeholderPNG(w, h, c1, c2) {
  const rows = [];
  for (let y = 0; y < h; y++) {
    const row = Buffer.alloc(1 + w * 3);
    for (let x = 0; x < w; x++) {
      const t = (x / w + y / h) / 2;
      const ring = Math.abs(Math.hypot(x - w * 0.62, y - h * 0.5) - h * 0.28) < 5; // a ring: a planet from far away
      for (let k = 0; k < 3; k++) row[1 + x * 3 + k] = ring ? 240 : Math.round(c1[k] + (c2[k] - c1[k]) * t);
    }
    rows.push(row);
  }
  const chunk = (type, data) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(8 + data.length + 4);
    out.writeUInt32BE(data.length, 0); body.copy(out, 4); out.writeUInt32BE(crc32(body) >>> 0, 8 + data.length);
    return out;
  };
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(Buffer.concat(rows))), chunk('IEND', Buffer.alloc(0))]);
}
if (!process.env.BASE_URL) {
  const games = await (await fetch(`${BASE}/api/games`)).json();
  const palettes = [[[16, 24, 64], [220, 120, 60]], [[10, 40, 50], [90, 170, 160]], [[40, 16, 60], [200, 90, 140]], [[20, 20, 30], [120, 130, 200]]];
  // Outer Wilds first (the game page shots use it), then a few more games, with dates in different years for the gallery page.
  const plan = [['Outer Wilds', 4, 2023], ['Hades', 3, 2024], ['Celeste', 3, 2022], ['Stardew Valley', 2, 2024]];
  for (const [title, n, yr] of plan) {
    const g = games.find((x) => x.title === title) || games.find((x) => x.title.includes(title));
    if (!g) continue;
    for (let i = 0; i < n; i++) {
      const [a, b] = palettes[(i + title.length) % palettes.length];
      const fd = new FormData();
      fd.append('modified', String(Date.UTC(yr, 1 + i * 3, 10 + i, 12)));
      fd.append('file', new Blob([placeholderPNG(640, 360, a, b)], { type: 'image/png' }), `demo-${i + 1}.png`);
      const r = await fetch(`${BASE}/api/games/${g.id}/screenshots`, { method: 'POST', body: fd });
      if (!r.ok) console.error('demo screenshot upload failed', r.status);
    }
  }
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
await p.$$eval('details.group', (d) => d.forEach((x) => { x.open = x.textContent.trim().startsWith('Status') || x.textContent.trim().startsWith('Time to beat'); }));
await wait(300);
await shot(p, 'filters-phone');

await go(p, '#/library');
await p.click('.sort-btn'); await wait(500);
await shot(p, 'sort-phone');

await go(p, '#/views');
await shot(p, 'views-phone');

await go(p, '#/series');
if (lookups && !process.env.BASE_URL) { // the games of the series' IGDB collections that the library lacks: read them (a few dozen requests)
  await p.evaluate(() => fetch('/api/series/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' } }));
  for (let i = 0; i < 120; i++) {
    const running = await p.evaluate(async () => (await (await fetch('/api/series/missing')).json()).refresh.running);
    if (!running) break;
    await wait(1000);
  }
  await go(p, '#/series');
}
await shot(p, 'series-phone');
await clickText(p, 'a.tile', 'Ace Attorney'); await wait(1200);
await shot(p, 'series-shelf-phone');

await go(p, '#/upcoming'); await wait(1200); // the wishlist (made-up dates) and the series' new games read above
await shot(p, 'upcoming-phone');

await go(p, '#/similar');
if (lookups && !process.env.BASE_URL) { // IGDB's similar games of the finished and favourite demo games (a handful of requests)
  await p.evaluate(() => fetch('/api/similar/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' } }));
  for (let i = 0; i < 120; i++) {
    const running = await p.evaluate(async () => (await (await fetch('/api/similar')).json()).refresh.running);
    if (!running) break;
    await wait(1000);
  }
  await go(p, '#/similar');
}
await wait(1200); // the posters load as they scroll into view
await shot(p, 'similar-phone');

await go(p, '#/deals'); await wait(1200); // made-up prices from the demo data (nothing is read from IsThereAnyDeal)
await shot(p, 'deals-phone');

await go(p, '#/stats');
await shot(p, 'stats-phone');

await go(p, '#/years'); // the latest year with games
await shot(p, 'year-phone');

// ---- rankings: a ranking like the ones TierMaker makes, seeded through the API of the throwaway server ----
let rankingId = null;
if (!process.env.BASE_URL) { // never write to a server we did not start
  rankingId = await p.evaluate(async () => {
    const send = async (method, url, body) => (await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) })).json();
    const games = (await (await fetch('/api/games')).json()).slice(0, 30);
    const r = await send('POST', '/api/rankings', { name: 'Mis juegos de 2025', preset: 'empty', game_ids: games.map((g) => g.id) });
    const labels = [['Me ha cambiado la vida', '#ff7f7f'], ['Me ha gustado mucho', '#ffbf7f'], ['Muy bueno, con alguna pega', '#ffdf7f'], ['Bien, sin más', '#ffff7f'],
      ['Mediocre, pero tiene sus cosillas', '#bfff7f'], ['No me ha convencido', '#7fffbf'], ['Lo dejé a medias', '#7fbfff'], ['Aburrido', '#bf7fff'], ['Vaya mierda', '#ff7fbf']];
    const tiers = [];
    for (const [label, color] of labels) tiers.push(await send('POST', `/api/rankings/${r.id}/tiers`, { label, color }));
    const sizes = [2, 4, 4, 3, 5, 2, 1, 1, 1]; // games per tier; the rest stay in the pool
    let k = 0;
    const board = tiers.map((t, i) => ({ id: t.id, label: t.label, color: t.color, games: games.slice(k, (k += sizes[i])).map((g) => g.id) }));
    await send('PUT', `/api/rankings/${r.id}/board`, { tiers: board, pool: games.slice(k).map((g) => g.id) });
    return r.id;
  });
  for (let i = 0; i < 90; i++) { // the posters are made in the background, one at a time
    const pending = await p.evaluate(async (id) => (await (await fetch(`/api/rankings/${id}`)).json()).posters_pending, rankingId);
    if (!pending) break;
    await wait(1000);
  }
  await go(p, '#/rankings');
  await shot(p, 'rankings-phone');
  await go(p, `#/rankings/${rankingId}`);
  await shot(p, 'ranking-phone');
}

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

await go(p, '#/screenshots');
await shot(p, 'gallery-phone');
await p.click('.shot-thumb'); await wait(900);
await shot(p, 'gallery-viewer-phone');
await p.keyboard.press('Escape'); await wait(300);

await go(p, '#/library?q=outer+wilds');
await p.click('.card'); await wait(700);
await shot(p, 'game-phone');
await p.$eval('.ttb', (e) => e.scrollIntoView()); await wait(400);
await shot(p, 'time-to-beat-phone');
await p.$eval('.shots', (e) => e.scrollIntoView()); await wait(900);
await shot(p, 'screenshots-phone');
await p.click('.shot-thumb'); await wait(900);
await shot(p, 'screenshot-viewer-phone');
await p.keyboard.press('Escape'); await wait(300);
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
await go(p, '#/screenshots');
await shot(p, 'gallery-desktop');
if (rankingId) {
  await go(p, `#/rankings/${rankingId}`);
  await shot(p, 'ranking-desktop');
}

await browser.close();
console.log('screenshots written to', OUT);
