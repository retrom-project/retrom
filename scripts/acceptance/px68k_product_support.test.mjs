import assert from "node:assert/strict";
import {createServer, request} from "node:http";
import {test} from "node:test";
import {px68kLocalProxy} from "./px68k_product_support.mjs";

test("PX68K browser and API proxy support CONNECT with an exact origin boundary", async () => {
  const upstream = createServer((_request, response) => response.end("ready"));
  await new Promise(resolve => upstream.listen(0, "127.0.0.1", resolve));
  const origin = `px68k-test.localhost:${upstream.address().port}`;
  const proxy = await px68kLocalProxy(`http://${origin}`);
  const target = new URL(proxy.contextOptions.proxy.server);
  async function tunnel(authority) {
    return new Promise((resolve, reject) => {
      const connecting = request({host: target.hostname, port: target.port, method: "CONNECT", path: authority});
      connecting.on("connect", (response, socket) => {socket.destroy(); resolve(response.statusCode);});
      connecting.on("error", reject); connecting.end();
    });
  }
  try {
    assert.equal(await tunnel(origin), 200);
    assert.equal(await tunnel("other.localhost:80"), 403);
    assert.equal(await tunnel("example.com:443"), 403);
  } finally {
    await proxy.close(); await new Promise(resolve => upstream.close(resolve));
  }
});
