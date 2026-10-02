import {createServer, request as requestHttp} from "node:http";
import {connect} from "node:net";
import {setTimeout as delay} from "node:timers/promises";

// Preserve actual immutable bytes and HTTP headers; only transport timing changes.
export async function emulatorjsSlowProxy(base) {
  const origin = new URL(base), sockets = new Set(), requests = [];
  if (origin.protocol !== "http:" || !origin.hostname.endsWith(".localhost")) {throw Error("EJS_SLOW_LOCAL_PFB_REQUIRED");}
  let stall = false, block = false;
  const target = (value, host) => {
    try {
      const url = new URL(value, `http://${host}`);
      const isolatedHost = url.hostname.endsWith(`.rpg.${origin.hostname}`)
        && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u.test(url.hostname.split(".rpg.")[0]);
      return url.protocol === "http:" && url.port === origin.port
        && (url.hostname === origin.hostname || isolatedHost) ? url : null;
    } catch {return null;}
  };
  const track = socket => {sockets.add(socket); socket.on("error", () => socket.destroy()); socket.once("close", () => sockets.delete(socket));};
  const server = createServer((request, response) => {
    const url = target(request.url, request.headers.host);
    if (!url) {response.writeHead(403).end(); return;}
    const limited = /^\/runtime\/providers\/emulatorjs\/.*\.data$/u.test(url.pathname);
    const row = {path: url.pathname, bytes: 0, startedMs: performance.now()};
    if (limited) {requests.push(row);}
    if (limited && block) {row.blocked = true; response.writeHead(503).end(); return;}
    const upstream = requestHttp({hostname: "127.0.0.1", port: origin.port, method: request.method,
      path: url.pathname + url.search, headers: {...request.headers, host: url.host}}, incoming => {
      if (!limited) {response.writeHead(incoming.statusCode, incoming.headers); incoming.pipe(response); return;}
      void forward(incoming, response, row).catch(() => response.destroy());
    });
    response.once("close", () => {row.closed = true; upstream.destroy();});
    upstream.on("error", () => response.destroy()); request.pipe(upstream);
  });
  async function forward(incoming, response, row) {
    await delay(300); response.writeHead(incoming.statusCode, incoming.headers); response.flushHeaders();
    try {
      for await (const chunk of incoming) {
        // Slice coalesced buffers so even unknown-length streams expose live progress.
        for (let offset = 0; offset < chunk.length; offset += 16384) {
          const part = chunk.subarray(offset, offset + 16384);
          if (stall && row.bytes > 0) {
            await new Promise(resolve => response.once("close", resolve)); return;
          }
          await delay(part.length / 131072 * 1000);
          if (response.destroyed) {return;}
          row.bytes += part.length; response.write(part);
        }
      }
      row.complete = incoming.complete; response.end();
    } finally {row.elapsedMs = performance.now() - row.startedMs;}
  }
  server.on("connection", track);
  server.on("connect", (request, socket, head) => {
    if (!target(`http://${request.url}`, origin.host)) {socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); return;}
    const upstream = connect(server.address().port, "127.0.0.1", () => {
      socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) {upstream.write(head);} socket.pipe(upstream); upstream.pipe(socket);
    });
    track(upstream); socket.once("close", () => upstream.destroy());
  });
  await new Promise((resolve, reject) => server.once("error", reject).listen(0, "127.0.0.1", resolve));
  return {requests, contextOptions: {proxy: {server: `http://127.0.0.1:${server.address().port}`}},
    stall(value) {stall = value;}, block(value) {block = value;},
    async close() {for (const socket of sockets) {socket.destroy();} await new Promise(resolve => server.close(resolve));}};
}
