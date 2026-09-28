self.addEventListener("install", event => {event.waitUntil(self.skipWaiting());});
self.addEventListener("activate", event => {event.waitUntil(self.clients.claim());});
self.addEventListener("message", event => {
  if (event.data?.type !== "CONNECT" || event.ports.length !== 2 || !event.source?.url) return;
  const source = new URL(event.source.url);
  if (source.origin !== self.location.origin || source.pathname !== configuration.bootstrap) return;
  bindConnection(event.ports[0]);
  event.ports[1].postMessage({type: "CONNECTED"}); event.ports[1].close();
});
self.addEventListener("fetch", event => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;
  const path = projectPath(url);
  if (path === null) return;
  event.respondWith(serveContent(event.request, path).catch(() => new Response(null, {status: 502})));
});
