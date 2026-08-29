#!/usr/bin/env node

import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { chromium } from "../../web/node_modules/playwright/index.mjs";

import { assertKiriKiriProductEvidence, kirikiriProductStages } from "./kirikiri_product_contract.mjs";
import { createProductClient, singleFile } from "./rpgmaker_security_upload.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";
import { trackRuntimeLoading } from "./runtime_loading_evidence.mjs";

const caseId = "ACC-KIRIKIRI-001";
const requiredEnvironment = [
  "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_KIRIKIRI_SMOKE_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR",
];
const missing = requiredEnvironment.filter((name) => !process.env[name]);
const caseDirectory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".");
const evidencePath = join(caseDirectory, "kirikiri-product.json");

class AcceptanceBlocked extends Error {}

if (missing.length) {
  writeEvidence({ schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "KIRIKIRI_ACCEPTANCE_INPUT_REQUIRED" });
  process.stderr.write(`KIRIKIRI_ACCEPTANCE_INPUT_REQUIRED:${missing.join(",")}\n`);
  process.exit(3);
}

const baseUrl = normalizedBaseUrl(process.env.RETROM_ACCEPTANCE_BASE_URL);
const screenshotsDirectory = join(caseDirectory, "screenshots");
mkdirSync(screenshotsDirectory, { recursive: true });

let browser;
try {
  browser = await chromium.launch({ executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true });
  const evidence = await runProductCase(browser);
  assertKiriKiriProductEvidence(evidence);
  writeEvidence(evidence);
  process.stdout.write(`${JSON.stringify(evidence)}\n`);
} catch (error) {
  const errorCode = stableErrorCode(error);
  const blocked = error instanceof AcceptanceBlocked;
  writeEvidence({ schemaVersion: 1, caseId, status: blocked ? "BLOCKED" : "FAIL", errorCode });
  process.stderr.write(`${errorCode}\n`);
  process.exitCode = blocked ? 3 : 1;
} finally {
  await browser?.close();
}

