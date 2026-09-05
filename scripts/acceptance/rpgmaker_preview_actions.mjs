import {createHash} from "node:crypto";
import {readEasyRpgPosition, readRgssFixtureLine} from "./rpgmaker_fixture_observation.mjs";
import {observeCheckpointUpload, readCheckpointMultipart} from "./rpgmaker_checkpoint_upload.mjs";

// Browser automation of the same Player buttons used by a reviewer.
export async function observeOwnedFixture(page) {
  const observations = {last: null};
  await page.exposeBinding("__retromObserveOwnedFixture", (_source, detail) => {
    if (detail?.code !== "RETROM_RUNTIME_MKXP_Z" || typeof detail.message !== "string") {return;}
    const value = readRgssFixtureLine(detail.message);
    if (value) {observations.last = {...value, receivedAtMs: Date.now()};}
  });
  await page.addInitScript(() => {
    window.addEventListener("retrom:runtime-diagnostic", (event) => {
      if (event instanceof CustomEvent && event.detail?.code === "RETROM_RUNTIME_MKXP_Z") {
        void window.__retromObserveOwnedFixture?.(event.detail);
      }
    });
  });
  return observations;
}

export async function waitForPreviewReady(page) {
  const fatalError = page.__retromFatalError ?? new Promise(() => {});
  const runtimeFailure = page.getByRole("alert").filter({hasText: /\b(?:RPG|RUNTIME)_[A-Z0-9_]+\b/u}).first();
  const statusFailure = page.getByRole("status").filter({hasText: /\b(?:RPG|RUNTIME|PLAYER)_[A-Z0-9_]+\b/u}).first();
  try {
    await Promise.race([
      page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 300_000}),
      runtimeFailure.waitFor({state: "visible", timeout: 300_000}).then(() => {throw new Error("runtime failed");}),
      statusFailure.waitFor({state: "visible", timeout: 300_000}).then(() => {throw new Error("runtime failed");}),
      fatalError.then(() => {throw new Error("page error");}),
      page.waitForResponse((response) => response.request().method() === "POST" &&
        /^\/runtime\/launches\/[^/]+\/finish$/.test(new URL(response.url()).pathname), {timeout: 300_000})
        .then(() => {throw new Error("session finished during mount");}),
    ]);
  } catch {
    await Promise.allSettled(page.__retromExceptionTasks ?? []);
    const runtimeDiagnostics = (page.__retromRuntimeDiagnostics ?? []).slice(-20);
    const trimDiagnostic = (value) => String(value).trim().slice(0, 600);
    const diagnostics = {
      alerts: (await page.getByRole("alert").allInnerTexts().catch(() => [])).map(trimDiagnostic).slice(0, 5),
      loading: (await page.locator(".player-loading").allTextContents().catch(() => [])).map(trimDiagnostic).slice(0, 3),
      statuses: (await page.getByRole("status").allTextContents().catch(() => [])).map(trimDiagnostic).slice(0, 10),
      pageErrors: (page.__retromPageErrors ?? []).map(trimDiagnostic).slice(0, 5),
      consoleDiagnostics: (page.__retromConsoleDiagnostics ?? []).slice(-30),
      exceptionDiagnostics: (page.__retromExceptionDiagnostics ?? []).slice(-20),
      projectRequests: (page.__retromProjectRequests ?? []).slice(-30),
      networkRequests: (page.__retromNetworkRequests ?? []).slice(-100),
      runtimeDiagnostics: runtimeDiagnostics.map((value) => ({
        code: trimDiagnostic(value.code).slice(0, 128), message: trimDiagnostic(value.message),
      })),
    };
    throw new Error("RPG_PROVISION_RUNTIME_ACTION_UNAVAILABLE_READY:" + JSON.stringify(diagnostics));
  }
}

