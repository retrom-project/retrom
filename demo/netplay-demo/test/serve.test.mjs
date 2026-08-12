import assert from "node:assert/strict";
import net from "node:net";
import test from "node:test";
import { startServer } from "../tools/serve.mjs";

test("server defaults do not overlap Retrom application ports", async (context) => {
  const { server, origin } = await startServer({ port: 0 });
  context.after(() => new Promise((resolve) => server.close(resolve)));
  const port = Number(new URL(origin).port);
  assert.notEqual(port, 3000);
  assert.notEqual(port, 8080);
});

test("server rejects main Retrom application ports", () => {
  assert.throws(() => startServer({ port: 3000 }), /reserved/);
  assert.throws(() => startServer({ port: 8080 }), /reserved/);
});

test("WebSocket upgrade rejects a cross-origin handshake", async (context) => {
  const { server, origin } = await startServer({ port: 0 });
  context.after(() => new Promise((resolve) => server.close(resolve)));
  const { hostname, port } = new URL(origin);
  const response = await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: hostname, port: Number(port) });
    let received = "";
    socket.setEncoding("utf8");
    socket.on("connect", () => socket.write([
      "GET /netplay?room=test&slot=0 HTTP/1.1",
      `Host: ${hostname}:${port}`,
      "Origin: http://attacker.invalid",
      "Upgrade: websocket",
      "Connection: Upgrade",
      "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==",
      "Sec-WebSocket-Version: 13",
      "\r\n"
    ].join("\r\n")));
    socket.on("data", (chunk) => { received += chunk; });
    socket.on("end", () => resolve(received));
    socket.on("error", reject);
  });
  assert.match(response, /^HTTP\/1\.1 404 Not Found/);
});
