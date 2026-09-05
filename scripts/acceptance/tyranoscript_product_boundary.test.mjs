import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {requireLocalRuntimeSite} from "./rpgmaker_security_runtime.mjs";

const driver = readFileSync(new URL("./tyranoscript_product.mjs", import.meta.url), "utf8");
const baseUrl = "http://runtime-pro-6fd8dbd46b48.localhost:3000";
const origin = "http://01980000-0000-7000-8000-000000000001.rpg.runtime-pro-6fd8dbd46b48.localhost:3000";
const game = {kind: "ISOLATED_WEB", role: "game", origin, contentDigest: "a".repeat(64)};
function boundary(name) {
  const source = driver.match(new RegExp(`(?:async )?function ${name}\\([\\s\\S]*?\\n\\}`, "u"))?.[0];
  assert.ok(source);
  return runInNewContext(`(${source})`, {baseUrl, requireLocalRuntimeSite});
}

test("Tyrano evidence reads the isolated game resource from the current Provider envelope", () => {
  const requireSite = boundary("requireTyranoScriptRuntimeSite");
  assert.equal(requireSite({resources: [game]}).contentDigest, game.contentDigest);
  for (const config of [
    {}, {adapter: {uniqueOrigin: origin}, contentDigest: game.contentDigest},
    {resources: []}, {resources: [game, game]},
    {resources: [{...game, kind: "SEEKABLE_BLOB"}]},
    {resources: [{...game, contentDigest: undefined}]},
    {resources: [{...game, origin: "http://localhost:4210"}]},
  ]) {assert.throws(() => requireSite(config), /TYRANOSCRIPT_ACCEPTANCE_RUNTIME_ORIGIN_INVALID/u);}
  assert.match(driver, /const contentDigest = requireTyranoScriptRuntimeSite\(await configPromise\)\.contentDigest/u);
});

test("Tyrano checkpoint resume waits for the real UI acknowledgement without forced clicks", async () => {
  const calls = [];
  const button = {
    isVisible: async () => true,
    waitFor: async ({state}) => {calls.push(state);},
    click: async (options) => {assert.equal(options?.force, undefined); calls.push("click");},
  };
  const page = {getByRole: () => button};
  await boundary("resumeAfterCheckpoint")(page);
  assert.deepEqual(calls, ["visible", "click", "hidden"]);
  assert.match(driver, /import \{installVirtualStandardGamepad\} from "\.\/standard_gamepad.mjs"/u);
});
