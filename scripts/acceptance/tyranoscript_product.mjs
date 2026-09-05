#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { chromium } from "../../web/node_modules/playwright/index.mjs";

import {captureOptionalReviewScreenshot, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";

import {
  assertTyranoScriptProductEvidence,
  tyranoScriptProductStages,
} from "./tyranoscript_product_contract.mjs";
import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";
import { requireLocalRuntimeSite } from "./rpgmaker_security_runtime.mjs";
import { createProductClient, singleFile } from "./rpgmaker_security_upload.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";

const caseId = "ACC-TYRANOSCRIPT-001";
const requiredEnvironment = [
  "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_TYRANOSCRIPT_SMOKE_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR",
];
const missing = requiredEnvironment.filter((name) => !process.env[name]);
const caseDirectory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".");
const evidencePath = join(caseDirectory, "tyranoscript-product.json");

if (missing.length) {
  writeEvidence({schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "TYRANOSCRIPT_ACCEPTANCE_INPUT_REQUIRED"});
  process.stderr.write(`TYRANOSCRIPT_ACCEPTANCE_INPUT_REQUIRED:${missing.join(",")}\n`);
  process.exit(3);
}

const baseUrl = normalizedBaseUrl(process.env.RETROM_ACCEPTANCE_BASE_URL);
const screenshotsDirectory = join(caseDirectory, "screenshots");
mkdirSync(screenshotsDirectory, {recursive: true});
const localProxy = await localRpgAcceptanceProxy(baseUrl);
let browser;
let observedEvidence = null;

try {
  browser = await chromium.launch({
    args: ["--disable-gpu"], executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true,
  });
  const evidence = await runProductCase(browser);
  observedEvidence = evidence;
  assertTyranoScriptProductEvidence(evidence);
  writeEvidence(evidence);
  process.stdout.write(`${JSON.stringify(evidence)}\n`);
} catch (error) {
  const errorCode = stableErrorCode(error);
  debugAcceptance(error instanceof Error ? error.stack ?? error.message : String(error));
  writeEvidence({schemaVersion: 1, caseId, status: "FAIL", errorCode, ...(observedEvidence ? {observedEvidence} : {})});
  process.stderr.write(`${errorCode}\n`);
  process.exitCode = 1;
} finally {
  await browser?.close();
  await localProxy.close();
}

