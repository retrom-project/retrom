import { createServer, request as requestHttp } from "node:http";
import { connect as connectTcp } from "node:net";

export async function localRpgAcceptanceProxy(origin) {
  const parsed = new URL(origin);
  if (parsed.protocol !== "http:" || !isRpgLocalhost(parsed.hostname)) {
    return { contextOptions: {}, close: async () => {} };
  }
  const sockets = new Set();
  const server = createServer((request, response) => proxyHttpRequest(request, response));
  server.on("connection", (socket) => trackSocket(sockets, socket));
  server.on("connect", (request, socket, head) => proxyTunnel(request, socket, head, sockets));
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

function proxyHttpRequest(request, response) {
  const target = parseTarget(request.url);
  if (!target) { response.writeHead(403).end(); return; }
  const upstream = requestHttp({
    hostname: "127.0.0.1", port: target.port, method: request.method,
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

function proxyTunnel(request, socket, head, sockets) {
  const target = parseTarget(`http://${request.url}`);
  if (!target) { socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); return; }
  const upstream = connectTcp(target.port, "127.0.0.1", () => {
    socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
    if (head.length) { upstream.write(head); }
    socket.pipe(upstream);
    upstream.pipe(socket);
  });
  trackSocket(sockets, upstream);
  upstream.on("error", () => socket.destroy());
}

function parseTarget(value) {
  let target;
  try { target = new URL(value); } catch { return null; }
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
  return hostname === "rpg.localhost" || hostname.endsWith(".rpg.localhost");
}

function trackSocket(sockets, socket) {
  sockets.add(socket);
  socket.once("close", () => sockets.delete(socket));
}
