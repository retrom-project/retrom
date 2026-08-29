import assert from "node:assert/strict";
import { createServer, request as requestHttp } from "node:http";
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
  const proxy = await localRpgAcceptanceProxy("https://dev.sendev.cc");
  assert.deepEqual(proxy.contextOptions, {});
  await proxy.close();
});

function listen(server) {
  return new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
}

function close(server) {
  return new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
}

function proxyGet(proxyUrl, targetPort) {
  return new Promise((resolve, reject) => {
    const request = requestHttp({
      hostname: proxyUrl.hostname, port: proxyUrl.port,
      path: `http://retrom-app.rpg.localhost:${targetPort}/`,
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