export async function focusPreviewCanvas(page) {
  await resumePreview(page);
  for (const frame of page.frames()) {
    const canvas = frame.locator("canvas").first();
    if (await canvas.isVisible().catch(() => false)) {
      await canvas.evaluate((element) => {element.tabIndex = 0; element.focus();});
      return canvas;
    }
  }
  throw new Error("RPG_PREVIEW_CANVAS_MISSING");
}

export async function advanceFixture(page, keys) {
  const canvas = await focusPreviewCanvas(page);
  for (const key of keys) {
    // The owned LCF fixture crosses a tile in ~133 ms; longer holds repeat
    // movement and can skip the distinct B checkpoint before reaching C.
    await canvas.press(key, {delay: 80});
    await page.waitForTimeout(800);
  }
}

export async function revealPreviewToolbar(page) {
  if (!await page.locator(".player-toolbar").evaluate((element) => element.classList.contains("is-visible"))) {
    await page.locator(".player-hud-handle").hover();
  }
  await page.locator(".player-toolbar.is-visible").waitFor({state: "visible"});
  // The reveal handle starts a two-second idle timer; entering the toolbar is
  // the product's normal hold interaction. Keep it open during CDP setup too.
  await page.locator(".player-game-meta").hover();
}

export async function resumePreview(page) {
  const resume = page.getByRole("button", {name: "继续游戏", exact: true});
  if (await resume.isVisible().catch(() => false)) {
    await resume.click();
    await resume.waitFor({state: "hidden", timeout: 30_000});
  }
}

export async function capturePreviewCheckpoint(page, previewId) {
  await revealPreviewToolbar(page);
  const observation = await observeCheckpointUpload(page, previewId);
  try {return await captureObservedCheckpoint(page, previewId, observation);}
  finally {await observation.close();}
}

async function captureObservedCheckpoint(page, previewId, observation) {
  const startedAtMs = Date.now();
  const requestTask = page.waitForRequest((request) => request.method() === "POST" &&
    new URL(request.url()).pathname === "/runtime/launches/" + previewId + "/save-states", {timeout: 300_000})
    .then((request) => ({request}), (error) => ({error}));
  await page.getByRole("button", {name: "创建存档", exact: true}).click();
  const observedRequest = await requestTask;
  if (observedRequest.error) {throw observedRequest.error;}
  const {request} = observedRequest;
  const response = await request.response();
  if (!response) {throw new Error("RPG_PREVIEW_CHECKPOINT_RESPONSE_MISSING");}
  const receipt = await response.json();
  const observed = await observation.take(request);
  const result = await inspectPreviewCheckpoint(request, response.status(), receipt, previewId, observed);
  await resumePreview(page);
  return {...result, startedAtMs, finishedAtMs: Date.now()};
}

export async function inspectPreviewCheckpoint(request, status, receipt, previewId, observed) {
  if (status !== 201 || receipt?.resourceKind !== "REVIEW_PREVIEW_CHECKPOINT" ||
      receipt.previewId !== previewId || typeof receipt.checkpointFormat !== "string" ||
      !Number.isSafeInteger(receipt.createdAtMs) || receipt.createdAtMs < 0) {
    const code = typeof receipt?.error?.code === "string" && /^[A-Z][A-Z0-9_]{0,127}$/.test(receipt.error.code)
      ? receipt.error.code : "UNKNOWN";
    throw new Error("RPG_PREVIEW_CHECKPOINT_RECEIPT_INVALID:HTTP_" + status + ":" + code);
  }
  const {form, length} = await readCheckpointMultipart(request, observed);
  const payload = form.get("payload");
  const screenshot = form.get("screenshot");
  if (!(payload instanceof Blob) || !(screenshot instanceof Blob) || !payload.size || !screenshot.size) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_PAYLOAD_MISSING");
  }
  const bytes = Buffer.from(await payload.arrayBuffer());
  return {
    bytes, screenshot: Buffer.from(await screenshot.arrayBuffer()), format: receipt.checkpointFormat,
    sizeBytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex"),
    requestContentLengthBytes: length, responseStatus: status,
  };
}