async function runProductCase(activeBrowser) {
  const context = await activeBrowser.newContext({ viewport: { width: 1440, height: 1000 } });
  await installVirtualStandardGamepad(context);
  const browserErrors = { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 };
  try {
    const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
      headers: { Origin: baseUrl },
      data: { username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD },
      failOnStatusCode: false,
    });
    requireStatus(loginResponse.status(), 200, "KIRIKIRI_ACCEPTANCE_LOGIN_FAILED");
    const login = await loginResponse.json();
    const client = createProductClient(context, baseUrl, login.csrfToken);
    const platformInstanceId = await kirikiriPlatformInstance(client);
    const uploadId = await client.upload(singleFile(process.env.RETROM_KIRIKIRI_SMOKE_ARCHIVE), "FILES", "KIRIKIRI_PROJECT");
    const importedResponse = await client.raw("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(),
      timeout: 120_000,
      data: {
        uploadId, targetPlatformInstanceId: platformInstanceId, metadataProvider: "NONE",
        contentMode: "KIRIKIRI_PROJECT_V1", tagIds: [],
      },
    });
    if (importedResponse.status() !== 202) {
      const body = await importedResponse.text();
      const code = responseErrorCode(body);
      process.stderr.write(`KIRIKIRI_ACCEPTANCE_IMPORT_CREATE_FAILED:status=${importedResponse.status()},body=${body}\n`);
      if (importedResponse.status() === 422 && code === "ARCHIVE_ENCRYPTED_UNSUPPORTED") {
        throw new AcceptanceBlocked("KIRIKIRI_ACCEPTANCE_ARCHIVE_ENCRYPTED");
      }
      throw new Error("KIRIKIRI_ACCEPTANCE_IMPORT_CREATE_FAILED");
    }
    const imported = await importedResponse.json();
    await waitForImport(client, imported.importJobId);
    const review = await reviewForImport(client, imported.importJobId);

    const preview = await createPreview(client, review.itemId);
    const previewPage = await trackedPage(context, browserErrors);
    await previewPage.goto(`${baseUrl}${preview.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const previewCanvas = await runtimeCanvas(previewPage);
    await withStableStage(
      () => verifyGamepadCancel(previewCanvas), "KIRIKIRI_ACCEPTANCE_GAMEPAD_CANCEL_FAILED",
    );
    await withStableStage(() => advanceKag(previewCanvas), "KIRIKIRI_ACCEPTANCE_GAMEPAD_CONFIRM_FAILED");
    await previewPage.getByText("第 5 秒运行截图已保存；可以继续试玩。").waitFor({ timeout: 120_000 });
    const previewFrame = await screenshotEvidence(previewCanvas, "preview.png");
    await previewPage.close();

    const approved = await approveReview(client, review.itemId);
    const immersive = await createLaunch(client, approved.gameId, null);
    const immersivePage = await trackedPage(context, browserErrors);
    const immersiveUrl = new URL(`${baseUrl}${immersive.playUrl}`);
    immersiveUrl.searchParams.set("experience", "immersive");
    await immersivePage.goto(immersiveUrl.href, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const immersiveCanvas = await runtimeCanvas(immersivePage);
    await withStableStage(
      () => waitForKagStable(immersiveCanvas), "KIRIKIRI_ACCEPTANCE_IMMERSIVE_RUNTIME_NOT_READY",
    );
    const immersiveMenu = await withStableStage(
      () => openImmersiveExitMenu(immersivePage, immersiveCanvas),
      "KIRIKIRI_ACCEPTANCE_IMMERSIVE_EXIT_MENU_FAILED",
    );
    await immersivePage.close();

    const original = await createLaunch(client, approved.gameId, null);
    const originalPage = await trackedPage(context, browserErrors);
    const originalLoadingProbe = trackRuntimeLoading(originalPage);
    await originalPage.goto(`${baseUrl}${original.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const originalCanvas = await runtimeCanvas(originalPage);
    await waitForProductReady(originalPage);
    const beforeInput = await screenshotEvidence(originalCanvas, "product-before-input.png");
    const originalLoading = await originalLoadingProbe.snapshot();
    originalLoadingProbe.stop();
    await advanceKag(originalCanvas);
    const afterInput = await screenshotEvidence(originalCanvas, "product-after-input.png");
    requireChanged(beforeInput, afterInput, "KIRIKIRI_ACCEPTANCE_PRODUCT_INPUT_UNOBSERVED");
    const saved = await createCheckpoint(originalPage, original.launchId);
    await resumePlayerAfterCheckpoint(originalPage);
    await advanceKag(originalCanvas);
    const afterCheckpoint = await screenshotEvidence(originalCanvas, "product-after-checkpoint.png");
    requireChanged(afterInput, afterCheckpoint, "KIRIKIRI_ACCEPTANCE_PRODUCT_C_UNOBSERVED");
    await originalPage.close();

    const restored = await createLaunch(client, approved.gameId, saved.saveStateId);
    if (restored.launchId === original.launchId) {throw new Error("KIRIKIRI_ACCEPTANCE_RESTORE_LAUNCH_REUSED");}
    const restoredPage = await trackedPage(context, browserErrors);
    const restoreLoadingProbe = trackRuntimeLoading(restoredPage);
    const stateResponsePromise = restoredPage.waitForResponse((response) =>
      response.request().method() === "GET" && response.url().endsWith(`/runtime/launches/${restored.launchId}/state`),
    { timeout: 120_000 });
    await restoredPage.goto(`${baseUrl}${restored.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const restoredCanvas = await runtimeCanvas(restoredPage);
    await waitForProductReady(restoredPage);
    const restoreLoading = await restoreLoadingProbe.snapshot();
    restoreLoadingProbe.stop();
    const stateResponse = await stateResponsePromise;
    requireStatus(stateResponse.status(), 200, "KIRIKIRI_ACCEPTANCE_RESTORE_PAYLOAD_FAILED");
    const payloadSize = Number(stateResponse.headers()["content-length"]);
    const restoredFrame = await waitForMatchingScreenshot(
      restoredCanvas, "restored.png", afterInput.rgbaSha256, 60_000,
    );
    await advanceKag(restoredCanvas);
    const postRestoreFrame = await screenshotEvidence(restoredCanvas, "post-restore-input.png");
    requireChanged(restoredFrame, postRestoreFrame, "KIRIKIRI_ACCEPTANCE_RESTORE_INPUT_UNOBSERVED");
    await restoredPage.close();
    if (Object.values(browserErrors).some((count) => count !== 0)) {throw new Error("KIRIKIRI_ACCEPTANCE_BROWSER_ERROR");}

    return {
      schemaVersion: 1,
      caseId,
      status: "PASS",
      stages: [...kirikiriProductStages],
      ids: {
        importItemId: review.itemId, gameId: approved.gameId, saveStateId: saved.saveStateId,
        immersiveLaunchId: immersive.launchId, originalLaunchId: original.launchId, restoreLaunchId: restored.launchId,
      },
      immersiveMenu,
      checkpoint: { payloadKind: saved.payloadKind, sizeBytes: payloadSize },
      loading: {
        schemaVersion: 1,
        sameProjectContentIdentity: originalLoading.projectContentIdentity !== null &&
          originalLoading.projectContentIdentity === restoreLoading.projectContentIdentity,
        firstVisible: originalLoading.evidence,
        restoreVisible: restoreLoading.evidence,
      },
      screenshots: {
        preview: previewFrame, productBeforeInput: beforeInput, productAfterInput: afterInput,
        productAfterCheckpoint: afterCheckpoint,
        restored: restoredFrame, postRestoreInput: postRestoreFrame,
      },
      browser: browserErrors,
    };
  } finally {
    await context.close();
  }
}

async function openImmersiveExitMenu(page, canvas) {
  await setVirtualGamepadButton(canvas, 8, true);
  await page.waitForTimeout(50);
  await setVirtualGamepadButton(canvas, 9, true);
  await page.waitForTimeout(50);
  await setVirtualGamepadButton(canvas, 8, false);
  await setVirtualGamepadButton(canvas, 9, false);
  await page.waitForTimeout(80);
  await setVirtualGamepadButton(canvas, 8, true);
  await page.waitForTimeout(50);
  await setVirtualGamepadButton(canvas, 9, true);
  const dialog = page.getByRole("dialog", { name: "游戏菜单", exact: true });
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  await setVirtualGamepadButton(canvas, 8, false);
  await setVirtualGamepadButton(canvas, 9, false);
  const actions = ["取消", "创建存档", "退出游戏"];
  for (const action of actions) {await dialog.getByRole("button", { name: action, exact: true }).waitFor();}
  const screenshot = "screenshots/immersive-exit-menu.png";
  await page.screenshot({ path: join(caseDirectory, screenshot), fullPage: true });
  return { actions, screenshot };
}

async function kirikiriPlatformInstance(client) {
  let response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=kirikiri&limit=100");
  let found = response.items?.find((item) => item.enabled && item.defaultCoreId === "kirikiri2");
  if (!found) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=kirikiri&limit=100");
    found = response.items?.find((item) => item.enabled && item.defaultCoreId === "kirikiri2");
  }
  const identifier = found?.id ?? found?.platformInstanceId;
  if (typeof identifier !== "string") {throw new Error("KIRIKIRI_ACCEPTANCE_PLATFORM_MISSING");}
  return identifier;
}

async function waitForImport(client, importJobId) {
  for (let attempt = 0; attempt < 1_200; attempt += 1) {
    const job = await client.json("GET", `/api/v1/admin/imports/${importJobId}`);
    if (["REVIEW_PENDING", "COMPLETED"].includes(job.state)) {return;}
    if (["FAILED", "CANCELLED"].includes(job.state)) {throw new Error("KIRIKIRI_ACCEPTANCE_IMPORT_FAILED");}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_IMPORT_TIMEOUT");
}

async function reviewForImport(client, importJobId) {
  const queue = await client.json("GET", `/api/v1/admin/reviews?importJobId=${importJobId}&limit=20`);
  if (queue.items?.length !== 1) {throw new Error("KIRIKIRI_ACCEPTANCE_REVIEW_CARDINALITY");}
  return client.json("GET", `/api/v1/admin/reviews/${queue.items[0].itemId}`);
}

async function createPreview(client, itemId) {
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/previews`, {
    headers: client.writeHeaders(),
    data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
  });
  requireStatus(response.status(), 201, "KIRIKIRI_ACCEPTANCE_PREVIEW_CREATE_FAILED");
  return response.json();
}

async function approveReview(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  requireStatus(snapshot.status(), 200, "KIRIKIRI_ACCEPTANCE_REVIEW_READ_FAILED");
  const etag = snapshot.headers().etag;
  if (!etag) {throw new Error("KIRIKIRI_ACCEPTANCE_REVIEW_ETAG_MISSING");}
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: { ...client.writeHeaders(), "If-Match": etag }, data: {},
  });
  requireStatus(response.status(), 201, "KIRIKIRI_ACCEPTANCE_APPROVE_FAILED");
  return response.json();
}

