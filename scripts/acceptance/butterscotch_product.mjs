#!/usr/bin/env node

import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { chromium } from "../../web/node_modules/playwright/index.mjs";

import {captureOptionalReviewScreenshot, resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

import {
  assertButterscotchProductEvidence,
  butterscotchProductStages,
} from "./butterscotch_product_contract.mjs";
import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";
import { createProductClient, singleFile } from "./rpgmaker_security_upload.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";

const caseId = "ACC-BUTTERSCOTCH-001";
const requiredEnvironment = [
  "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_BUTTERSCOTCH_SMOKE_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR",
];
const missing = requiredEnvironment.filter((name) => !process.env[name]);
const caseDirectory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".");
const evidencePath = join(caseDirectory, "butterscotch-product.json");

if (missing.length) {
  writeEvidence({
    schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "BUTTERSCOTCH_ACCEPTANCE_INPUT_REQUIRED",
  });
  process.stderr.write(`BUTTERSCOTCH_ACCEPTANCE_INPUT_REQUIRED:${missing.join(",")}\n`);
  process.exit(3);
}

const baseUrl = normalizedBaseUrl(process.env.RETROM_ACCEPTANCE_BASE_URL);
const screenshotsDirectory = join(caseDirectory, "screenshots");
mkdirSync(screenshotsDirectory, { recursive: true });
const localProxy = await localRpgAcceptanceProxy(baseUrl);
let browser;
let observedEvidence = null;
const runtimeDiagnostics = [];

try {
  browser = await chromium.launch({ executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true });
  const evidence = await runProductCase(browser);
  observedEvidence = evidence;
  assertButterscotchProductEvidence(evidence);
  writeEvidence(evidence);
  process.stdout.write(`${JSON.stringify(evidence)}\n`);
} catch (error) {
  const errorCode = stableErrorCode(error);
  writeEvidence({
    schemaVersion: 1, caseId, status: "FAIL", errorCode,
    runtimeDiagnostics,
    ...(observedEvidence ? { observedEvidence } : {}),
  });
  process.stderr.write(`${errorCode}\n`);
  process.exitCode = 1;
} finally {
  await browser?.close();
  await localProxy.close();
}