export async function observeFixturePosition(page, generation, observations, checkpoint) {
  if (generation === "RPG2000" || generation === "RPG2003") {
    if (!checkpoint) {throw new Error("RPG_PREVIEW_POSITION_NEEDS_ORDINARY_SAVE");}
    return readEasyRpgPosition(checkpoint.bytes, generation);
  }
  if (["RPGXP", "RPGVX", "RPGVXACE"].includes(generation)) {
    await resumePreview(page);
    const since = Date.now();
    for (let i = 0; i < 100; i += 1) {
      if (observations.last?.receivedAtMs >= since) {return observations.last.position;}
      await page.waitForTimeout(100);
    }
    throw new Error("RPG_PREVIEW_OWNED_FIXTURE_STATE_MISSING");
  }
  for (const frame of page.frames()) {
    const value = await frame.evaluate((expected) => {
      if (globalThis.Utils?.RPGMAKER_NAME !== expected || !globalThis.$gameMap ||
          !globalThis.$gamePlayer || !globalThis.$gameVariables) {return null;}
      return {mapId: $gameMap.mapId(), playerX: $gamePlayer.x, playerY: $gamePlayer.y,
        fixtureState: $gameVariables.value(1)};
    }, generation === "RPGMV" ? "MV" : "MZ").catch(() => null);
    if (value) {return value;}
  }
  throw new Error("RPG_PREVIEW_NATIVE_ENGINE_STATE_MISSING");
}

export async function observePreviewFrames(page) {
  await resumePreview(page);
  const beforeFrame = await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount());
  if (!Number.isSafeInteger(beforeFrame) || beforeFrame < 0) {throw new Error("RPG_PREVIEW_FRAME_COUNT_MISSING");}
  try {
    await page.waitForFunction((before) => {
      const after = window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount();
      return Number.isSafeInteger(after) && after - before >= 300;
    }, beforeFrame, {timeout: 30_000});
  } catch {
    const trim = (value) => String(value).trim().slice(0, 600);
    const diagnostics = {
      beforeFrame,
      afterFrame: await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount()).catch(() => null),
      alerts: (await page.getByRole("alert").allInnerTexts().catch(() => [])).map(trim).slice(0, 5),
      statuses: (await page.getByRole("status").allInnerTexts().catch(() => [])).map(trim).slice(0, 10),
      pageErrors: (page.__retromPageErrors ?? []).map(trim).slice(0, 5),
      consoleDiagnostics: (page.__retromConsoleDiagnostics ?? []).slice(-30),
      networkRequests: (page.__retromNetworkRequests ?? []).slice(-100),
    };
    throw new Error("RPG_PREVIEW_FRAMES_STALLED:" + JSON.stringify(diagnostics));
  }
  const afterFrame = await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount());
  return {beforeFrame, afterFrame};
}

export async function captureOptionalReviewScreenshot(page, previewId) {
  await revealPreviewToolbar(page);
  const responseTask = page.waitForResponse((response) => response.request().method() === "POST" &&
    new URL(response.url()).pathname === "/runtime/launches/" + previewId + "/review-screenshot");
  await page.getByRole("button", {name: "保存审核截图", exact: true}).click();
  const response = await responseTask;
  if (response.status() !== 201) {throw new Error("RPG_PREVIEW_SCREENSHOT_UPLOAD_FAILED");}
  await resumePreview(page);
}

export async function finishPreview(page, previewId) {
  await revealPreviewToolbar(page);
  await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
  const responseTask = page.waitForResponse((response) => response.request().method() === "POST" &&
    new URL(response.url()).pathname === "/runtime/launches/" + previewId + "/finish");
  await page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click();
  const response = await responseTask;
  if (!response.ok()) {throw new Error("RPG_PREVIEW_FINISH_FAILED");}
  if (!page.isClosed()) {await page.close();}
}
