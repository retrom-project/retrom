import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {createHash} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {observePC98, openPC98, visiblePC98Menu, pressPC98, pausePC98, savePC98} from "./pc98_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/checkpoint-storage-product");
mkdirSync(directory, {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-SAVE-004", status: "FAIL", errors: [], runtimes: [], diskRequests: 0,
  disk: {sha256: "3bc33e01942b253cef0e19ab2ce6cf4c27befe49a0f4c1e5a021711480c100da", sizeBytes: 262168576}};
let browser, proxy;
try {
  if (![base, env.RETROM_PC98_GAME_ID, env.RETROM_PC98_LEGACY_SAVE_ID, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("CHECKPOINT_STORAGE_INPUT_REQUIRED");
  }
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true, args: ["--autoplay-policy=no-user-gesture-required"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observePC98(context);
  const client = await fantasyClient(context, base);
  const legacy = await launchCart(client, env.RETROM_PC98_GAME_ID, env.RETROM_PC98_LEGACY_SAVE_ID);
  const first = await openPC98(context, base, legacy, evidence);
  assert.equal(first.config.restore.format, "np2kai-state-v1");
  const before = await visiblePC98Menu(first, directory, "legacy-restored");
  await pressPC98(first, 12);
  const moved = await visiblePC98Menu(first, directory, "before-save");
  assert.notEqual(moved.menuSha256, before.menuSha256, "LEGACY_RESTORE_INPUT_FAILED");
  await pausePC98(first);
  const saved = await savePC98(first, client, legacy.launchId, env.RETROM_PC98_GAME_ID, directory);
  await first.page.close();
  const next = await launchCart(client, env.RETROM_PC98_GAME_ID, saved.saveStateId);
  const resumed = await openPC98(context, base, next, evidence);
  assert.notEqual(next.launchId, legacy.launchId);
  assert.equal(resumed.config.restore.format, "np2kai-state-v1-storage-v1");
  const response = await client.raw("GET", resumed.config.restore.url);
  assert.equal(response.status(), 200);
  const stored = await response.body();
  assert.equal(stored.length, saved.sizeBytes);
  assert.equal(createHash("sha256").update(stored).digest("hex"), resumed.config.restore.sha256);
  const native = gunzipSync(stored, {maxOutputLength: 402653184});
  assert.equal(native.subarray(0, 8).toString("ascii"), "NP2STATE", "SAVE_DOUBLE_COMPRESSED_OR_NATIVE_FORMAT_CHANGED");
  assert.ok(stored.length < native.length / 2, "PC98_COMPRESSION_DID_NOT_REDUCE_STORAGE");
  const restored = await visiblePC98Menu(resumed, directory, "compressed-restored");
  assert.equal(restored.menuSha256, moved.menuSha256, "COMPRESSED_EXECUTION_STATE_NOT_RESTORED");
  await pressPC98(resumed, 12);
  assert.notEqual((await visiblePC98Menu(resumed, directory, "restored-input")).menuSha256, moved.menuSha256);
  assert.deepEqual(evidence.errors, []);
  Object.assign(evidence, {status: "PASS", gameId: env.RETROM_PC98_GAME_ID,
    legacySaveId: env.RETROM_PC98_LEGACY_SAVE_ID, saveStateId: saved.saveStateId,
    originalLaunchId: legacy.launchId, restoredLaunchId: next.launchId,
    storedBytes: stored.length, nativeBytes: native.length, reductionPercent: (1 - stored.length / native.length) * 100,
    menu: {before, moved, restored}});
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "checkpoint-storage-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
