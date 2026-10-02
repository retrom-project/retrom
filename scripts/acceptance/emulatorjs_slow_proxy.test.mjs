import assert from "node:assert/strict";
import {createServer, request} from "node:http";
import test from "node:test";
import {emulatorjsSlowProxy} from "./emulatorjs_slow_proxy.mjs";

test("slow proxy preserves isolated Launch hosts and actual unknown-length core bytes", async () => {
  const bytes = Buffer.alloc(32768, 37), hosts = [];
  const server = createServer((req, res) => {hosts.push(req.headers.host); res.writeHead(200, {"Cache-Control": "immutable"}); res.end(bytes);});
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const port = server.address().port, host = "audit-start-012345abcdef.localhost";
  const launchHost = `01980000-0000-7000-8000-000000000001.rpg.${host}`;
  const proxy = await emulatorjsSlowProxy(`http://${host}:${port}`), address = new URL(proxy.contextOptions.proxy.server);
  const get = target => new Promise((resolve, reject) => {
    request({hostname: address.hostname, port: address.port, path: target}, response => {
      const chunks = []; response.on("data", chunk => chunks.push(chunk));
      response.on("end", () => resolve({status: response.statusCode, headers: response.headers, bytes: Buffer.concat(chunks)}));
    }).once("error", reject).end();
  });
  try {
    const config = await get(`http://${launchHost}:${port}/runtime/launches/example/config`);
    assert.equal(config.status, 200); assert.equal(hosts[0], `${launchHost}:${port}`);
    const result = await get(`http://${launchHost}:${port}/runtime/providers/emulatorjs/hash/core.data`);
    assert.deepEqual(result.bytes, bytes); assert.equal(result.headers["cache-control"], "immutable");
    assert.equal(proxy.requests[0].bytes, bytes.length); assert.ok(proxy.requests[0].elapsedMs >= 500);
    assert.equal(proxy.requests[0].complete, true);
    const denied = await get(`http://unrelated.localhost:${port}/`); assert.equal(denied.status, 403);
    proxy.block(true);
    const blocked = await get(`http://${host}:${port}/runtime/providers/emulatorjs/hash/core.data`);
    assert.equal(blocked.status, 503); assert.equal(proxy.requests.length, 2); assert.equal(proxy.requests[1].blocked, true);
  } finally {await proxy.close(); await new Promise(resolve => server.close(resolve));}
});
