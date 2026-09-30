import assert from "node:assert/strict";
import {EventEmitter} from "node:events";
import test from "node:test";
import {observeSymbianAssets} from "./symbian_assets.mjs";

const root = "http://example.test/runtime/providers/retrom-runtime/bundle/";
test("Worker evidence uses its executing script without reading an unavailable network response body", async () => {
  const context = new EventEmitter(), errors = [], observer = observeSymbianAssets(context, errors);
  context.emit("response", {url: () => root + "assets/content-io/worker.mjs", body() {throw new Error("Worker body unavailable");}});
  await observer.flush();
  assert.equal(observer.workerAssetPath, "/runtime/providers/retrom-runtime/bundle/assets/content-io/worker.mjs");
  assert.deepEqual(observer.assets, []);
  assert.deepEqual(errors, []);
  observer.dispose();assert.equal(context.listenerCount("response"), 0);
});
test("client and core payload failures remain acceptance errors", async () => {
  const context = new EventEmitter(), errors = [], observer = observeSymbianAssets(context, errors);
  context.emit("response", {url: () => root + "client.mjs", body: async () => Buffer.from("client")});
  context.emit("response", {url: () => root + "assets/eka2l1/eka2l1-runtime.zip", body: async () => {throw Error("missing");}});
  await observer.flush();
  assert.equal(observer.assets.length, 1);assert.equal(observer.assets[0].sizeBytes, 6);
  assert.equal(errors.length, 1);assert.match(errors[0], /SYMBIAN_PROVIDER_ASSET_UNAVAILABLE/u);
  observer.dispose();
});
