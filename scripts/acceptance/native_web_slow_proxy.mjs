import {createServer, request as requestHttp} from "node:http";
import {connect} from "node:net";
import {setTimeout as delay} from "node:timers/promises";

/** Throttle real content streams, including Worker requests; preserve development HMR. */
export async function nativeWebSlowProxy(base, bytesPerSecond = 524288, latencyMs = 200) {
  const origin = new URL(base), sockets = new Set(), requests = [];
  if (origin.protocol !== "http:" || !origin.hostname.endsWith(".localhost")) throw Error("NATIVE_SLOW_LOCAL_PFB_REQUIRED");
  let nextSlot = 0;
  const target = (value, host = origin.host) => {
    try {
      const url = new URL(value, `http://${host}`);
      return url.protocol === "http:" && url.port === origin.port &&
        (url.hostname === origin.hostname || url.hostname.endsWith(`.rpg.${origin.hostname}`)) ? url : null;
    } catch {return null;}
  };
  const track = socket => {
    sockets.add(socket); socket.on("error", () => socket.destroy());
    socket.once("close", () => sockets.delete(socket));
  };
  const server = createServer((request, response) => {
    const url = target(request.url, request.headers.host);
    if (!url) {response.writeHead(403).end(); return;}
    const limited = url.pathname.startsWith("/runtime/content/web/");
    const row = {path: url.pathname, bytes: 0, startedMs: performance.now()};
    if (limited) requests.push(row);
    const upstream = requestHttp({hostname: "127.0.0.1", port: url.port, method: request.method,
      path: url.pathname + url.search, headers: {...request.headers, host: url.host}}, incoming => {
      if (!limited) {response.writeHead(incoming.statusCode, incoming.headers); incoming.pipe(response); return;}
      void forward(incoming, response, row).catch(() => response.destroy());
    });
    response.once("close", () => upstream.destroy()); upstream.on("error", () => response.destroy());
    request.pipe(upstream);
  });
  async function forward(incoming, response, row) {
    await delay(latencyMs);
    row.status = incoming.statusCode;
    response.writeHead(incoming.statusCode, incoming.headers); response.flushHeaders();
    try {
      for await (const chunk of incoming) {
        nextSlot = Math.max(nextSlot, performance.now()) + chunk.length / bytesPerSecond * 1000;
        await delay(Math.max(0, nextSlot - performance.now()));
        if (response.destroyed) break;
        row.bytes += chunk.length; response.write(chunk);
      }
      row.complete = incoming.complete && !response.destroyed;
      response.end();
    } finally {row.elapsedMs = performance.now() - row.startedMs;}
  }
  server.on("connection", track);
  server.on("connect", (request, socket, head) => {
    if (!target(`http://${request.url}`)) {socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); return;}
    // APIRequestContext also tunnels HTTP; parse its inner requests through this server.
    const upstream = connect(server.address().port, "127.0.0.1", () => {
      socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) upstream.write(head);
      socket.pipe(upstream); upstream.pipe(socket);
    });
    track(upstream); socket.once("close", () => upstream.destroy());
  });
  server.on("upgrade", (request, socket, head) => {
    const url = target(request.url, request.headers.host);
    if (!url) {socket.destroy(); return;}
    const upstream = connect(Number(url.port), "127.0.0.1", () => {
      const headers = [];
      for (let n = 0; n < request.rawHeaders.length; n += 2) headers.push(`${request.rawHeaders[n]}: ${request.rawHeaders[n + 1]}`);
      upstream.write(`${request.method} ${url.pathname + url.search} HTTP/1.1\r\n${headers.join("\r\n")}\r\n\r\n`);
      if (head.length) upstream.write(head);
      socket.pipe(upstream); upstream.pipe(socket);
    });
    track(upstream); socket.once("close", () => upstream.destroy());
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject); server.listen(0, "127.0.0.1", resolve);
  });
  return {requests, bytesPerSecond, latencyMs,
    contextOptions: {proxy: {server: `http://127.0.0.1:${server.address().port}`}},
    async close() {for (const socket of sockets) socket.destroy(); await new Promise(resolve => server.close(resolve));}};
}
