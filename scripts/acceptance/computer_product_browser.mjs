import assert from "node:assert/strict";
import {join} from "node:path";
import {proofDigest} from "./content_io_case_proof.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {finalContentMetrics} from "./content_io_measurement.mjs";
import {validateContentResources} from "./content_io_performance.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {executingContentWorker} from "./content_io_worker_identity.mjs";

export function computerResources(config) {
  return config.resources.flatMap(row => row.kind === "EXTERNAL_FILE_SET" ? row.files : row.kind === "ROM_BLOB" ? [row] : []);
}

export async function openComputer(context, base, launch, target, sources, preparePage = async () => {}) {
  const id = launch.launchId ?? launch.previewId;
  const response = await context.request.get(`${base}/runtime/launches/${id}/config`);
  assert.equal(response.status(), 200);
  let config = await response.json(); assert.equal(config.runtime.targetId, target);
  const resources = computerResources(config);
  const identity = rows => rows.map(({sha256, sizeBytes}) => `${sha256}:${sizeBytes}`).sort();
  assert.deepEqual(identity(resources), identity(sources), "COMPUTER_LAUNCH_SOURCE_MISMATCH");
  const network = observeContentIO(context, contentSourceMatcher(resources, base)); await network.ready;
  const page = await context.newPage(), errors = [], assets = [], pending = [];
  let workerAssetPath;
  page.on("pageerror", error => errors.push(error.message.split("\n")[0].slice(0, 160)));
  const assetListener = response => {
    const path = new URL(response.url()).pathname;
    if (path === `/runtime/launches/${id}/config`) {
      pending.push(response.json().then(observed => {config = observed;}, () => {errors.push("COMPUTER_CONFIG_RESPONSE_UNAVAILABLE");}));
      return;
    }
    if (!path.startsWith("/runtime/providers/") || !/\.(?:mjs|wasm|js)$/u.test(path)) return;
    if (path.endsWith("/assets/content-io/worker.mjs")) {workerAssetPath = path; return;}
    pending.push(response.body().then(bytes => {assets.push({path, sha256: proofDigest(bytes), sizeBytes: bytes.length});},
      error => {errors.push(`COMPUTER_ASSET_RESPONSE_UNAVAILABLE:${path}:${error.message.split("\n")[0]}`);}));
  };
  context.on("response", assetListener);
  const startedAt = performance.now();
  await preparePage(page);
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 60000});
  let frame, canvas;
  for (const deadline = Date.now() + 60000; Date.now() < deadline && !canvas;) {
    for (const candidate of page.frames()) {
      const ready = await candidate.evaluate(target => target === "bbc-jsbeeb" ? !!globalThis.RetromJsbeeb?.canvas :
        !!globalThis.Module?.FS && !!globalThis.Module?.ccall && !!document.querySelector("canvas"), target).catch(() => false);
      if (ready) {frame = candidate; canvas = frame.locator("canvas").first(); break;}
    }
    if (!canvas) await page.waitForTimeout(100);
  }
  assert.ok(canvas, "COMPUTER_READY_TIMEOUT");
  await resumePreview(page);
  // jsbeeb's visible monitor bezel covers the canvas; use a real pointer click on its centre.
  const box = await canvas.boundingBox(); assert.ok(box);
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await Promise.all(pending);
  assets.push(await executingContentWorker(context, page, workerAssetPath));
  assert.equal(config.runtime.targetId, target); assert.deepEqual(identity(computerResources(config)), identity(sources));
  return {page, frame, canvas, config, resources, network, launch: {...launch, launchId: id}, startedAt, errors, assets,
    async flush() {await network.flush(); await Promise.all(pending); assert.deepEqual(errors, []);},
    dispose() {network.close(); context.off("response", assetListener);}};
}

export async function pictureComputer(opened, directory, name) {
  const bytes = await opened.canvas.screenshot();
  await opened.page.screenshot({path: join(directory, `${name}.png`)});
  return {path: `${name}.png`, canvasSha256: proofDigest(bytes)};
}

export async function pauseComputer(opened) {
  await revealPreviewToolbar(opened.page);
  const pause = opened.page.getByRole("button", {name: "暂停", exact: true});
  if (await pause.isVisible()) await pause.click();
}

export async function closeComputer(opened, base, collector, semantics = "INSTANT") {
  await opened.flush();
  if (opened.config.session.purpose === "REVIEW_PREVIEW") {
    await revealPreviewToolbar(opened.page);
    const finished = opened.page.waitForResponse(response => response.request().method() === "POST" &&
      new URL(response.url()).pathname === `/runtime/launches/${opened.launch.launchId}/finish`)
      .then(response => ({response}), error => ({error}));
    await opened.page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
    const name = semantics === "GAME_SAVE" ? /^(直接退出|继续退出)$/u : "退出游戏";
    await opened.page.getByRole("alertdialog").getByRole("button", {name, exact: true}).click();
    const result = await finished; if (result.error) throw result.error;
    assert.ok(result.response.ok());
  } else await exitContentIOPlayer(opened.page, base, opened.launch, semantics);
  return collectComputerExit(opened, collector);
}

export async function saveComputerDisk(page, launchId) {
  await revealPreviewToolbar(page);
  const response = page.waitForResponse(entry => entry.request().method() === "POST" &&
    new URL(entry.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 30000})
    .then(value => ({value}), error => ({error}));
  await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
  await page.getByRole("button", {name: "存档并退出", exact: true}).click();
  const result = await response; if (result.error) throw result.error;
  const saved = result.value; assert.equal(saved.status(), 201, "COMPUTER_NATIVE_SAVE_FAILED");
  return saved.json();
}

export async function collectComputerExit(opened, collector) {
  await opened.flush();
  const sessions = collector.snapshot(opened.page), metrics = finalContentMetrics(sessions);
  validateContentResources(metrics.publicPeak, false); validateContentResources(metrics.closed, true);
  const result = {launchId: opened.launch.launchId, runtime: {
    bundleSha256: opened.config.runtime.bundleSha256, moduleSha256: opened.config.runtime.moduleSha256},
    assets: opened.assets, requests: opened.network.requests, sessions, metrics};
  opened.dispose(); await opened.page.close(); return result;
}