async function createLaunch(client, gameId, saveStateId) {
  return client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: {
      gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
}

async function trackedPage(context, browserErrors) {
  const page = await context.newPage();
  page.on("pageerror", () => {browserErrors.pageErrorCount += 1;});
  page.on("console", (message) => {if (message.type() === "error") {browserErrors.consoleErrorCount += 1;}});
  page.on("dialog", async (dialog) => {browserErrors.dialogCount += 1; await dialog.dismiss();});
  return page;
}

async function runtimeCanvas(page) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator("canvas").first();
      if (!await canvas.isVisible().catch(() => false)) {continue;}
      await focusRuntimeCanvas(canvas);
      const layout = await canvasLayoutEvidence(canvas).catch(() => null);
      if (validCanvasLayout(layout)) {return canvas;}
    }
    await page.waitForTimeout(100);
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_CANVAS_LAYOUT_INVALID");
}

async function advanceKag(canvas) {
  await focusRuntimeCanvas(canvas);
  await waitForKagStable(canvas);
  const transition = waitForKagTransition(canvas);
  await moveVirtualGamepadCursor(canvas, 0.5, 0.34);
  await setVirtualGamepadButton(canvas, 0, true);
  await canvas.page().waitForTimeout(100);
  await setVirtualGamepadButton(canvas, 0, false);
  await transition;
  await canvas.page().waitForTimeout(2_000);
}