async function runProductCase(activeBrowser) {
  const context = await activeBrowser.newContext({
    viewport: { width: 1440, height: 1000 },
    ...localProxy.contextOptions,
  });
  await installVirtualStandardGamepad(context);
  const browserErrors = { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 };
  try {
    const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
      headers: { Origin: baseUrl },
      data: { username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD },
      failOnStatusCode: false,
    });
    requireStatus(loginResponse.status(), 200, "BUTTERSCOTCH_ACCEPTANCE_LOGIN_FAILED");
    const login = await loginResponse.json();
    const client = createProductClient(context, baseUrl, login.csrfToken);
    const platformInstanceId = await butterscotchPlatformInstance(client);
    const uploadId = await client.upload(
      singleFile(process.env.RETROM_BUTTERSCOTCH_SMOKE_ARCHIVE), "FILES", "PROJECT",
    );
    const importedResponse = await client.raw("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), timeout: 120_000,
      data: {
        uploadId, targetPlatformInstanceId: platformInstanceId, metadataProvider: "NONE",
        contentMode: "BUTTERSCOTCH_PROJECT", tagIds: [],
      },
    });
    requireStatus(importedResponse.status(), 202, "BUTTERSCOTCH_ACCEPTANCE_IMPORT_CREATE_FAILED");
    const imported = await importedResponse.json();
    await waitForImport(client, imported.importJobId);
    const review = await reviewForImport(client, imported.importJobId);

    const preview = await createPreview(client, review.itemId);
    const previewPage = await trackedPage(context, browserErrors);
    const previewResponses = trackProjectResponses(previewPage);
    await previewPage.goto(`${baseUrl}${preview.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const previewCanvas = await runtimeCanvas(previewPage);
    await captureOptionalReviewScreenshot(previewPage, preview.previewId);
    const previewFrame = await screenshotEvidence(previewCanvas, "preview.png");
    await previewPage.close();

    const approved = await approveReview(client, review.itemId);
    const original = await createLaunch(client, approved.gameId, null);
    const originalPage = await trackedPage(context, browserErrors);
    await originalPage.goto(`${baseUrl}${original.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const originalCanvas = await runtimeCanvas(originalPage);
    await waitForCheckpoint(originalPage);
    const beforeInput = await screenshotEvidence(originalCanvas, "product-before-input.png");
    await sendGamepadInput(originalCanvas);
    const afterInput = await screenshotEvidence(originalCanvas, "product-after-input.png");
    requireChanged(beforeInput, afterInput, "BUTTERSCOTCH_ACCEPTANCE_GAMEPAD_INPUT_UNOBSERVED");
    const saved = await createCheckpoint(originalPage, original.launchId);
    await originalPage.close();

    const restored = await createLaunch(client, approved.gameId, saved.saveStateId);
    if (restored.launchId === original.launchId) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_RESTORE_LAUNCH_REUSED");}
    const restoredPage = await trackedPage(context, browserErrors);
    const restoreResponses = trackProjectResponses(restoredPage);
    const stateResponsePromise = restoredPage.waitForResponse((response) =>
      response.request().method() === "GET" && response.url().endsWith(`/runtime/launches/${restored.launchId}/state`),
    { timeout: 120_000 });
    await restoredPage.goto(`${baseUrl}${restored.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const restoredCanvas = await runtimeCanvas(restoredPage);
    await waitForCheckpoint(restoredPage);
    const stateResponse = await stateResponsePromise;
    requireStatus(stateResponse.status(), 200, "BUTTERSCOTCH_ACCEPTANCE_RESTORE_PAYLOAD_FAILED");
    const restoredFrame = await screenshotEvidence(restoredCanvas, "restored.png");
    await sendGamepadInput(restoredCanvas);
    const postRestoreFrame = await screenshotEvidence(restoredCanvas, "post-restore-input.png");
    requireChanged(restoredFrame, postRestoreFrame, "BUTTERSCOTCH_ACCEPTANCE_RESTORE_INPUT_UNOBSERVED");
    await restoredPage.close();
    if (Object.values(browserErrors).some((count) => count !== 0)) {
      throw new Error("BUTTERSCOTCH_ACCEPTANCE_BROWSER_ERROR");
    }

    const contentDigest = projectIdentity(previewResponses.urls);
    if (projectIdentity(restoreResponses.urls) !== contentDigest) {
      throw new Error("BUTTERSCOTCH_ACCEPTANCE_CONTENT_IDENTITY_CHANGED");
    }
    return {
      schemaVersion: 1,
      caseId,
      status: "PASS",
      stages: [...butterscotchProductStages],
      ids: {
        importItemId: review.itemId, gameId: approved.gameId, saveStateId: saved.saveStateId,
        originalLaunchId: original.launchId, restoreLaunchId: restored.launchId,
      },
      checkpoint: { format: saved.checkpointFormat, sizeBytes: Number(stateResponse.headers()["content-length"]) },
      cache: {
        contentDigest,
        firstDataWinResponseCount: countProjectFile(previewResponses.urls, "data.win"),
        restoreDataWinResponseCount: countProjectFile(restoreResponses.urls, "data.win"),
        restoreIndexResponseCount: countProjectFile(restoreResponses.urls, "index.json"),
      },
      screenshots: {
        preview: previewFrame, productBeforeInput: beforeInput, productAfterInput: afterInput,
        restored: restoredFrame, postRestoreInput: postRestoreFrame,
      },
      browser: browserErrors,
    };
  } finally {
    await context.close();
  }
}

async function butterscotchPlatformInstance(client) {
  let response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=butterscotch&limit=100");
  let found = response.items?.find((item) => item.enabled && item.defaultCoreId === "butterscotch");
  if (!found) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=butterscotch&limit=100");
    found = response.items?.find((item) => item.enabled && item.defaultCoreId === "butterscotch");
  }
  const identifier = found?.id ?? found?.platformInstanceId;
  if (typeof identifier !== "string") {throw new Error("BUTTERSCOTCH_ACCEPTANCE_PLATFORM_MISSING");}
  return identifier;
}

async function waitForImport(client, importJobId) {
  for (let attempt = 0; attempt < 1_200; attempt += 1) {
    const job = await client.json("GET", `/api/v1/admin/imports/${importJobId}`);
    if (["REVIEW_PENDING", "COMPLETE", "COMPLETED"].includes(job.state)) {return;}
    if (["FAILED", "CANCELLED"].includes(job.state)) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_IMPORT_FAILED");}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("BUTTERSCOTCH_ACCEPTANCE_IMPORT_TIMEOUT");
}

async function reviewForImport(client, importJobId) {
  const queue = await client.json("GET", `/api/v1/admin/reviews?importJobId=${importJobId}&limit=20`);
  if (queue.items?.length !== 1) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_REVIEW_CARDINALITY");}
  return client.json("GET", `/api/v1/admin/reviews/${queue.items[0].itemId}`);
}

async function createPreview(client, itemId) {
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/previews`, {
    headers: client.writeHeaders(),
    data: { clientCapabilities: capabilities() },
  });
  requireStatus(response.status(), 201, "BUTTERSCOTCH_ACCEPTANCE_PREVIEW_CREATE_FAILED");
  return response.json();
}

async function approveReview(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  requireStatus(snapshot.status(), 200, "BUTTERSCOTCH_ACCEPTANCE_REVIEW_READ_FAILED");
  const etag = snapshot.headers().etag;
  if (!etag) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_REVIEW_ETAG_MISSING");}
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: { ...client.writeHeaders(), "If-Match": etag }, data: {},
  });
  requireStatus(response.status(), 201, "BUTTERSCOTCH_ACCEPTANCE_APPROVE_FAILED");
  return response.json();
}

