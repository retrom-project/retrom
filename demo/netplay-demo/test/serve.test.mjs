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

test("same-origin room API issues opaque participant credentials without exposing them in status", async (context) => {
  const { server, origin } = await startServer({ port: 0 });
  context.after(() => new Promise((resolve) => server.close(resolve)));
  const profileDigest = "e".repeat(64);
  const createdResponse = await fetch(`${origin}/api/rooms`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: origin },
    body: JSON.stringify({ profileDigest })
  });
  assert.equal(createdResponse.status, 201);
  const room = await createdResponse.json();
  assert.equal(room.participants.length, 2);
  assert.ok(room.participants.every((participant) => participant.joinToken.length >= 32));

  const status = await (await fetch(`${origin}/api/rooms/${room.roomId}`)).json();
  assert.equal(status.profileDigest, profileDigest);
  assert.equal("participants" in status, false);
  assert.equal(JSON.stringify(status).includes(room.participants[0].joinToken), false);

  const rejected = await fetch(`${origin}/api/rooms`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: "http://attacker.invalid" },
    body: JSON.stringify({ profileDigest })
  });
  assert.equal(rejected.status, 403);
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