async function runProductCase(activeBrowser) {
  const context = await activeBrowser.newContext({viewport: {width: 1440, height: 1000}, ...localProxy.contextOptions});
  await installVirtualStandardGamepad(context);
  await context.addInitScript((enabled) => {globalThis.__retromRuntimeDebug = enabled;},
    process.env.RETROM_ACCEPTANCE_DEBUG === "1");
  const browserEvidence = {pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0, ignoredSandboxAlertCount: 0};
  const resources = {engineAsset200Count: 0, failedResponseCount: 0};
  try {
    const client = await authenticatedClient(context);
    const platformInstanceId = await tyranoScriptPlatformInstance(client);
    const uploadId = await client.upload(
      singleFile(process.env.RETROM_TYRANOSCRIPT_SMOKE_ARCHIVE), "FILES", "PROJECT",
    );
    const imported = await client.json("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), expected: 202, timeout: 120_000,
      data: {
        uploadId, targetPlatformInstanceId: platformInstanceId, metadataProvider: "NONE",
        contentMode: "TYRANOSCRIPT_PROJECT", tagIds: [],
      },
    });
    await waitForImport(client, imported.importJobId);
    const review = await reviewForImport(client, imported.importJobId);

    const preview = await createPreview(client, review.itemId);
    const previewPage = await trackedPage(context, browserEvidence, resources);
    const previewConfigPromise = waitForConfig(previewPage, preview.previewId);
    await previewPage.goto(`${baseUrl}${preview.playUrl}`, {waitUntil: "domcontentloaded", timeout: 120_000});
    requireTyranoScriptRuntimeSite(await previewConfigPromise);
    try {
      await waitForPreviewCapture(client, review.itemId, previewPage, preview.previewId);
    } catch (error) {
      debugAcceptance(`page:closed=${previewPage.isClosed()}:url=${previewPage.url()}`);
      debugAcceptance(`preview:${await previewPage.locator("body").innerText().catch((reason) =>
        `unavailable:${reason instanceof Error ? reason.message : String(reason)}`)}`);
      throw error;
    }
    const previewSurface = await tyranoSurface(previewPage);
    const previewScreenshot = await screenshotEvidence(previewSurface, "preview");
    await previewPage.close();

    const approved = await approveReview(client, review.itemId);
    const original = await createLaunch(client, approved.gameId, null);
    const originalPage = await trackedPage(context, browserEvidence, resources);
    const configPromise = waitForConfig(originalPage, original.launchId);
    await originalPage.goto(`${baseUrl}${original.playUrl}`, {waitUntil: "domcontentloaded", timeout: 120_000});
    const contentDigest = requireTyranoScriptRuntimeSite(await configPromise).contentDigest;
    await waitForCheckpoint(originalPage);
    const originalSurface = await tyranoSurface(originalPage);
    await originalSurface.evaluate(() => {window.TYRANO.kag.stat.f.__retrom_checkpoint_marker = "B";});
    const stateB = await engineState(originalSurface);
    const productScreenshot = await screenshotEvidence(originalSurface, "product");
    const saved = await createCheckpoint(originalPage, original.launchId);
    await originalSurface.evaluate(() => {window.TYRANO.kag.stat.f.__retrom_checkpoint_marker = "C";});
    const stateC = await engineState(originalSurface);
    await resumeAfterCheckpoint(originalPage);
    requireGamepadB(await sendGamepadInput(originalSurface));
    await originalPage.close();

    const restored = await createLaunch(client, approved.gameId, saved.saveStateId);
    if (restored.launchId === original.launchId) {throw new Error("TYRANOSCRIPT_ACCEPTANCE_RESTORE_LAUNCH_REUSED");}
    const restoredPage = await trackedPage(context, browserEvidence, resources);
    const stateResponsePromise = restoredPage.waitForResponse((response) =>
      response.request().method() === "GET" && response.url().endsWith(`/runtime/launches/${restored.launchId}/state`),
    {timeout: 120_000});
    await restoredPage.goto(`${baseUrl}${restored.playUrl}`, {waitUntil: "domcontentloaded", timeout: 120_000});
    const stateResponse = await stateResponsePromise;
    requireStatus(stateResponse.status(), 200, "TYRANOSCRIPT_ACCEPTANCE_RESTORE_PAYLOAD_FAILED");
    const restoredSurface = await tyranoSurface(restoredPage);
    const restoredState = await waitForRestoredState(restoredSurface);
    const restoredScreenshot = await screenshotEvidence(restoredSurface, "restored");
    requireGamepadB(await sendGamepadInput(restoredSurface));
    await restoredPage.close();

    const evidence = {
      schemaVersion: 1, caseId, status: "PASS", stages: [...tyranoScriptProductStages],
      ids: {
        importItemId: review.itemId, gameId: approved.gameId, saveStateId: saved.saveStateId,
        originalLaunchId: original.launchId, restoreLaunchId: restored.launchId,
      },
      checkpoint: {format: saved.checkpointFormat, sizeBytes: Number(stateResponse.headers()["content-length"])},
      state: {b: stateB, c: stateC, restoredB: restoredState},
      resources: {...resources, contentDigest},
      screenshots: {preview: previewScreenshot, product: productScreenshot, restored: restoredScreenshot},
      browser: browserEvidence,
    };
    observedEvidence = evidence;
    return evidence;
  } finally {
    await context.close();
  }
}

async function authenticatedClient(context) {
  const response = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: {Origin: baseUrl},
    data: {username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD},
    failOnStatusCode: false,
  });
  requireStatus(response.status(), 200, "TYRANOSCRIPT_ACCEPTANCE_LOGIN_FAILED");
  return createProductClient(context, baseUrl, (await response.json()).csrfToken);
}

async function tyranoScriptPlatformInstance(client) {
  let response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=tyranoscript&limit=100");
  let found = response.items?.find((item) => item.enabled && item.defaultCoreId === "tyranoscript");
  if (!found) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), expected: 200, data: {},
    });
    response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=tyranoscript&limit=100");
    found = response.items?.find((item) => item.enabled && item.defaultCoreId === "tyranoscript");
  }
  const identifier = found?.id ?? found?.platformInstanceId;
  if (typeof identifier !== "string") {throw new Error("TYRANOSCRIPT_ACCEPTANCE_PLATFORM_MISSING");}
  return identifier;
}

