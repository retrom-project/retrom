import { createServer, request as requestHttp } from "node:http";
import { connect as connectTcp } from "node:net";

export async function localRpgAcceptanceProxy(origin, productionWebOrigin = process.env.RETROM_ACCEPTANCE_PRODUCTION_WEB_ORIGIN) {
  const parsed = new URL(origin);
  const web = productionWebOrigin ? new URL(productionWebOrigin) : null;
  if (web && (web.protocol !== "http:" || web.hostname !== "127.0.0.1" || !web.port ||
      web.pathname !== "/" || web.search || web.hash || web.username || web.password)) {
    throw new Error("RPG_ACCEPTANCE_PRODUCTION_WEB_ORIGIN_INVALID");
  }
  if (parsed.protocol !== "http:" || !isRpgLocalhost(parsed.hostname)) {
    return { contextOptions: {}, close: async () => {} };
  }
  const sockets = new Set();
  const server = createServer((request, response) => proxyHttpRequest(request, response, parsed, web));
  server.on("connection", (socket) => trackSocket(sockets, socket));
  server.on("connect", (request, socket, head) =>
    proxyTunnel(request, socket, head, sockets, web ? server.address().port : null));
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") { throw new Error("RPG_ACCEPTANCE_LOCAL_PROXY_ADDRESS"); }
  return {
    contextOptions: { proxy: { server: `http://127.0.0.1:${address.port}` } },
    close: async () => {
      for (const socket of sockets) { socket.destroy(); }
      await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    },
  };
}

function proxyHttpRequest(request, response, origin, web) {
  const target = parseTarget(request.url, request.headers.host);
  if (!target) { response.writeHead(403).end(); return; }
  const webPage = web && target.host === origin.host && !/^\/(?:api|health|content|runtime)(?:\/|$)/u.test(target.pathname);
  const upstream = requestHttp({
    hostname: "127.0.0.1", port: webPage ? Number(web.port) : target.port, method: request.method,
    path: `${target.pathname}${target.search}`, headers: { ...request.headers, host: target.host },
  }, (upstreamResponse) => {
    response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
    upstreamResponse.pipe(response);
  });
  upstream.on("error", () => {
    if (!response.headersSent) { response.writeHead(502); }
    response.end();
  });
  request.pipe(upstream);
}

function proxyTunnel(request, socket, head, sockets, proxyPort) {
  const target = parseTarget(`http://${request.url}`);
  if (!target) { socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); return; }
  const upstream = connectTcp(proxyPort ?? target.port, "127.0.0.1", () => {
    socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
    if (head.length) { upstream.write(head); }
    socket.pipe(upstream);
    upstream.pipe(socket);
  });
  trackSocket(sockets, upstream);
  socket.once("close", () => upstream.destroy());
  upstream.on("error", () => socket.destroy());
}

function parseTarget(value, host) {
  let target;
  try { target = new URL(value, host ? `http://${host}` : undefined); } catch { return null; }
  const port = Number(target.port || 80);
  if (target.protocol !== "http:" || !isRpgLocalhost(target.hostname) ||
      !Number.isInteger(port) || port < 1 || port > 65_535) {
    return null;
  }
  return {
    host: target.host, pathname: target.pathname, port, protocol: target.protocol,
    search: target.search,
  };
}

function isRpgLocalhost(hostname) {
  return hostname === "rpg.localhost" || hostname.endsWith(".rpg.localhost") ||
    /^(?:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.rpg\.)?[a-z0-9][a-z0-9-]*-[0-9a-f]{12}\.localhost$/u.test(hostname);
}

function trackSocket(sockets, socket) {
  sockets.add(socket);
  socket.on("error", () => socket.destroy());
  socket.once("close", () => sockets.delete(socket));
}