async function createLaunch(client, gameId, saveStateId) {
  return client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: {
      gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
      clientCapabilities: capabilities(),
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

function trackProjectResponses(page) {
  const urls = [];
  page.on("response", (response) => {
    if (response.request().method() === "GET" && response.status() === 200 &&
        new URL(response.url()).pathname.startsWith("/runtime/content/project/")) {
      urls.push(response.url());
    }
  });
  return { urls };
}

async function runtimeCanvas(page) {
  const deadline = Date.now() + 120_000;
  let lastObservation = {canvasVisible: false};
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator('canvas[aria-label="Butterscotch game"]').first();
      if (!await canvas.isVisible().catch(() => false)) {continue;}
      const layout = await canvasLayoutEvidence(canvas).catch((error) => {
        lastObservation = {canvasVisible: true, error: error.message};
        return null;
      });
      if (layout !== null) {lastObservation = {canvasVisible: true, layout};}
      if (validCanvasLayout(layout)) {return canvas;}
    }
    await page.waitForTimeout(100);
  }
  runtimeDiagnostics.push(lastObservation);
  throw new Error("BUTTERSCOTCH_ACCEPTANCE_CANVAS_LAYOUT_INVALID");
}

async function sendGamepadInput(canvas) {
  await canvas.evaluate((element) => {element.tabIndex = 0; element.focus();});
  for (const input of [
    { axis: 1, value: 1 }, { axis: 1, value: 0 }, { button: 0, pressed: true }, { button: 0, pressed: false },
  ]) {
    await canvas.evaluate((element, next) => {
      const gamepad = element.ownerDocument.defaultView?.__retromTestGamepad;
      if ("axis" in next) {gamepad?.axis(next.axis, next.value);}
      else {gamepad?.button(next.button, next.pressed);}
    }, input);
    await canvas.page().waitForTimeout(300);
  }
}

async function waitForCheckpoint(page) {
  await revealPreviewToolbar(page);
  const button = page.getByRole("button", { name: "创建存档", exact: true });
  await button.waitFor({ state: "visible", timeout: 120_000 });
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    if (await button.isEnabled().catch(() => false)) {
      await resumePreview(page);
      await runtimeCanvas(page);
      return;
    }
    await page.waitForTimeout(100);
  }
  throw new Error("BUTTERSCOTCH_ACCEPTANCE_SAVE_UNAVAILABLE");
}

async function createCheckpoint(page, launchId) {
  await revealPreviewToolbar(page);
  const button = page.getByRole("button", { name: "创建存档", exact: true });
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && response.url().includes(`/runtime/launches/${launchId}/save-states`),
  { timeout: 120_000 });
  await button.click();
  const response = await responsePromise;
  requireStatus(response.status(), 201, "BUTTERSCOTCH_ACCEPTANCE_SAVE_FAILED");
  return response.json();
}

