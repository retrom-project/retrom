#!/usr/bin/env node
import { createHash, randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import { loadCompatibilityProvisioning } from "./rpgmaker_compatibility_provenance.mjs";

const caseId = required("RETROM_RPG_CASE_ID");
if (caseId !== "ACC-RPG-012") { throw new Error("RPG_ACCEPTANCE_COMPATIBILITY_CASE_INVALID"); }
const caseDir = required("RETROM_RPG_CASE_DIR");
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const username = required("RETROM_ACCEPTANCE_USERNAME");
const password = required("RETROM_ACCEPTANCE_PASSWORD");
const state = JSON.parse(readFileSync(required("RETROM_ACC_RPG_012_STATE"), "utf8"));
const provisioningEvidence = loadCompatibilityProvisioning(state);
const chromeExecutablePath = required("RETROM_CHROME_EXECUTABLE");
const screenshotDir = join(caseDir, "screenshots");
mkdirSync(screenshotDir, { recursive: true });

const browser = await chromium.launch({ executablePath: chromeExecutablePath, headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const login = await jsonRequest(context.request, "POST", "/api/v1/auth/login", {
    headers: { Origin: baseUrl }, data: { username, password }, expected: 200,
  });
  if (!login.csrfToken) { throw new Error("RPG_ACCEPTANCE_LOGIN_CSRF_MISSING"); }
  const writeHeaders = () => ({
    Origin: baseUrl, "X-Retrom-Csrf": login.csrfToken, "Idempotency-Key": randomUUID(),
  });
  const artifacts = await allArtifacts(context.request);
  const oldArtifact = requireArtifact(artifacts, state.oldArtifact);
  const newArtifact = requireArtifact(artifacts, state.newArtifact);
  const oldSave = await requireOldSave(context.request, state.oldCheckpoint);
  const restored = await restoreOldCheckpoint(context, writeHeaders, oldSave);
  const current = await launchCurrentVariant(context, writeHeaders);
  const driftRejections = await rejectDriftedCheckpoints(context.request, writeHeaders);
  const payload = {
    schemaVersion: 1, caseId, status: "PASS",
    artifacts: { old: safeArtifact(oldArtifact), new: safeArtifact(newArtifact) },
    bindings: { oldCheckpoint: state.oldCheckpoint, newVariant: state.newVariant, provisioningEvidence },
    oldRestore: restored, newLaunch: current, driftRejections,
    screenshots: ["screenshots/acc-rpg-012-old-save.png", "screenshots/acc-rpg-012-restored-save.png",
      "screenshots/acc-rpg-012-old-player.png", "screenshots/acc-rpg-012-new-player.png"],
  };
  writeFileSync(join(caseDir, "rpgmaker-product.json"), `${JSON.stringify(payload, null, 2)}\n`);
} finally {
  await browser.close();
}

async function requireOldSave(request, checkpoint) {
  const response = await jsonRequest(
    request, "GET", `/api/v1/saves?gameId=${encodeURIComponent(checkpoint.gameId)}&limit=100`,
  );
  const save = array(response.items).find((item) => item.saveStateId === checkpoint.saveStateId);
  if (!save || !save.screenshotUrl) { throw new Error("RPG_ACCEPTANCE_OLD_SAVE_SCREENSHOT_MISSING"); }
  return save;
}

async function restoreOldCheckpoint(context, writeHeaders, oldSave) {
  const checkpoint = state.oldCheckpoint;
  const launch = await createLaunch(
    context.request, writeHeaders, checkpoint.gameId, state.oldArtifact.coreId, checkpoint.saveStateId, 201,
  );
  const config = await jsonRequest(context.request, "GET", `/runtime/launches/${launch.launchId}/config`);
  exact(config.artifactId, state.oldArtifact.id, "RPG_ACCEPTANCE_OLD_RESTORE_ARTIFACT_MISMATCH");
  exact(config.routeKey, state.oldArtifact.routeKey, "RPG_ACCEPTANCE_OLD_RESTORE_ROUTE_MISMATCH");
  const oldScreenshot = await binaryRequest(context.request, oldSave.screenshotUrl);
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.goto(`${baseUrl}${launch.playUrl}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("status").filter({ hasText: "可创建存档" }).waitFor({ state: "attached", timeout: 120_000 });
  await assertPlayerBinding(page, config);
  const saveResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    response.url().includes(`/runtime/launches/${launch.launchId}/save-states`), { timeout: 120_000 });
  await page.getByRole("button", { name: "创建存档", exact: true }).click();
  const replayResponse = await saveResponse;
  if (replayResponse.status() !== 201) { throw new Error("RPG_ACCEPTANCE_RESTORE_REPLAY_SAVE_FAILED"); }
  const replay = await replayResponse.json();
  if (!replay.saveStateId || !replay.screenshotUrl) { throw new Error("RPG_ACCEPTANCE_RESTORE_REPLAY_RECEIPT_INVALID"); }
  const restoredScreenshot = await binaryRequest(context.request, replay.screenshotUrl);
  writeFileSync(join(screenshotDir, "acc-rpg-012-old-save.png"), oldScreenshot);
  writeFileSync(join(screenshotDir, "acc-rpg-012-restored-save.png"), restoredScreenshot);
  if (!oldScreenshot.equals(restoredScreenshot)) {
    throw new Error("RPG_ACCEPTANCE_RESTORED_POSITION_SCREENSHOT_MISMATCH");
  }
  await page.screenshot({ path: join(screenshotDir, "acc-rpg-012-old-player.png"), fullPage: true });
  if (pageErrors.length) { throw new Error("RPG_ACCEPTANCE_OLD_PLAYER_PAGE_ERROR"); }
  await page.close();
  return {
    launchId: launch.launchId, replaySaveStateId: replay.saveStateId, playerRunning: true,
    artifactId: config.artifactId, routeKey: config.routeKey,
    adapterAbi: checkpoint.adapterAbi,
    dependencySnapshotSha256: checkpoint.dependencySnapshotSha256,
    originalScreenshotSha256: sha256(oldScreenshot), replayScreenshotSha256: sha256(restoredScreenshot),
    screenshotRoundTripExact: true,
  };
}

async function launchCurrentVariant(context, writeHeaders) {
  const variant = state.newVariant;
  const launch = await createLaunch(
    context.request, writeHeaders, variant.gameId, state.newArtifact.coreId, null, 201,
  );
  const config = await jsonRequest(context.request, "GET", `/runtime/launches/${launch.launchId}/config`);
  exact(config.artifactId, state.newArtifact.id, "RPG_ACCEPTANCE_NEW_LAUNCH_ARTIFACT_MISMATCH");
  exact(config.routeKey, state.newArtifact.routeKey, "RPG_ACCEPTANCE_NEW_LAUNCH_ROUTE_MISMATCH");
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.goto(`${baseUrl}${launch.playUrl}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("status").filter({ hasText: "可创建存档" }).waitFor({ state: "attached", timeout: 120_000 });
  await assertPlayerBinding(page, config);
  await page.screenshot({ path: join(screenshotDir, "acc-rpg-012-new-player.png"), fullPage: true });
  if (pageErrors.length) { throw new Error("RPG_ACCEPTANCE_NEW_PLAYER_PAGE_ERROR"); }
  await page.close();
  return { launchId: launch.launchId, playerRunning: true, artifactId: config.artifactId, routeKey: config.routeKey };
}

async function rejectDriftedCheckpoints(request, writeHeaders) {
  const results = [];
  for (const kind of ["content", "artifact", "pack", "adapterAbi"]) {
    const saveStateId = state.driftSaveStateIds[kind];
    const response = await createLaunch(
      request, writeHeaders, state.oldCheckpoint.gameId, state.oldArtifact.coreId, saveStateId, 422,
    );
    const code = response.error?.code;
    if (code !== "LAUNCH_BLOCKED" || response.launchId) {
      throw new Error(`RPG_ACCEPTANCE_${kind.toUpperCase()}_DRIFT_NOT_REJECTED`);
    }
    results.push({ kind, saveStateId, status: 422, code, launchCreated: false });
  }
  return results;
}

async function assertPlayerBinding(page, config) {
  await revealProductToolbar(page);
  const moreActions = page.getByRole("button", { name: "更多操作" });
  await moreActions.waitFor({ state: "visible" });
  await moreActions.click();
  const debugControl = page.locator(".player-debug-control");
  await debugControl.waitFor({ state: "visible" });
  await debugControl.click();
  const diagnostics = page.getByRole("complementary", { name: "运行调试信息" });
  await diagnostics.waitFor({ state: "visible" });
  const text = await diagnostics.innerText();
  if (!text.includes(config.coreName) || !text.includes("RPG Maker")) {
    throw new Error("RPG_ACCEPTANCE_PLAYER_DIAGNOSTIC_BINDING_MISMATCH");
  }
  const internalValues = [config.routeKey, config.artifactId, config.adapter?.adapterId].filter(Boolean);
  if (internalValues.some((value) => text.includes(value))) {
    throw new Error("RPG_ACCEPTANCE_PLAYER_DIAGNOSTIC_IMPLEMENTATION_LEAK");
  }
  await page.getByRole("button", { name: "关闭调试信息面板" }).click();
  await diagnostics.waitFor({ state: "hidden" });
}

async function revealProductToolbar(page) {
  const toolbar = page.locator(".player-toolbar");
  const visible = await toolbar.evaluate((element) => element.classList.contains("is-visible"));
  if (!visible) {
    await page.locator(".player-hud-handle").click();
  }
  await page.locator(".player-toolbar.is-visible").waitFor({ state: "visible" });
}

async function createLaunch(request, writeHeaders, gameId, coreId, saveStateId, expected) {
  return jsonRequest(request, "POST", "/api/v1/launches", {
    headers: writeHeaders(), expected,
    data: {
      gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
}

async function allArtifacts(request) {
  const response = await jsonRequest(request, "GET", "/api/v1/admin/core-artifacts");
  return array(response.items);
}

function requireArtifact(artifacts, expected) {
  const artifact = artifacts.find((item) => item.id === expected.id);
  if (!artifact || artifact.routeKey !== expected.routeKey ||
      artifact.selectedForNewBindings !== expected.selectedForNewBindings || !artifact.availableForLaunch) {
    throw new Error("RPG_ACCEPTANCE_ARTIFACT_HISTORY_MISMATCH");
  }
  return artifact;
}

async function jsonRequest(request, method, path, options = {}) {
  const response = await request.fetch(`${baseUrl}${path}`, {
    method, headers: options.headers, data: options.data, failOnStatusCode: false,
  });
  const expected = options.expected ?? 200;
  if (response.status() !== expected) { throw new Error(`RPG_ACCEPTANCE_HTTP_${method}_${response.status()}`); }
  return response.json();
}

async function binaryRequest(request, path) {
  const response = await request.get(`${baseUrl}${path}`, { failOnStatusCode: false });
  if (response.status() !== 200 || !response.headers()["content-type"]?.startsWith("image/")) {
    throw new Error("RPG_ACCEPTANCE_SCREENSHOT_UNAVAILABLE");
  }
  return response.body();
}

function safeArtifact(item) {
  return {
    id: item.id, coreId: item.coreId, routeKey: item.routeKey,
    selectedForNewBindings: item.selectedForNewBindings, availableForLaunch: item.availableForLaunch,
  };
}

function sha256(contents) { return createHash("sha256").update(contents).digest("hex"); }
function array(value) { return Array.isArray(value) ? value : []; }
function exact(actual, expected, code) { if (actual !== expected) { throw new Error(code); } }
function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_ACCEPTANCE_ENV_MISSING_${name}`); }
  return value;
}
function normalizedBase(value) {
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("RPG_ACCEPTANCE_BASE_URL_INVALID");
  }
  if (parsed.protocol !== "https:" && !["127.0.0.1", "localhost"].includes(parsed.hostname)) {
    throw new Error("RPG_ACCEPTANCE_BASE_URL_REQUIRES_HTTPS");
  }
  return parsed.origin;
}
