#!/usr/bin/env node

import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { chromium } from "../../web/node_modules/playwright/index.mjs";

import { assertOnsProductEvidence, onsProductStages } from "./ons_product_contract.mjs";
import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";
import { createProductClient, singleFile } from "./rpgmaker_security_upload.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";
import { trackRuntimeLoading } from "./runtime_loading_evidence.mjs";

const caseId = "ACC-ONS-001";
const requiredEnvironment = [
  "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_ONS_SMOKE_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR",
];
const missing = requiredEnvironment.filter((name) => !process.env[name]);
const caseDirectory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".");
const evidencePath = join(caseDirectory, "ons-product.json");

if (missing.length) {
  writeEvidence({ schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "ONS_ACCEPTANCE_INPUT_REQUIRED" });
  process.stderr.write(`ONS_ACCEPTANCE_INPUT_REQUIRED:${missing.join(",")}\n`);
  process.exit(3);
}

const baseUrl = normalizedBaseUrl(process.env.RETROM_ACCEPTANCE_BASE_URL);
const screenshotsDirectory = join(caseDirectory, "screenshots");
mkdirSync(screenshotsDirectory, { recursive: true });
const localProxy = await localRpgAcceptanceProxy(baseUrl);

let browser;
let observedEvidence = null;
try {
  browser = await chromium.launch({ executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true });
  const evidence = await runProductCase(browser);
  observedEvidence = evidence;
  assertOnsProductEvidence(evidence);
  writeEvidence(evidence);
  process.stdout.write(`${JSON.stringify(evidence)}\n`);
} catch (error) {
  const errorCode = stableErrorCode(error);
  writeEvidence({
    schemaVersion: 1, caseId, status: "FAIL", errorCode,
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
  const browserErrors = { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 };
  try {
    const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
      headers: { Origin: baseUrl },
      data: { username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD },
      failOnStatusCode: false,
    });
    requireStatus(loginResponse.status(), 200, "ONS_ACCEPTANCE_LOGIN_FAILED");
    const login = await loginResponse.json();
    const client = createProductClient(context, baseUrl, login.csrfToken);
    const platformInstanceId = await onsPlatformInstance(client);
    const uploadId = await client.upload(singleFile(process.env.RETROM_ONS_SMOKE_ARCHIVE), "FILES", "ONS_PROJECT");
    const importedResponse = await client.raw("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(),
      timeout: 120_000,
      data: {
        uploadId, targetPlatformInstanceId: platformInstanceId, metadataProvider: "NONE",
        contentMode: "ONS_PROJECT_V1", tagIds: [],
      },
    });
    requireStatus(importedResponse.status(), 202, "ONS_ACCEPTANCE_IMPORT_CREATE_FAILED");
    const imported = await importedResponse.json();
    await waitForImport(client, imported.importJobId);
    const review = await reviewForImport(client, imported.importJobId);

    const preview = await createPreview(client, review.itemId);
    const previewPage = await trackedPage(context, browserErrors);
    await previewPage.goto(`${baseUrl}${preview.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const previewCanvas = await runtimeCanvas(previewPage);
    await sendKeys(previewPage, previewCanvas, ["Enter", "ArrowDown", "Enter"]);
    await previewPage.getByText("第 5 秒运行截图已保存；可以继续试玩。").waitFor({ timeout: 120_000 });
    const previewFrame = await screenshotEvidence(previewCanvas, "preview.png");
    await previewPage.close();

    const approved = await approveReview(client, review.itemId);
    const original = await createLaunch(client, approved.gameId, null);
    const originalPage = await trackedPage(context, browserErrors);
    const originalLoadingProbe = trackRuntimeLoading(originalPage);
    await originalPage.goto(`${baseUrl}${original.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const originalCanvas = await runtimeCanvas(originalPage);
    const firstVisibleLoading = await originalLoadingProbe.snapshot();
    const beforeInput = await screenshotEvidence(originalCanvas, "product-before-input.png");
    await sendKeys(originalPage, originalCanvas, ["Enter", "ArrowDown", "Enter", "ArrowRight"]);
    const afterInput = await screenshotEvidence(originalCanvas, "product-after-input.png");
    requireChanged(beforeInput, afterInput, "ONS_ACCEPTANCE_PRODUCT_INPUT_UNOBSERVED");
    const saved = await createCheckpoint(originalPage, original.launchId);
    originalLoadingProbe.stop();
    await originalPage.close();

    const restored = await createLaunch(client, approved.gameId, saved.saveStateId);
    if (restored.launchId === original.launchId) {throw new Error("ONS_ACCEPTANCE_RESTORE_LAUNCH_REUSED");}
    const restoredPage = await trackedPage(context, browserErrors);
    const restoreLoadingProbe = trackRuntimeLoading(restoredPage);
    const stateResponsePromise = restoredPage.waitForResponse((response) =>
      response.request().method() === "GET" && response.url().endsWith(`/runtime/launches/${restored.launchId}/state`),
    { timeout: 120_000 });
    await restoredPage.goto(`${baseUrl}${restored.playUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
    const restoredCanvas = await runtimeCanvas(restoredPage);
    const restoreVisibleLoading = await restoreLoadingProbe.snapshot();
    const stateResponse = await stateResponsePromise;
    requireStatus(stateResponse.status(), 200, "ONS_ACCEPTANCE_RESTORE_PAYLOAD_FAILED");
    const payloadSize = Number(stateResponse.headers()["content-length"]);
    const restoredFrame = await screenshotEvidence(restoredCanvas, "restored.png");
    await sendKeys(restoredPage, restoredCanvas, ["ArrowLeft", "Enter"]);
    const postRestoreFrame = await screenshotEvidence(restoredCanvas, "post-restore-input.png");
    requireChanged(restoredFrame, postRestoreFrame, "ONS_ACCEPTANCE_RESTORE_INPUT_UNOBSERVED");
    restoreLoadingProbe.stop();
    await restoredPage.close();
    if (Object.values(browserErrors).some((count) => count !== 0)) {throw new Error("ONS_ACCEPTANCE_BROWSER_ERROR");}

    return {
      schemaVersion: 1,
      caseId,
      status: "PASS",
      stages: [...onsProductStages],
      ids: {
        importItemId: review.itemId, gameId: approved.gameId, saveStateId: saved.saveStateId,
        originalLaunchId: original.launchId, restoreLaunchId: restored.launchId,
      },
      checkpoint: { format: saved.checkpointFormat, sizeBytes: payloadSize },
      loading: {
        schemaVersion: 1,
        sameProjectContentIdentity: firstVisibleLoading.projectContentIdentity !== null &&
          firstVisibleLoading.projectContentIdentity === restoreVisibleLoading.projectContentIdentity,
        firstVisible: firstVisibleLoading.evidence,
        restoreVisible: restoreVisibleLoading.evidence,
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

async function onsPlatformInstance(client) {
  let response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=ons&limit=100");
  let found = response.items?.find((item) => item.enabled && item.defaultCoreId === "onscripter_yuri");
  if (!found) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=ons&limit=100");
    found = response.items?.find((item) => item.enabled && item.defaultCoreId === "onscripter_yuri");
  }
  const identifier = found?.id ?? found?.platformInstanceId;
  if (typeof identifier !== "string") {throw new Error("ONS_ACCEPTANCE_PLATFORM_MISSING");}
  return identifier;
}

async function waitForImport(client, importJobId) {
  for (let attempt = 0; attempt < 1_200; attempt += 1) {
    const job = await client.json("GET", `/api/v1/admin/imports/${importJobId}`);
    if (["REVIEW_PENDING", "COMPLETE", "COMPLETED"].includes(job.state)) {return;}
    if (["FAILED", "CANCELLED"].includes(job.state)) {throw new Error("ONS_ACCEPTANCE_IMPORT_FAILED");}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("ONS_ACCEPTANCE_IMPORT_TIMEOUT");
}

async function reviewForImport(client, importJobId) {
  const queue = await client.json("GET", `/api/v1/admin/reviews?importJobId=${importJobId}&limit=20`);
  if (queue.items?.length !== 1) {throw new Error("ONS_ACCEPTANCE_REVIEW_CARDINALITY");}
  return client.json("GET", `/api/v1/admin/reviews/${queue.items[0].itemId}`);
}

async function createPreview(client, itemId) {
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/previews`, {
    headers: client.writeHeaders(),
    data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
  });
  requireStatus(response.status(), 201, "ONS_ACCEPTANCE_PREVIEW_CREATE_FAILED");
  return response.json();
}

async function approveReview(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  requireStatus(snapshot.status(), 200, "ONS_ACCEPTANCE_REVIEW_READ_FAILED");
  const etag = snapshot.headers().etag;
  if (!etag) {throw new Error("ONS_ACCEPTANCE_REVIEW_ETAG_MISSING");}
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: { ...client.writeHeaders(), "If-Match": etag }, data: {},
  });
  requireStatus(response.status(), 201, "ONS_ACCEPTANCE_APPROVE_FAILED");
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
      const layout = await canvasLayoutEvidence(canvas).catch(() => null);
      if (validCanvasLayout(layout)) {return canvas;}
    }
    await page.waitForTimeout(100);
  }
  throw new Error("ONS_ACCEPTANCE_CANVAS_LAYOUT_INVALID");
}

