"use strict";
// APP_VERSION and CACHE_STATIC are supplied by the /sw.js route.
const prefix = "retrom-pwa-";
const shellCache = `${prefix}shell-${APP_VERSION}`;
const staticCache = `${prefix}static-${APP_VERSION}`;
const shellPaths = ["/offline", "/pwa/icons/icon-192.png", "/pwa/icons/icon-512.png", "/pwa/icons/maskable-512.png", "/pwa/icons/apple-180.png"];
const excluded = /^\/(?:api|health|content|runtime|__retrom|run)(?:\/|$)/u;

self.addEventListener("install", event => {
  // Updates wait for consent or for all existing windows to close.
  event.waitUntil(installShell());
});
async function installShell() {
  const responses = await Promise.all(shellPaths.map(async path => {
    const response = await fetch(new Request(new URL(path, self.location.origin), { cache: "reload", credentials: "omit" }));
    if (!response.ok || response.redirected) { throw new Error("PWA_SHELL_UNAVAILABLE"); }
    return response;
  }));
  const cache = await caches.open(shellCache);
  await Promise.all(shellPaths.map((path, index) => cache.put(path, responses[index])));
}
self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    // Retain the preceding revision for lazy chunks in other open windows.
    const keys = await caches.keys();
    for (const kind of ["shell", "static"]) {
      const owned = keys.filter(key => key.startsWith(`${prefix}${kind}-`));
      const current = kind === "shell" ? shellCache : staticCache;
      const previous = owned.filter(key => key !== current).at(-1);
      await Promise.all(owned.filter(key => key !== current && key !== previous).map(key => caches.delete(key)));
    }
    await self.clients.claim();
  })());
});
self.addEventListener("message", event => {
  if (event.data?.type !== "RETROM_ACTIVATE_UPDATE" || !event.source?.url || event.ports.length !== 1) { return; }
  if (new URL(event.source.url).origin !== self.location.origin) { return; }
  event.waitUntil((async () => {
    const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const playing = clients.some(client => /^\/(?:play|immersive)(?:\/|$)/u.test(new URL(client.url).pathname));
    if (playing) {
      event.ports[0].postMessage({ status: "playing" });
      return;
    }
    await self.skipWaiting();
    event.ports[0].postMessage({ status: "activating" });
  })());
});
self.addEventListener("fetch", event => {
  const request = event.request;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || request.method !== "GET" || request.headers.has("Range") || excluded.test(url.pathname)) { return; }
  if (request.mode === "navigate" && request.destination !== "iframe") {
    event.respondWith(navigate(request));
  } else if (shellPaths.includes(url.pathname) && !url.search) {
    event.respondWith(shellResource(request));
  } else if (CACHE_STATIC && url.pathname.startsWith("/_next/static/")) {
    event.respondWith(staticResource(request, event));
  }
});
async function navigate(request) {
  try {
    const response = await fetch(request);
    if (response.status < 500) { return response; }
  } catch { /* A failed document load uses the public, self-contained fallback. */ }
  const cache = await caches.open(shellCache);
  return await cache.match("/offline") ?? Response.error();
}
async function shellResource(request) {
  try {
    const cache = await caches.open(shellCache);
    const response = await cache.match(request);
    if (response) { return response; }
  } catch { /* Storage can be revoked while an application is open. */ }
  return fetch(request);
}
async function staticResource(request, event) {
  const cached = await findStatic(request);
  if (cached) { return cached; }
  const response = await fetch(request);
  if (response.ok && !response.redirected && response.headers.get("Cache-Control")?.includes("immutable")) {
    const copy = response.clone();
    event.waitUntil((async () => {
      try {
        const cache = await caches.open(staticCache);
        await cache.put(request, copy);
        const keys = await cache.keys();
        await Promise.all(keys.slice(0, Math.max(0, keys.length - 192)).map(key => cache.delete(key)));
      } catch { /* Optional static caching must not interrupt online use. */ }
    })());
  }
  return response;
}
async function findStatic(request) {
  try {
    const keys = await caches.keys();
    for (const key of keys.filter(key => key.startsWith(`${prefix}static-`)).reverse()) {
      const cache = await caches.open(key);
      const response = await cache.match(request);
      if (response) { return response; }
    }
  } catch { /* Persistent storage is optional for online static resources. */ }
  return undefined;
}
