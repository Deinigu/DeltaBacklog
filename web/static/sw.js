// App shell cache + runtime caching. Bump VERSION when shell files change.
const VERSION = 'v4';
const SHELL = `shell-${VERSION}`;
const COVERS = 'covers';
const API = 'api';
const SHELL_FILES = ['/', '/styles.css', '/js/main.js', '/js/api.js', '/js/util.js', '/js/library.js', '/js/drawer.js', '/js/sort.js', '/js/filters.js', '/js/views.js', '/icon.svg'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(SHELL).then((c) => c.addAll(SHELL_FILES)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('shell-') && k !== SHELL).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== location.origin) return;

  if (url.pathname.startsWith('/covers/')) {
    e.respondWith(cacheFirst(req, COVERS));
  } else if (url.pathname.startsWith('/api/')) {
    e.respondWith(networkFirst(req, API));
  } else {
    // Network first: new deploys show up immediately; the cache only matters when offline.
    e.respondWith(networkFirst(req, SHELL));
  }
});

async function cacheFirst(req, name) {
  const cache = await caches.open(name);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok) cache.put(req, res.clone());
  return res;
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