async function waitForImport(client, importJobId) {
  for (let attempt = 0; attempt < 6_000; attempt += 1) {
    const job = await client.json("GET", `/api/v1/admin/imports/${importJobId}`);
    if (["REVIEW_PENDING", "COMPLETE", "COMPLETED"].includes(job.state)) {return;}
    if (["FAILED", "CANCELLED"].includes(job.state)) {throw new Error("TYRANOSCRIPT_ACCEPTANCE_IMPORT_FAILED");}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("TYRANOSCRIPT_ACCEPTANCE_IMPORT_TIMEOUT");
}

async function reviewForImport(client, importJobId) {
  const queue = await client.json("GET", `/api/v1/admin/reviews?importJobId=${importJobId}&limit=20`);
  if (queue.items?.length !== 1) {throw new Error("TYRANOSCRIPT_ACCEPTANCE_REVIEW_CARDINALITY");}
  return client.json("GET", `/api/v1/admin/reviews/${queue.items[0].itemId}`);
}

async function createPreview(client, itemId) {
  return client.json("POST", `/api/v1/admin/reviews/${itemId}/previews`, {
    headers: client.writeHeaders(), expected: 201, data: {clientCapabilities: capabilities()},
  });
}

async function waitForPreviewCapture(client, itemId, page, previewId) {
  await captureOptionalReviewScreenshot(page, previewId);
  const review = await client.json("GET", `/api/v1/admin/reviews/${itemId}`);
  if (!review.runtimeScreenshot) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_PREVIEW_CAPTURE_MISSING");
  }
}

async function approveReview(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  requireStatus(snapshot.status(), 200, "TYRANOSCRIPT_ACCEPTANCE_REVIEW_READ_FAILED");
  const etag = snapshot.headers().etag;
  if (!etag) {throw new Error("TYRANOSCRIPT_ACCEPTANCE_REVIEW_ETAG_MISSING");}
  const review = await snapshot.json();
  const duplicateGames = Array.isArray(review.duplicateGames) ? review.duplicateGames : [];
  const data = duplicateGames.length ? {
    duplicatePolicy: "ALLOW_NEW",
    acknowledgedGameIds: duplicateGames.map((game) => game.gameId),
  } : {};
  return client.json("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": etag}, expected: 201, data,
  });
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

async function trackedPage(context, browserEvidence, resources) {
  const page = await context.newPage();
  page.on("close", () => {debugAcceptance("page:close");});
  page.on("crash", () => {debugAcceptance("page:crash");});
  page.on("pageerror", (error) => {
    browserEvidence.pageErrorCount += 1;
    debugAcceptance(`pageerror:${error.message}`);
  });
  page.on("dialog", async (dialog) => {browserEvidence.dialogCount += 1; await dialog.dismiss();});
  page.on("console", (message) => {
    if (message.type() !== "error") {
      debugAcceptance(`console:${message.type()}:${message.text()}`);
      return;
    }
    if (message.text().startsWith("Ignored call to 'alert()'. The document is sandboxed")) {
      browserEvidence.ignoredSandboxAlertCount += 1;
      return;
    }
    browserEvidence.consoleErrorCount += 1;
    debugAcceptance(`console:${message.text()}`);
  });
  page.on("response", (response) => {
    const path = new URL(response.url()).pathname;
    if (response.status() === 200 && path.startsWith("/__retrom/tyranoscript/project/data/")) {
      resources.engineAsset200Count += 1;
    }
    if (response.status() >= 400 &&
        (path.startsWith("/__retrom/tyranoscript/") || path.startsWith("/runtime/content/project/"))) {
      resources.failedResponseCount += 1;
      const failedUrl = new URL(response.url());
      debugAcceptance(`response:${response.status()}:${failedUrl.pathname}${failedUrl.search}`);
    }
  });
  return page;
}

