import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {createContext, runInContext} from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../../internal/httpapi/web_content/bridge.js", import.meta.url), "utf8");
async function bootstrap(preloaded, available) {
  const ports = [], messages = [];
  let receive, complete;
  const result = new Promise(resolve => {complete = resolve;});
  const parent = {postMessage(data, origin, transferred) {
    messages.push({type: data.type, origin});
    if (data.type === "RETROM_WEB_CONTENT_CONNECT") {
      const port = transferred[0]; ports.push(port);
      port.postMessage({type: "READY", preloaded});
    }
    if (data.type === "RETROM_WEB_CONTENT_FAILED") complete({failed: data.code});
  }};
  const context = createContext({parent, MessageChannel, setTimeout, clearTimeout, isSecureContext: true,
    document: {currentScript: {dataset: {parent: "https://app.test", entry: "/__retrom/entry", worker: "/__retrom/content-worker.js"}}, body: {}},
    navigator: {serviceWorker: available ? {addEventListener() {}, register: async () => {throw new Error("STORAGE_DENIED");}} : undefined},
    location: {replace: path => complete({path})}, addEventListener: (_type, handler) => {receive = handler;}});
  runInContext(source, context);
  try {
    assert.deepEqual(messages, [{type: "RETROM_WEB_CONTENT_WAITING", origin: "https://app.test"}]);
    receive({source: {}, origin: "https://app.test", data: {type: "RETROM_WEB_CONTENT_START", v: 1}});
    assert.equal(messages.length, 1);
    receive({source: parent, origin: "https://app.test", data: {type: "RETROM_WEB_CONTENT_START", v: 1}});
    return await result;
  } finally {for (const port of ports) port.close();}
}
test("on-demand keeps playing when service workers are unavailable or storage rejects registration", async () => {
  for (const available of [false, true]) assert.deepEqual(await bootstrap(false, available), {path: "/__retrom/entry"});
});
test("preload never silently falls back to network when service worker setup fails", async () => {
  assert.equal((await bootstrap(true, false)).failed, "CONTENT_IO_STORAGE_UNAVAILABLE");
  assert.equal((await bootstrap(true, true)).failed, "STORAGE_DENIED");
});
