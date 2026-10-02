import assert from "node:assert/strict";
import {execFileSync} from "node:child_process";
import {createHash, randomUUID} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {fantasyClient, approveCart, launchCart, runtimeCanvas, saveCart} from "./fantasy_product_client.mjs";
import {runCart} from "./wasm4_run_cart.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

const base = process.env.RETROM_ACCEPTANCE_BASE_URL;
assert.ok(base && process.env.RETROM_CHROME_EXECUTABLE && process.env.RETROM_ACCEPTANCE_USERNAME && process.env.RETROM_ACCEPTANCE_PASSWORD);
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? `.artifacts/content-replacement-${randomUUID()}`);
mkdirSync(directory, {recursive: true});
const evidence = {status: "FAIL", checks: [], errors: []};
const sha = bytes => createHash("sha256").update(bytes).digest("hex");
const proxy = await localRpgAcceptanceProxy(base);
let browser;
try {
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  const context = await browser.newContext({...proxy.contextOptions, viewport: {width: 1440, height: 900}});
  const client = await fantasyClient(context, base);
  const fixture = readFileSync(new URL("../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url));
  assert.equal(sha(fixture), "c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd");
  const original = runCart(fixture, randomUUID());
  const raw = write("controls.wasm", original);
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=wasm4&limit=100");
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "wasm4");
  assert.ok(instance);
  const uploadId = await client.upload(singleFile(raw), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  const review = await reviewForImport(client, imported.importJobId);
  const {gameId} = await approveCart(client, review.itemId);
  evidence.gameId = gameId;
  const first = await openGame(context, client, gameId, null, sha(original));
  await verifyDebugFocus(first.page);
  const saved = await saveCart(first.page, first.launch.launchId, "wasm4");
  evidence.saveStateId = saved.saveStateId;
  await closeGame(first, gameId);
  const snapshot = contentFacts(await client.json("GET", `/api/v1/admin/games/${gameId}`));
  const invalids = [
    [write("disguised.wasm", readFileSync(archive("disguised.zip", raw, "controls.wasm"))), "WASM4_CART_INVALID"],
    [write("wrong-platform.wasm", readFileSync(new URL("../../testdata/public-roms/nes-smoke/nes-smoke.nes", import.meta.url))), "WASM4_CART_INVALID"],
    [write("truncated.wasm", original.subarray(0, original.length - 1)), "WASM4_CART_INVALID"],
    [archive("wrong.zip", raw, "other.nes"), "NO_SUPPORTED_CONTENT"],
    [archive("same.zip", raw, "nested/controls.wasm"), "GAME_CONTENT_UNCHANGED"],
  ];
  for (const [filename, errorCode] of invalids) {
    const job = await replace(client, gameId, filename);
    assert.equal(job.state, "FAILED"); assert.equal(job.errorCode, errorCode);
    assert.deepEqual(contentFacts(await client.json("GET", `/api/v1/admin/games/${gameId}`)), snapshot);
    const restored = await openGame(context, client, gameId, saved.saveStateId, sha(original));
    await closeGame(restored, gameId);
    evidence.checks.push({errorCode, jobId: job.jobId, restored: true});
  }
  const changed = runCart(fixture, randomUUID());
  const replacement = archive("valid.zip", write("changed.wasm", changed), "nested/controls.wasm");
  const job = await replace(client, gameId, replacement);
  assert.equal(job.state, "SUCCEEDED");
  const updated = await openGame(context, client, gameId, null, sha(changed));
  await updated.page.screenshot({path: join(directory, "valid-replacement-playing.png")});
  await closeGame(updated, gameId);
  evidence.checks.push({validZIP: true, jobId: job.jobId, publishedSHA256: sha(changed)});
  const rawChanged = runCart(fixture, randomUUID());
  const rawJob = await replace(client, gameId, write("raw-replacement.wasm", rawChanged));
  assert.equal(rawJob.state, "SUCCEEDED");
  await closeGame(await openGame(context, client, gameId, null, sha(rawChanged)), gameId);
  evidence.checks.push({validRaw: true, jobId: rawJob.jobId, publishedSHA256: sha(rawChanged)});
  assert.deepEqual(evidence.errors, []);
  evidence.status = "PASS";
} catch (error) {
  evidence.error = error.stack; process.exitCode = 1;
} finally {
  await browser?.close(); await proxy.close();
  writeFileSync(join(directory, "result.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

function write(name, bytes) {const path = join(directory, name); writeFileSync(path, bytes); return path;}
function contentFacts({files, variants, version, status, payloadState, contentKind}) {
  return {files, variants, version, status, payloadState, contentKind};
}
function archive(name, source, entry) {
  const target = join(directory, name);
  execFileSync("python3", ["-c", "import sys,zipfile; z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED); z.write(sys.argv[2],sys.argv[3]); z.close()", target, source, entry]);
  return target;
}
async function replace(client, gameId, filename) {
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const current = await client.raw("GET", `/api/v1/admin/games/${gameId}`);
  const {jobId} = await client.json("POST", `/api/v1/admin/games/${gameId}/content-replacement`, {
    expected: 202, headers: {...client.writeHeaders(), "If-Match": current.headers().etag}, data: {uploadId},
  });
  for (const deadline = Date.now() + 30_000; Date.now() < deadline;) {
    const job = await client.json("GET", `/api/v1/admin/jobs/${jobId}`);
    if (["SUCCEEDED", "FAILED"].includes(job.state)) {return {...job, jobId};}
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw Error("replacement did not reach a terminal state");
}
async function openGame(context, client, gameId, saveStateId, digest) {
  const launch = await launchCart(client, gameId, saveStateId);
  const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
  assert.equal(config.resources.find(item => item.role === "game").sha256, digest);
  if (saveStateId) {assert.ok(config.restore.sizeBytes > 0);}
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message));
  await page.goto(`${base}${launch.playUrl}`, {waitUntil: "domcontentloaded"});
  const canvas = await runtimeCanvas(page, "WASM-4");
  await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 60_000});
  const before = sha(await canvas.screenshot());
  await canvas.press("ArrowRight", {delay: 160});
  assert.notEqual(sha(await canvas.screenshot()), before, "real game input must change the frame");
  return {page, launch};
}
async function closeGame({page, launch}, gameId) {
  await exitContentIOPlayer(page, base, {...launch, returnTo: `/games/${gameId}`});
  await page.close();
}
async function verifyDebugFocus(page) {
  await revealPreviewToolbar(page);
  const trigger = page.locator("#player-debug-trigger"), panel = page.locator("#player-debug-panel");
  assert.equal(await panel.getAttribute("inert"), "");
  await trigger.focus();
  for (let index = 0; index < 12; index++) {
    await page.keyboard.press("Tab");
    assert.equal(await panel.evaluate(element => element.contains(document.activeElement)), false);
  }
  await trigger.click();
  const summary = panel.locator("summary").filter({hasText: "运行环境与显示"});
  await summary.focus();
  assert.equal(await summary.evaluate(element => element === document.activeElement), true);
  await trigger.click();
  assert.equal(await trigger.evaluate(element => element === document.activeElement), true);
  assert.equal(await panel.getAttribute("inert"), "");
  evidence.checks.push({closedDebugTabExcluded: true, closeReturnsFocus: true});
}