async function tyranoSurface(page) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const frame = page.frames().find((candidate) => {
      try {return new URL(candidate.url()).pathname === "/__retrom/tyranoscript/entry";} catch {return false;}
    });
    if (frame) {
      const surface = frame.locator("#tyrano_base");
      if (await surface.isVisible().catch(() => false)) {return surface;}
    }
    await page.waitForTimeout(100);
  }
  debugAcceptance(`surface-page:${page.url()}:${await page.locator("body").innerText().catch((error) =>
    `unavailable:${error instanceof Error ? error.message : String(error)}`)}`);
  debugAcceptance(`surface-frames:${page.frames().map((frame) => frame.url()).join("|")}`);
  throw new Error("TYRANOSCRIPT_ACCEPTANCE_SURFACE_MISSING");
}

async function waitForRestoredState(surface) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const state = await engineState(surface).catch(() => null);
    if (state?.marker === "B") {return state;}
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("TYRANOSCRIPT_ACCEPTANCE_RESTORED_STATE_UNAVAILABLE");
}

async function waitForCheckpoint(page) {
  const button = page.getByRole("button", {name: "创建存档", exact: true});
  const surface = await tyranoSurface(page);
  await page.waitForTimeout(10_000);
  await page.mouse.move(720, 1);
  await page.waitForTimeout(250);
  if (await button.isVisible().catch(() => false) && await button.isEnabled().catch(() => false)) {return;}
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    await page.mouse.move(720, 1);
    await page.waitForTimeout(250);
    if (await button.isVisible().catch(() => false) && await button.isEnabled().catch(() => false)) {return;}
    await advanceTyranoToStableWait(surface);
    await page.waitForTimeout(125);
  }
  throw new Error("TYRANOSCRIPT_ACCEPTANCE_SAVE_UNAVAILABLE");
}

async function advanceTyranoToStableWait(surface) {
  await surface.evaluate((base) => {
    const visible = (element) => {
      const style = getComputedStyle(element);
      const rectangle = element.getBoundingClientRect();
      return style.display !== "none" && style.visibility !== "hidden" && rectangle.width > 0 && rectangle.height > 0;
    };
    const click = (element) => element.dispatchEvent(new MouseEvent("click", {
      bubbles: true, cancelable: true, view: window,
    }));
    const choice = [...document.querySelectorAll(".glink_button")].find(visible);
    if (choice) {click(choice); return;}
    const start = [...document.querySelectorAll("img")].find((image) =>
      visible(image) && /(?:button.*start|start.*button)/iu.test(image.getAttribute("src") ?? ""));
    if (start) {click(start); return;}
    for (const video of document.querySelectorAll("video")) {if (visible(video)) {click(video);}}
    const eventLayer = [...document.querySelectorAll(".layer_event_click")].find(visible);
    click(eventLayer ?? base);
  });
}

async function createCheckpoint(page, launchId) {
  await revealPreviewToolbar(page);
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && response.url().includes(`/runtime/launches/${launchId}/save-states`),
  {timeout: 120_000});
  await page.getByRole("button", {name: "创建存档", exact: true}).click();
  const response = await responsePromise;
  requireStatus(response.status(), 201, "TYRANOSCRIPT_ACCEPTANCE_SAVE_FAILED");
  return response.json();
}

async function resumeAfterCheckpoint(page) {
  const resumeButton = page.getByRole("button", {name: "继续游戏", exact: true});
  await resumeButton.waitFor({state: "visible"});
  await resumeButton.click();
  await resumeButton.waitFor({state: "hidden"});
}

async function waitForConfig(page, launchId) {
  const response = await page.waitForResponse((candidate) =>
    candidate.request().method() === "GET" && candidate.url().endsWith(`/runtime/launches/${launchId}/config`),
  {timeout: 120_000});
  requireStatus(response.status(), 200, "TYRANOSCRIPT_ACCEPTANCE_CONFIG_FAILED");
  return response.json();
}

function requireTyranoScriptRuntimeSite(config) {
  try {
    const games = config.resources?.filter((resource) => resource.role === "game");
    const game = games?.[0];
    if (games?.length !== 1 || game.kind !== "ISOLATED_WEB" ||
      typeof game.origin !== "string" || !game.origin ||
      typeof game.contentDigest !== "string" || !/^[a-f0-9]{64}$/u.test(game.contentDigest)) {
      throw new Error("invalid isolated game resource");
    }
    requireLocalRuntimeSite(baseUrl, game.origin);
    return game;
  } catch {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_RUNTIME_ORIGIN_INVALID");
  }
}

