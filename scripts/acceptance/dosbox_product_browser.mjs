import assert from "node:assert/strict";
import {proofDigest} from "./content_io_case_proof.mjs";
import {observeContentIO, contentSourceMatcher, rangeSummary} from "./content_io_observation.mjs";
import {executingContentWorker} from "./content_io_worker_identity.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";

export async function observeDOSStates(context) {
  await context.addInitScript(() => {
    globalThis.__dosStateObservation = {captures: [], restores: [], pending: []};
    const installed = new WeakSet(); let callback;
    const record = async (kind, bytes) => {
      const sha = await crypto.subtle.digest("SHA-256", bytes);
      globalThis.__dosStateObservation[kind].push({sizeBytes: bytes.byteLength,
        sha256: Array.from(new Uint8Array(sha), byte => byte.toString(16).padStart(2, "0")).join("")});
    };
    Object.defineProperty(globalThis, "EJS_onGameStart", {configurable: true, get: () => callback, set(value) {
      if (typeof value !== "function") {callback = value; return;}
      callback = function(...args) {
        const manager = globalThis.EJS_emulator.gameManager;
        if (!installed.has(manager)) {
          installed.add(manager);
          const capture = manager.getState, restore = manager.loadExplicitStateAndWait;
          manager.getState = function(...parameters) {
            const bytes = capture.apply(this, parameters);
            globalThis.__dosStateObservation.pending.push(Promise.resolve(bytes).then(value => record("captures", value)));
            return bytes;
          };
          manager.loadExplicitStateAndWait = async function(bytes, ...parameters) {
            await restore.call(this, bytes, ...parameters); await record("restores", bytes);
          };
        }
        return value.apply(this, args);
      };
    }});
  });
}

export async function openDOS(context, base, launch, preparePage = async () => {}) {
  const id = launch.launchId ?? launch.previewId;
  const response = await context.request.get(`${base}/runtime/launches/${id}/config`); assert.equal(response.status(), 200);
  let config = await response.json(); assert.equal(config.runtime.targetId, "dosbox-pure");
  const tree = config.resources.find(row => row.role === "game" && row.kind === "FILE_TREE"); assert.ok(tree);
  const indexResponse = await context.request.get(new URL(tree.indexUrl, base).href); assert.equal(indexResponse.status(), 200);
  const index = await indexResponse.json(); assert.equal(index.schemaVersion, 1); assert.equal(index.files.length, 1);
  const source = index.files[0]; assert.equal(source.path, "game.zip");
  source.url = new URL(source.url, new URL(tree.indexUrl, base)).href;
  const network = observeContentIO(context, contentSourceMatcher([source], base)); await network.ready;
  const page = await context.newPage(), errors = [], assets = [], pending = []; let workerAssetPath;
  page.on("pageerror", error => errors.push(error.message.split("\n")[0].slice(0, 200)));
  const listener = response => {
    const path = new URL(response.url()).pathname;
    if (path === `/runtime/launches/${id}/config`) {
      pending.push(response.json().then(value => {config = value;})); return;
    }
    if (!path.startsWith("/runtime/providers/") || !/\.(?:mjs|js|wasm|data)$/u.test(path)) return;
    if (path.endsWith("/assets/content-io/worker.mjs")) {workerAssetPath = path; return;}
    pending.push(response.body().then(bytes => assets.push({path, sha256: proofDigest(bytes), sizeBytes: bytes.length}),
      () => errors.push(`DOS_PROVIDER_BODY_UNAVAILABLE:${path}`)));
  };
  context.on("response", listener);
  await preparePage(page, source);
  const startedAt = performance.now();
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 60000});
  let frame;
  for (const deadline = Date.now() + 90000; Date.now() < deadline && !frame;) {
    for (const candidate of page.frames()) {
      if (await candidate.evaluate(() => !!globalThis.EJS_emulator?.started && !!globalThis.EJS_emulator?.gameManager).catch(() => false)) {
        frame = candidate; break;
      }
    }
    if (!frame) await page.waitForTimeout(100);
  }
  assert.ok(frame, "DOS_CORE_START_TIMEOUT");
  const canvas = frame.locator("canvas").first(); await resumePreview(page); await canvas.click();
  await Promise.all(pending); assets.push(await executingContentWorker(context, page, workerAssetPath));
  assert.equal(config.resources.find(row => row.role === "game").contentDigest, tree.contentDigest);
  return {context, page, frame, canvas, launch: {...launch, launchId: id}, config, source, contentDigest: tree.contentDigest,
    network, assets, errors, startedAt,
    async flush() {await network.flush(); await Promise.all(pending); assert.deepEqual(errors, []);},
    rangeSummary: () => rangeSummary(network.requests, source),
    dispose() {network.close(); context.off("response", listener);}};
}
