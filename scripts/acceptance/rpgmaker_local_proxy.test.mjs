import assert from "node:assert/strict";
import { createServer, request as requestHttp } from "node:http";
import { connect } from "node:net";
import test from "node:test";

import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";

test("local acceptance proxy resolves the reserved RPG site for HTTP and CONNECT", async () => {
  const target = createServer((request, response) => response.end(request.headers.host));
  await listen(target);
  const targetPort = target.address().port;
  const proxy = await localRpgAcceptanceProxy(`http://retrom-app.rpg.localhost:${targetPort}`);
  const proxyUrl = new URL(proxy.contextOptions.proxy.server);
  try {
    assert.equal(await proxyGet(proxyUrl, targetPort), `retrom-app.rpg.localhost:${targetPort}`);
    assert.match(await proxyConnect(proxyUrl, targetPort), /200 Connection Established/);
  } finally {
    await proxy.close();
    await close(target);
  }
});

test("non-local acceptance origins do not install a proxy", async () => {
  const proxy = await localRpgAcceptanceProxy("https://retrom.example");
  assert.deepEqual(proxy.contextOptions, {});
  await proxy.close();
});

test("resetting a browser tunnel does not crash the acceptance proxy", async () => {
  const target = createServer((request, response) => response.end("alive"));
  await listen(target);
  const port = target.address().port;
  const proxy = await localRpgAcceptanceProxy(`http://rpg.localhost:${port}`);
  const url = new URL(proxy.contextOptions.proxy.server);
  try {
    await new Promise((resolve, reject) => {
      const socket = connect(Number(url.port), url.hostname, () => {
        socket.write(`CONNECT rpg.localhost:${port} HTTP/1.1\r\nHost: rpg.localhost:${port}\r\n\r\n`);
      });
      socket.once("error", reject);
      socket.once("data", () => {socket.resetAndDestroy(); resolve();});
    });
    await new Promise(resolve => setTimeout(resolve, 100));
    assert.equal(await proxyGet(url, port), "alive");
  } finally {await proxy.close(); await close(target);}
});

function listen(server) {
  return new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
}

function close(server) {
  return new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
}

function proxyGet(proxyUrl, targetPort, host = "retrom-app.rpg.localhost") {
  return new Promise((resolve, reject) => {
    const request = requestHttp({
      hostname: proxyUrl.hostname, port: proxyUrl.port,
      path: `http://${host}:${targetPort}/`,
    }, (response) => {
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    });
    request.on("error", reject).end();
  });
}

function proxyConnect(proxyUrl, targetPort) {
  return new Promise((resolve, reject) => {
    const request = requestHttp({
      hostname: proxyUrl.hostname, port: proxyUrl.port, method: "CONNECT",
      path: `runtime.rpg.localhost:${targetPort}`,
    });
    request.on("connect", (response, socket) => {
      socket.destroy();
      resolve(`HTTP/${response.httpVersion} ${response.statusCode} ${response.statusMessage}`);
    });
    request.on("error", reject).end();
  });
}


test("PFB acceptance resolves only the bounded PFB localhost family", async () => {
  const host = "pc98-012345abcdef.localhost";
  const target = createServer((request, response) => response.end(request.headers.host));
  await listen(target);
  const port = target.address().port;
  const proxy = await localRpgAcceptanceProxy(`http://${host}:${port}`);
  try {
    assert.ok(proxy.contextOptions.proxy);
    const url = new URL(proxy.contextOptions.proxy.server);
    assert.equal(await proxyGet(url, port, host), `${host}:${port}`);
    const runtime = `01234567-1234-7123-8123-0123456789ab.rpg.${host}`;
    assert.equal(await proxyGet(url, port, runtime), `${runtime}:${port}`);
    assert.equal(await proxyGet(url, port, "unrelated.localhost"), "");
  } finally {await proxy.close(); await close(target);}
});