async function sendGamepadInput(surface) {
  return surface.evaluate(async () => {
    window.__retromObservedGamepad = null;
    const suppressEscape = (event) => {
      if (event.key !== "Escape" && event.keyCode !== 27) {return;}
      event.preventDefault();
      event.stopImmediatePropagation();
    };
    document.addEventListener("keydown", suppressEscape, true);
    document.addEventListener("keyup", suppressEscape, true);
    window.TYRANO.kag.once("gamepad-pressdown.retrom-acceptance", (event) => {
      window.__retromObservedGamepad = event.detail.button_name;
    });
    try {
      window.__retromTestGamepad.button(1, true);
      window.dispatchEvent(new Event("gamepadconnected"));
      const deadline = Date.now() + 10_000;
      while (window.__retromObservedGamepad === null && Date.now() < deadline) {
        await new Promise((resolvePromise) => setTimeout(resolvePromise, 20));
      }
      window.__retromTestGamepad.button(1, false);
      await new Promise((resolvePromise) => setTimeout(resolvePromise, 150));
      return window.__retromObservedGamepad;
    } finally {
      document.removeEventListener("keydown", suppressEscape, true);
      document.removeEventListener("keyup", suppressEscape, true);
    }
  });
}

async function engineState(surface) {
  return surface.evaluate(() => ({
    marker: window.TYRANO.kag.stat.f.__retrom_checkpoint_marker ?? null,
    scenario: window.TYRANO.kag.stat.current_scenario,
    order: window.TYRANO.kag.ftag.current_order_index,
  }));
}

async function screenshotEvidence(surface, stem) {
  const pngBytes = await surface.screenshot({animations: "disabled", timeout: 120_000, type: "png"});
  const decoded = await sharp(pngBytes).removeAlpha().raw().toBuffer({resolveWithObject: true});
  let nonBlackPixels = 0;
  for (let offset = 0; offset < decoded.data.length; offset += decoded.info.channels) {
    if (decoded.data[offset] || decoded.data[offset + 1] || decoded.data[offset + 2]) {nonBlackPixels += 1;}
  }
  writeFileSync(join(screenshotsDirectory, `${stem}.png`), pngBytes, {mode: 0o600});
  return {
    height: decoded.info.height, nonBlackPixels,
    pngSha256: createHash("sha256").update(pngBytes).digest("hex"), width: decoded.info.width,
  };
}

function requireGamepadB(value) {
  if (value !== "B") {throw new Error("TYRANOSCRIPT_ACCEPTANCE_GAMEPAD_INPUT_UNOBSERVED");}
}
function requireStatus(actual, expected, code) {if (actual !== expected) {throw new Error(code);}}
function capabilities() {return {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true};}

function normalizedBaseUrl(value) {
  try {
    const url = new URL(value);
    if ((url.protocol !== "https:" && (url.protocol !== "http:" || !isLocalAcceptanceHostname(url.hostname))) ||
      url.username || url.password || url.pathname !== "/" || url.search || url.hash) {throw new Error("invalid");}
    return url.origin;
  } catch {throw new Error("TYRANOSCRIPT_ACCEPTANCE_BASE_URL_INVALID");}
}

function stableErrorCode(error) {
  if (error instanceof Error) {
    const stable = /\bTYRANOSCRIPT_[A-Z0-9_]+\b/u.exec(error.message)?.[0];
    if (stable) {return stable;}
  }
  return "TYRANOSCRIPT_ACCEPTANCE_FAILED";
}

function debugAcceptance(value) {
  if (process.env.RETROM_ACCEPTANCE_DEBUG !== "1") {return;}
  process.stderr.write(`[tyranoscript-debug] ${String(value).slice(0, 4_000)}\n`);
}

function writeEvidence(value) {
  writeFileSync(evidencePath, `${JSON.stringify(value, null, 2)}\n`, {encoding: "utf8", mode: 0o600});
}
