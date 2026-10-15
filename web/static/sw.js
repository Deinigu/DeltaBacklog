// App shell cache + runtime caching. Bump VERSION when shell files change.
const VERSION = 'v38';

const SHELL = `shell-${VERSION}`;
const COVERS = 'covers-v2'; // keyed by path (one entry per cover file); the old 'covers' cache was keyed by URL with ?v=
const THUMBS = 'screenshot-thumbs';
const MAX_THUMBS = 400; // thumbnails are ~30 KB each; originals are never cached
const API = 'api';
const SHELL_FILES = ['/', '/styles.css', '/js/main.js', '/js/api.js', '/js/util.js', '/js/library.js', '/js/drawer.js', '/js/sort.js', '/js/filters.js', '/js/ttb.js', '/js/views.js', '/js/order.js', '/js/reorder.js', '/js/table.js', '/js/picker.js', '/js/csv.js', '/js/add.js', '/js/platforms.js', '/js/ownership.js', '/js/form.js', '/js/editpatch.js', '/js/years.js', '/js/yearreview.js', '/js/yearpage.js', '/js/tierpng.js', '/js/rankboard.js', '/js/rankdrag.js', '/js/rankingedit.js', '/js/rankingspage.js', '/js/gamepicker.js', '/js/savequeue.js', '/js/more.js', '/js/colors.js', '/js/pick.js', '/js/surprise.js', '/js/series.js', '/js/seriesview.js', '/js/stats.js', '/js/statspage.js', '/js/link.js', '/js/linkqueue.js', '/js/shots.js', '/js/shotsview.js', '/icon.svg'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(SHELL).then((c) => c.addAll(SHELL_FILES)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => (k.startsWith('shell-') && k !== SHELL) || k === 'covers').map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== location.origin) return;

  if (url.pathname.startsWith('/screenshots/')) {
    // Only the small thumbnails (<hex>_t.jpg) are cached; an original goes straight to the network.
    if (/_t\.jpg$/.test(url.pathname)) e.respondWith(cacheFirst(req, THUMBS, MAX_THUMBS));
  } else if (url.pathname.startsWith('/covers/')) {
    e.respondWith(coverFirst(req));
  } else if (url.pathname.startsWith('/api/')) {
    e.respondWith(networkFirst(req, API));
  } else {
    // Network first: new deploys show up immediately; the cache only matters when offline.
    e.respondWith(networkFirst(req, SHELL));
  }
});

async function cacheFirst(req, name, max = 0) {
  const cache = await caches.open(name);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok) {
    cache.put(req, res.clone());
    if (max) trim(cache, max);
  }
  return res;
}

// A cover is /covers/<file>?v=<version>. One cache entry per file (keyed by path, so a new version replaces the old
// one without scanning the cache); the entry is used only when its own URL carries the requested version.
async function coverFirst(req) {
  const cache = await caches.open(COVERS);
  const key = new URL(req.url).pathname;
  const hit = await cache.match(key);
  if (hit && hit.url === req.url) return hit;
  try {
    const res = await fetch(req);
    if (res.ok) await cache.put(key, res.clone());
    return res;
  } catch (err) {
    if (hit) return hit; // offline: an older cover beats a broken image
    throw err;
  }
}

// Keep a cache to its newest `max` entries (keys come back oldest first).
async function trim(cache, max) {
  const keys = await cache.keys();
  for (const k of keys.slice(0, Math.max(0, keys.length - max))) await cache.delete(k);
}

async function networkFirst(req, name) {
  const cache = await caches.open(name);
  try {
    const res = await fetch(req);
    if (res.ok) cache.put(req, res.clone());
    return res;
  } catch (err) {
    const hit = await cache.match(req);
    if (hit) return hit;
    throw err;
  }
}

