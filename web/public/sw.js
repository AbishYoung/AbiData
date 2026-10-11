/* AbiData service worker (hand-written; the build fills in VERSION and PRECACHE, see
   pwaServiceWorker in vite.config.ts).

   Strategy
   - install: precache the app shell (index.html, entry JS/CSS, icons, offline.html) into a
     versioned cache. It does NOT skipWaiting; the page asks (postMessage SKIP_WAITING) so an
     update never swaps code under a session mid-edit.
   - /assets/* (content-hashed): cache-first.
   - navigations: network-first; offline falls back to the cached index.html (the SPA shell,
     whose data calls then fail visibly), then offline.html.
   - Never touched: non-GET requests, cross-origin requests, /api/*, /metrics, /healthz,
     /readyz, and anything asking for an event stream. Those always hit the network.
   - activate: delete every other abidata-* cache, then claim clients. */
const VERSION = "__ABI_VERSION__";
const PRECACHE = ["__ABI_PRECACHE__"];
const CACHE = "abidata-shell-" + VERSION;
const NEVER = ["/api/", "/metrics", "/healthz", "/readyz"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) =>
      Promise.all(
        PRECACHE.map((url) =>
          fetch(new Request(url, { cache: "reload" })).then((res) => {
            if (!res.ok) throw new Error("precache " + url + ": " + res.status);
            return cache.put(url, res);
          }),
        ),
      ),
    ),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith("abidata-") && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("message", (event) => {
  if (event.data && event.data.type === "SKIP_WAITING") self.skipWaiting();
});

function bypass(request, url) {
  if (request.method !== "GET") return true;
  if (url.origin !== self.location.origin) return true;
  if (NEVER.some((p) => url.pathname === p || url.pathname.startsWith(p))) return true;
  if ((request.headers.get("accept") || "").includes("text/event-stream")) return true;
  return false;
}

self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (bypass(request, url)) return;

  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(async () => {
        const cache = await caches.open(CACHE);
        return (await cache.match("/index.html")) || (await cache.match("/offline.html")) || Response.error();
      }),
    );
    return;
  }

  if (url.pathname.startsWith("/assets/") || PRECACHE.includes(url.pathname)) {
    event.respondWith(
      caches.open(CACHE).then(async (cache) => {
        const hit = await cache.match(request);
        if (hit) return hit;
        const res = await fetch(request);
        if (res.ok && url.pathname.startsWith("/assets/")) cache.put(request, res.clone());
        return res;
      }),
    );
  }
});
