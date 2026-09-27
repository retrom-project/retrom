import assert from "node:assert/strict";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {webcrypto, createHash} from "node:crypto";
import {observeDOSStates} from "../dosbox_product_browser.mjs";

test("DOS state observation follows methods prepared by the Provider start callback", async () => {
  let initialization;
  await observeDOSStates({addInitScript: async script => {initialization = script;}});
  const bytes = Uint8Array.of(82, 65, 83, 84, 65, 84, 69, 1);
  class Manager {getState() {throw Error("upstream thread state unavailable");}}
  const manager = new Manager(), realm = {crypto: webcrypto, EJS_emulator: {gameManager: manager}};
  runInNewContext(`(${initialization})()`, realm);
  let restored;
  realm.EJS_onGameStart = () => {
    Manager.prototype.getState = () => bytes;
    Manager.prototype.loadExplicitStateAndWait = async value => {restored = value;};
    return manager.loadExplicitStateAndWait(bytes);
  };
  await realm.EJS_onGameStart();
  assert.equal(manager.getState(), bytes); assert.equal(restored, bytes);
  await Promise.all(realm.__dosStateObservation.pending);
  const expected = {sizeBytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex")};
  assert.deepEqual(JSON.parse(JSON.stringify(realm.__dosStateObservation.captures)), [expected]);
  assert.deepEqual(JSON.parse(JSON.stringify(realm.__dosStateObservation.restores)), [expected]);
});
