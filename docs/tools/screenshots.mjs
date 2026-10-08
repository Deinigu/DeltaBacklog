// Regenerates the screenshots used by the manuals (docs/img/*.jpg) from a running DeltaBacklog.
//
//   cd docs/tools && npm install puppeteer        (node_modules is git-ignored)
//   BASE_URL=http://127.0.0.1:18100 node screenshots.mjs
//
// Point BASE_URL at a TEST server with a copy of your data, never at your real one: the script opens
// sheets and forms but saves nothing, yet it is still safest to keep it away from live data.
import puppeteer from 'puppeteer';
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const BASE = (process.env.BASE_URL || 'http://127.0.0.1:18100').replace(/\/$/, '');
const OUT = join(dirname(fileURLToPath(import.meta.url)), '..', 'img');
mkdirSync(OUT, { recursive: true });

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

await go(p, '#/view/2'); // "Up next"
await shot(p, 'view-upnext-phone');
await clickText(p, '.view-head .btn', 'Reorder'); await wait(700);
await shot(p, 'reorder-phone');

await go(p, '#/library?q=outer+wilds');
await p.click('.card'); await wait(700);
await shot(p, 'game-phone');
await clickText(p, '.drawer-actions .btn', 'Edit'); await wait(700);
await shot(p, 'edit-phone');
await clickText(p, '.editbar .btn', 'Cancel'); await wait(400);
await clickText(p, '.drawer-actions .btn', 'Change cover'); await wait(500);
await clickText(p, '.sheet.open .mode button', 'IGDB'); await wait(4500);
await shot(p, 'cover-picker-phone');

await go(p, '');
await p.click('.fab'); await wait(500);
await p.type('.sheet.open .pick-search input', 'Stray');
await p.waitForSelector('.cand', { timeout: 30000 }); await wait(1500);
await shot(p, 'add-search-phone');
// pick the first match that is not already in the library, so the form has no "already in your library" banner
await p.$$eval('.cand', (c) => (c.find((x) => !x.querySelector('.inlib')) || c[0]).click()); await wait(5000);
await shot(p, 'add-form-phone');
await p.close();

// ---- desktop ----
p = await page(1280, 800, false);
await go(p, '#/library?sort=status:asc,last_year:desc,title:asc');
await shot(p, 'library-desktop');
await go(p, '#/library?layout=table&sort=title:asc');
await shot(p, 'table-desktop');

await browser.close();
console.log('screenshots written to', OUT);