async function verifyGamepadCancel(canvas) {
  await canvas.evaluate((element) => {
    element.dataset.retromAcceptanceContextMenu = "";
    element.ownerDocument.defaultView?.addEventListener("contextmenu", (event) => {
      event.preventDefault();
      element.dataset.retromAcceptanceContextMenu = `${event.button}:${event.buttons}`;
    }, { capture: true, once: true });
  });
  await setVirtualGamepadButton(canvas, 1, true);
  await canvas.page().waitForTimeout(150);
  await setVirtualGamepadButton(canvas, 1, false);
  const deadline = Date.now() + 2_000;
  while (Date.now() < deadline) {
    const observed = await canvas.evaluate((element) => element.dataset.retromAcceptanceContextMenu);
    if (observed === "2:0") {return;}
    await canvas.page().waitForTimeout(25);
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_GAMEPAD_CANCEL_FAILED");
}

async function moveVirtualGamepadCursor(canvas, targetX, targetY) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    const position = await virtualGamepadCursorPosition(canvas);
    if (position && Math.abs(position.x - targetX) <= 0.015 && Math.abs(position.y - targetY) <= 0.015) {
      await setVirtualGamepadAxis(canvas, 0, 0);
      return;
    }
    const x = position ? Math.sign(targetX - position.x) : 1;
    const y = position ? Math.sign(targetY - position.y) : -1;
    await setVirtualGamepadAxis(canvas, x, y);
    await canvas.page().waitForTimeout(25);
  }
  await setVirtualGamepadAxis(canvas, 0, 0);
  throw new Error("KIRIKIRI_ACCEPTANCE_GAMEPAD_CURSOR_FAILED");
}

async function virtualGamepadCursorPosition(canvas) {
  return canvas.evaluate((element) => {
    const cursor = element.ownerDocument.querySelector("[data-kirikiri-gamepad-cursor]");
    const surface = element.closest("[data-kirikiri-runtime-surface]");
    if (!(cursor instanceof HTMLElement) || !(surface instanceof HTMLElement) || cursor.hidden) {return null;}
    const cursorRect = cursor.getBoundingClientRect();
    const canvasRect = element.getBoundingClientRect();
    return {
      x: (cursorRect.left + cursorRect.width / 2 - canvasRect.left) / canvasRect.width,
      y: (cursorRect.top + cursorRect.height / 2 - canvasRect.top) / canvasRect.height,
    };
  });
}

async function setVirtualGamepadAxis(canvas, x, y) {
  await canvas.evaluate((element, input) => {
    const controller = element.ownerDocument.defaultView?.__retromTestGamepad;
    controller?.axis(0, input.x);
    controller?.axis(1, input.y);
  }, { x, y });
}

