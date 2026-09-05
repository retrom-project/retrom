import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";

const driver = readFileSync(new URL("./kirikiri_product.mjs", import.meta.url), "utf8");
function driverFunction(name) {
  const source = driver.match(new RegExp(`async function ${name}\\([\\s\\S]*?\\n\\}`, "u"))?.[0];
  assert.ok(source, `${name} must remain a testable boundary`);
  return runInNewContext(`(${source})`);
}

test("immersive Kiri launch uses the ordinary immersive library return contract", async () => {
  const calls = [];
  const client = {writeHeaders: () => ({}), json: async (...args) => {calls.push(args);}};
  const createLaunch = driverFunction("createLaunch");
  await createLaunch(client, "game-id", null, "immersive");
  await createLaunch(client, "game-id", null);
  assert.equal(calls[0][2].data.returnTo, "/immersive/library/all?gameId=game-id");
  assert.equal(calls[1][2].data.returnTo, "/games/game-id");
  assert.match(driver, /const immersive = await createLaunch\(client, approved.gameId, null, "immersive"\)/u);
});

test("Kiri controller buttons reach both Host immersive controls and core realms", async () => {
  const received = [[], []];
  const realms = received.map((log) => ({
    __retromTestGamepad: {button: (index, pressed) => log.push([index, pressed])},
  }));
  const frames = realms.map((realm) => ({evaluate: async (callback, input) => {
    runInNewContext(`(${callback.toString()})(input)`, {...realm, input});
  }}));
  const canvas = {
    page: () => ({frames: () => frames}),
    evaluate: async (callback, input) => callback({ownerDocument: {defaultView: realms[1]}}, input),
  };
  await driverFunction("setVirtualGamepadButton")(canvas, 8, true);
  assert.deepEqual(received, [[[8, true]], [[8, true]]]);
});