async function screenshotEvidence(canvas, filename) {
  const layout = await canvasLayoutEvidence(canvas);
  if (!validCanvasLayout(layout)) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_CANVAS_LAYOUT_INVALID");}
  const screenshot = await canvas.screenshot({ type: "png", path: join(screenshotsDirectory, filename) });
  const pixels = await canvas.page().evaluate(async (encoded) => {
    const binary = atob(encoded);
    const png = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([png], { type: "image/png" }));
    const probe = document.createElement("canvas");
    probe.width = bitmap.width;
    probe.height = bitmap.height;
    const context = probe.getContext("2d", { willReadFrequently: true });
    if (!context) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_SCREENSHOT_CONTEXT");}
    context.drawImage(bitmap, 0, 0);
    bitmap.close();
    const { data, width, height } = context.getImageData(0, 0, probe.width, probe.height);
    let nonBlackPixels = 0;
    for (let offset = 0; offset < data.length; offset += 4) {
      if (data[offset] || data[offset + 1] || data[offset + 2]) {nonBlackPixels += 1;}
    }
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", data));
    return {
      width, height, nonBlackPixels,
      rgbaSha256: [...digest].map((byte) => byte.toString(16).padStart(2, "0")).join(""),
    };
  }, screenshot.toString("base64"));
  return { ...pixels, ...layout };
}

async function canvasLayoutEvidence(canvas) {
  return canvas.evaluate((element) => {
    if (!(element instanceof HTMLCanvasElement)) {return null;}
    const surface = element.closest("[data-butterscotch-runtime-surface]");
    if (!(surface instanceof HTMLElement)) {return null;}
    const canvasRect = element.getBoundingClientRect();
    const surfaceRect = surface.getBoundingClientRect();
    return {
      backingWidth: element.width,
      backingHeight: element.height,
      centerOffsetXPx: Math.abs(canvasRect.left + canvasRect.width / 2 - surfaceRect.left - surfaceRect.width / 2),
      centerOffsetYPx: Math.abs(canvasRect.top + canvasRect.height / 2 - surfaceRect.top - surfaceRect.height / 2),
      focused: element.ownerDocument.activeElement === element,
      displayWidth: canvasRect.width,
      displayHeight: canvasRect.height,
      surfaceWidth: surfaceRect.width,
      surfaceHeight: surfaceRect.height,
    };
  });
}

function validCanvasLayout(layout) {
  return layout !== null && Number.isSafeInteger(layout.backingWidth) && layout.backingWidth >= 64 &&
    Number.isSafeInteger(layout.backingHeight) && layout.backingHeight >= 64 &&
    layout.displayWidth >= 64 && layout.displayHeight >= 64 &&
    layout.surfaceWidth >= layout.displayWidth && layout.surfaceHeight >= layout.displayHeight &&
    (layout.surfaceWidth - layout.displayWidth <= 1 || layout.surfaceHeight - layout.displayHeight <= 1) &&
    Math.abs(layout.backingWidth / layout.backingHeight - layout.displayWidth / layout.displayHeight) <= 0.01 &&
    layout.centerOffsetXPx <= 1 && layout.centerOffsetYPx <= 1 && layout.focused === true;
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
        axes: state.axes, buttons: state.buttons, connected: true,
        id: "Retrom acceptance standard gamepad", index: 0, mapping: "standard", timestamp: performance.now(),
      }],
    });
    globalThis.__retromTestGamepad = {
      axis(index, value) {state.axes[index] = value;},
      button(index, pressed) {state.buttons[index] = { pressed, touched: pressed, value: pressed ? 1 : 0 };},
    };
  });
}

function projectIdentity(urls) {
  const values = new Set(urls.map((value) => /^\/runtime\/content\/project\/([0-9a-f]{64})\//u.exec(
    new URL(value).pathname,
  )?.[1]).filter(Boolean));
  if (values.size !== 1) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_CONTENT_IDENTITY_INVALID");}
  return [...values][0];
}

function countProjectFile(urls, filename) {
  return urls.filter((value) => new URL(value).pathname.endsWith(`/${filename}`)).length;
}

function requireChanged(before, after, code) {
  if (before.rgbaSha256 === after.rgbaSha256) {throw new Error(code);}
}

function requireStatus(actual, expected, code) {
  if (actual !== expected) {throw new Error(code);}
}

function capabilities() {
  return { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true };
}

function normalizedBaseUrl(value) {
  try {
    const url = new URL(value);
    if ((url.protocol !== "https:" && (url.protocol !== "http:" || !isLocalAcceptanceHostname(url.hostname))) ||
      url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
      throw new Error("invalid");
    }
    return url.origin;
  } catch {throw new Error("BUTTERSCOTCH_ACCEPTANCE_BASE_URL_INVALID");}
}

function stableErrorCode(error) {
  if (error instanceof Error && /^BUTTERSCOTCH_[A-Z0-9_]+$/u.test(error.message)) {return error.message;}
  return "BUTTERSCOTCH_ACCEPTANCE_FAILED";
}

function writeEvidence(value) {
  writeFileSync(evidencePath, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
}