async function setVirtualGamepadButton(canvas, index, pressed) {
  await canvas.evaluate((element, input) => {
    element.ownerDocument.defaultView?.__retromTestGamepad?.button(input.index, input.pressed);
  }, { index, pressed });
}

async function installVirtualStandardGamepad(context) {
  await context.addInitScript(() => {
    const state = {
      axes: [0, 0, 0, 0],
      buttons: Array.from({ length: 17 }, () => ({ pressed: false, touched: false, value: 0 })),
    };
    Object.defineProperty(navigator, "getGamepads", {
      configurable: true,
      value: () => [{
        axes: state.axes,
        buttons: state.buttons,
        connected: true,
        id: "Retrom acceptance standard gamepad",
        index: 0,
        mapping: "standard",
        timestamp: performance.now(),
      }],
    });
    globalThis.__retromTestGamepad = {
      axis(index, value) {state.axes[index] = value;},
      button(index, pressed) {
        state.buttons[index] = { pressed, touched: pressed, value: pressed ? 1 : 0 };
      },
    };
  });
}

async function waitForKagStable(canvas) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (await kagBookmarkReady(canvas)) {return;}
    await canvas.page().waitForTimeout(50);
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_RUNTIME_NOT_STABLE");
}

async function waitForKagTransition(canvas) {
  const deadline = Date.now() + 60_000;
  let observedUnstable = false;
  while (Date.now() < deadline) {
    const ready = await kagBookmarkReady(canvas);
    if (!ready) {observedUnstable = true;}
    if (observedUnstable && ready) {return;}
    await canvas.page().waitForTimeout(50);
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_INPUT_TRANSITION_TIMEOUT");
}

async function kagBookmarkReady(canvas) {
  return canvas.evaluate((element) => {
    const module = element.ownerDocument.defaultView?.Module;
    const readiness = module?._krkr2_host_bookmark_is_ready;
    return typeof readiness === "function" && readiness.call(module) === 1;
  });
}

async function focusRuntimeCanvas(canvas) {
  await canvas.evaluate((element) => {element.tabIndex = 0; element.focus();});
}

async function createCheckpoint(page, launchId) {
  await page.mouse.move(720, 1);
  const saveButton = page.getByRole("button", { name: "创建存档", exact: true });
  await saveButton.waitFor({ state: "visible", timeout: 120_000 });
  await waitForEnabled(saveButton, 120_000);
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && response.url().includes(`/runtime/launches/${launchId}/save-states`),
  { timeout: 120_000 });
  await saveButton.click();
  const response = await responsePromise;
  requireStatus(response.status(), 201, "KIRIKIRI_ACCEPTANCE_SAVE_FAILED");
  return response.json();
}

async function resumePlayerAfterCheckpoint(page) {
  const paused = page.getByRole("button", { name: "已暂停，点击游戏画面继续", exact: true });
  await paused.waitFor({ state: "visible", timeout: 120_000 });
  await page.locator(".player-stage").dispatchEvent("click");
  await paused.waitFor({ state: "hidden", timeout: 10_000 });
}

async function waitForProductReady(page) {
  await page.mouse.move(720, 1);
  const saveButton = page.getByRole("button", { name: "创建存档", exact: true });
  await saveButton.waitFor({ state: "visible", timeout: 120_000 });
  await waitForEnabled(saveButton, 120_000);
}

async function waitForEnabled(button, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await button.isEnabled().catch(() => false)) {return;}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_SAVE_UNAVAILABLE");
}