async function sendKeys(page, canvas, keys) {
  await canvas.evaluate((element) => {element.tabIndex = 0; element.focus();});
  for (const key of keys) {await page.keyboard.press(key); await page.waitForTimeout(500);}
}

async function createCheckpoint(page, launchId) {
  await page.mouse.move(720, 1);
  const saveButton = page.getByRole("button", { name: "创建存档", exact: true });
  await saveButton.waitFor({ state: "visible", timeout: 120_000 });
  if (!await saveButton.isEnabled()) {throw new Error("ONS_ACCEPTANCE_SAVE_UNAVAILABLE");}
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && response.url().includes(`/runtime/launches/${launchId}/save-states`),
  { timeout: 120_000 });
  await saveButton.click();
  const response = await responsePromise;
  requireStatus(response.status(), 201, "ONS_ACCEPTANCE_SAVE_FAILED");
  return response.json();
}

async function screenshotEvidence(canvas, filename) {
  const layout = await canvasLayoutEvidence(canvas);
  if (!validCanvasLayout(layout)) {throw new Error("ONS_ACCEPTANCE_CANVAS_LAYOUT_INVALID");}
  const screenshot = await canvas.screenshot({ type: "png", path: join(screenshotsDirectory, filename) });
  const pixels = await canvas.page().evaluate(async (encoded) => {
    const binary = atob(encoded);
    const png = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([png], { type: "image/png" }));
    const probe = document.createElement("canvas");
    probe.width = bitmap.width;
    probe.height = bitmap.height;
    const context = probe.getContext("2d", { willReadFrequently: true });
    if (!context) {throw new Error("ONS_ACCEPTANCE_SCREENSHOT_CONTEXT");}
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
    if (!(element instanceof HTMLCanvasElement) || element.id !== "canvas") {return null;}
    const surface = element.closest("[data-ons-runtime-surface]");
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

function normalizedBaseUrl(value) {
  try {
    const url = new URL(value);
    if ((url.protocol !== "https:" &&
        (url.protocol !== "http:" || !isLocalAcceptanceHostname(url.hostname))) ||
      url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
      throw new Error("invalid");
    }
    return url.origin;
  } catch {throw new Error("ONS_ACCEPTANCE_BASE_URL_INVALID");}
}

function stableErrorCode(error) {
  if (error instanceof Error && /^ONS_[A-Z0-9_]+$/.test(error.message)) {return error.message;}
  return "ONS_ACCEPTANCE_FAILED";
}

function writeEvidence(value) {
  writeFileSync(evidencePath, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
}