async function screenshotEvidence(canvas, filename) {
  const layout = await canvasLayoutEvidence(canvas);
  if (!validCanvasLayout(layout)) {throw new Error("KIRIKIRI_ACCEPTANCE_CANVAS_LAYOUT_INVALID");}
  const capture = await canvas.evaluate(async (element) => {
    const pngDataUrl = element.toDataURL("image/png");
    const binary = atob(pngDataUrl.slice(pngDataUrl.indexOf(",") + 1));
    const png = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([png], { type: "image/png" }));
    const probe = document.createElement("canvas");
    probe.width = bitmap.width;
    probe.height = bitmap.height;
    const context = probe.getContext("2d", { willReadFrequently: true });
    if (!context) {throw new Error("KIRIKIRI_ACCEPTANCE_SCREENSHOT_CONTEXT");}
    context.drawImage(bitmap, 0, 0);
    bitmap.close();
    const { data, width, height } = context.getImageData(0, 0, probe.width, probe.height);
    let nonBlackPixels = 0;
    for (let offset = 0; offset < data.length; offset += 4) {
      if (data[offset] || data[offset + 1] || data[offset + 2]) {nonBlackPixels += 1;}
    }
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", data));
    return {
      pngDataUrl, width, height, nonBlackPixels,
      rgbaSha256: [...digest].map((byte) => byte.toString(16).padStart(2, "0")).join(""),
    };
  });
  const pngBase64 = capture.pngDataUrl.slice(capture.pngDataUrl.indexOf(",") + 1);
  writeFileSync(join(screenshotsDirectory, filename), Buffer.from(pngBase64, "base64"));
  return {
    width: capture.width,
    height: capture.height,
    nonBlackPixels: capture.nonBlackPixels,
    rgbaSha256: capture.rgbaSha256,
    ...layout,
  };
}

async function waitForMatchingScreenshot(canvas, filename, expectedSha256, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const frame = await screenshotEvidence(canvas, filename);
    if (frame.rgbaSha256 === expectedSha256) {return frame;}
    await canvas.page().waitForTimeout(100);
  }
  throw new Error("KIRIKIRI_ACCEPTANCE_RESTORE_POSITION_TIMEOUT");
}

async function canvasLayoutEvidence(canvas) {
  return canvas.evaluate((element) => {
    if (!(element instanceof HTMLCanvasElement) || element.id !== "canvas") {return null;}
    const surface = element.closest("[data-kirikiri-runtime-surface]");
    if (!(surface instanceof HTMLElement)) {return null;}
    const canvasRect = element.getBoundingClientRect();
    const surfaceRect = surface.getBoundingClientRect();
    return {
      backingWidth: element.width,
      backingHeight: element.height,
      centerOffsetXPx: Math.abs(
        canvasRect.left + canvasRect.width / 2 - surfaceRect.left - surfaceRect.width / 2,
      ),
      centerOffsetYPx: Math.abs(
        canvasRect.top + canvasRect.height / 2 - surfaceRect.top - surfaceRect.height / 2,
      ),
      focused: element.ownerDocument.activeElement === element,
      displayWidth: canvasRect.width,
      displayHeight: canvasRect.height,
    };
  });
}

function validCanvasLayout(layout) {
  return layout !== null && Number.isSafeInteger(layout.backingWidth) && layout.backingWidth > 0 &&
    Number.isSafeInteger(layout.backingHeight) && layout.backingHeight > 0 &&
    (layout.backingWidth !== 300 || layout.backingHeight !== 150) &&
    layout.displayWidth >= 64 && layout.displayHeight >= 64 &&
    Math.abs(layout.backingWidth / layout.backingHeight - layout.displayWidth / layout.displayHeight) <= 0.01 &&
    layout.centerOffsetXPx <= 1 && layout.centerOffsetYPx <= 1 && layout.focused === true;
}

function requireChanged(before, after, code) {
  if (before.rgbaSha256 === after.rgbaSha256) {throw new Error(code);}
}

function requireStatus(actual, expected, code) {
  if (actual !== expected) {throw new Error(code);}
}

async function withStableStage(action, fallbackCode) {
  try {
    return await action();
  } catch (error) {
    if (error instanceof Error && /^KIRIKIRI_[A-Z0-9_]+$/.test(error.message)) {throw error;}
    throw new Error(fallbackCode);
  }
}

function responseErrorCode(body) {
  try {
    const value = JSON.parse(body);
    return typeof value?.error?.code === "string" ? value.error.code : "";
  } catch {return "";}
}

function normalizedBaseUrl(value) {
  try {
    const url = new URL(value);
    if ((url.protocol !== "https:" &&
        (url.protocol !== "http:" || !isLocalAcceptanceHostname(url.hostname))) ||
      url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
      throw new Error("invalid");
    }
    return url.origin;
  } catch {throw new Error("KIRIKIRI_ACCEPTANCE_BASE_URL_INVALID");}
}

function stableErrorCode(error) {
  if (error instanceof Error && /^KIRIKIRI_[A-Z0-9_]+$/.test(error.message)) {return error.message;}
  return "KIRIKIRI_ACCEPTANCE_FAILED";
}

function writeEvidence(value) {
  writeFileSync(evidencePath, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
}
