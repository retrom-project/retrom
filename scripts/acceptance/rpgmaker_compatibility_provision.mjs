#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import {
  createProductClient, directoryFiles, reviewForImport,
} from "./rpgmaker_security_upload.mjs";
import { gitProvenance } from "./rpgmaker_evidence_provenance.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";

const phase = process.argv[2];
if (!["old", "new"].includes(phase)) {
  throw new Error("usage: rpgmaker_compatibility_provision.mjs old|new");
}
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const chromeExecutablePath = required("RETROM_CHROME_EXECUTABLE");
const fixtureRoot = resolve("testdata/public-roms/rpgmaker-smoke");
const expectedRoute = phase === "old" ? "RPG2000_PREVIOUS_RELEASE" : "RPG2000_NEXT_RELEASE";
const fixture = phase === "old" ? "rpg2000" : "rpg2000-compat";
const browser = await chromium.launch({ executablePath: chromeExecutablePath, headless: true });

try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: { Origin: baseUrl },
    data: {
      username: required("RETROM_ACCEPTANCE_USERNAME"),
      password: required("RETROM_ACCEPTANCE_PASSWORD"),
    },
    failOnStatusCode: false,
  });
  if (loginResponse.status() !== 200) { throw new Error(`RPG_012_PROVISION_LOGIN_${loginResponse.status()}`); }
  const login = await loginResponse.json();
  if (!login.csrfToken) { throw new Error("RPG_012_PROVISION_CSRF_MISSING"); }
  const client = createProductClient(context, baseUrl, login.csrfToken);
  const platformInstanceId = await rpg2000Instance(client);
  await assertSelectedRoute(client);
  const imported = await client.importProject(
    directoryFiles(`${fixtureRoot}/${fixture}`, `${fixture}/`), "DIRECTORY", platformInstanceId,
  );
  if (imported.status !== 202) {
    throw new Error(`RPG_012_PROVISION_IMPORT_${imported.status}_${imported.body.error?.code ?? "UNKNOWN"}`);
  }
  const review = await reviewForImport(client, imported.body.importJobId);
  exact(review.rpgMaker?.selectedCoreId, "rpgmaker_2000", "RPG_012_PROVISION_CORE_MISMATCH");
  exact(review.rpgMaker?.generation, "RPG2000", "RPG_012_PROVISION_GENERATION_MISMATCH");
  const published = await validateAndPublish(context, client, review);
  const result = phase === "old"
    ? await createOldProductSave(context, client, published.gameId)
    : { gameId: published.gameId };
  process.stdout.write(`${JSON.stringify({
    schemaVersion: 1, caseId: "ACC-RPG-012", phase: phase.toUpperCase(),
    importItemId: review.itemId, validationId: published.validationId,
    routeKey: expectedRoute, ...result, repository: gitProvenance(),
  }, null, 2)}\n`);
} finally {
  await browser.close();
}

async function rpg2000Instance(client) {
  let instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  let found = (instances.items ?? []).find((item) => item.enabled && item.defaultCoreId === "rpgmaker_2000");
  if (found) { return found.id; }
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  found = (instances.items ?? []).find((item) => item.enabled && item.defaultCoreId === "rpgmaker_2000");
  if (!found) { throw new Error("RPG_012_PROVISION_PLATFORM_INSTANCE_MISSING"); }
  return found.id;
}

async function assertSelectedRoute(client) {
  const response = await client.json("GET", "/api/v1/admin/core-artifacts");
  const selected = (response.items ?? []).filter((item) =>
    item.coreId === "rpgmaker_2000" && item.selectedForNewBindings && item.availableForLaunch,
  );
  if (selected.length !== 1 || selected[0].routeKey !== expectedRoute) {
    throw new Error("RPG_012_PROVISION_SELECTED_ROUTE_MISMATCH");
  }
}

async function validateAndPublish(context, client, review) {
  const createdResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations`,
    { headers: validationHeaders(client, review.version), data: { clientCapabilities: capabilities() } },
  );
  exact(createdResponse.status(), 201, "RPG_012_PROVISION_VALIDATION_CREATE_FAILED");
  const created = await createdResponse.json();
  const original = await openPlayer(context, created.playerUrl);
  await runtimeAction(original, "输入已经生效", ["ArrowLeft"]);
  await runtimeAction(original, "已听到游戏音频", []);
  await runtimeAction(original, "记录 B 并创建检查点", ["ArrowRight", "ArrowRight"]);
  await runtimeAction(original, "记录 C 并结束原运行", ["ArrowRight", "ArrowRight"]);
  await waitForValidation(client, review.itemId, created.validationId, "CHECKPOINTED");
  await closeCleanPlayer(original, "RPG_012_PROVISION_ORIGINAL_PLAYER_ERROR");
  const restoreResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/restore-launch`,
    { headers: validationHeaders(client, review.version), data: { clientCapabilities: capabilities() } },
  );
  exact(restoreResponse.status(), 201, "RPG_012_PROVISION_RESTORE_CREATE_FAILED");
  const restored = await restoreResponse.json();
  if (restored.launchId === created.launchId) { throw new Error("RPG_012_PROVISION_RESTORE_LAUNCH_REUSED"); }
  const restorePage = await openPlayer(context, restored.playerUrl);
  await runtimeAction(
    restorePage, "恢复后输入已经生效",
    ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"],
  );
  await waitForValidation(client, review.itemId, created.validationId, "AWAITING_DECISION");
  await closeCleanPlayer(restorePage, "RPG_012_PROVISION_RESTORE_PLAYER_ERROR");
  const decision = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/decision`,
    { headers: validationHeaders(client, review.version), data: {
      decision: "PASS", note: "ACC-RPG-012 deterministic product prerequisite",
    } },
  );
  exact(decision.status(), 200, "RPG_012_PROVISION_DECISION_FAILED");
  const decided = await decision.json();
  exact(decided.state, "PASSED", "RPG_012_PROVISION_VALIDATION_NOT_PASSED");
  exact(decided.routeEvidence?.routeKey, expectedRoute, "RPG_012_PROVISION_VALIDATION_ROUTE_MISMATCH");
  const currentReview = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const approvalResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/approve`,
    { headers: validationHeaders(client, currentReview.version), data: {} },
  );
  const approval = await approvalResponse.json();
  if (approvalResponse.status() !== 201) {
    throw new Error(`RPG_012_PROVISION_APPROVAL_${approvalResponse.status()}_${approval.error?.code ?? "UNKNOWN"}`);
  }
  if (!approval.gameId) { throw new Error("RPG_012_PROVISION_GAME_ID_MISSING"); }
  return { gameId: approval.gameId, validationId: created.validationId };
}

async function createOldProductSave(context, client, gameId) {
  const launch = await client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: {
      gameId, coreId: "rpgmaker_2000", saveStateId: null, dosEntry: null,
      returnTo: `/games/${gameId}`, clientCapabilities: capabilities(),
    },
  });
  const page = await openPlayer(context, launch.playUrl);
  await page.getByRole("status").filter({ hasText: "可创建存档" }).waitFor({ state: "attached", timeout: 120_000 });
  const canvas = await focusRuntimeCanvas(page);
  await canvas.press("ArrowRight", { delay: 250 });
  await page.waitForTimeout(800);
  await revealProductToolbar(page);
  const saveResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    response.url().includes(`/runtime/launches/${launch.launchId}/save-states`),
  { timeout: 120_000 });
  await page.getByRole("button", { name: "创建存档", exact: true }).click();
  const response = await saveResponse;
  exact(response.status(), 201, "RPG_012_PROVISION_PRODUCT_SAVE_FAILED");
  const receipt = await response.json();
  if (!receipt.saveStateId || !receipt.screenshotUrl) {
    throw new Error("RPG_012_PROVISION_PRODUCT_SAVE_RECEIPT_INVALID");
  }
  const saves = await client.json("GET", `/api/v1/saves?gameId=${encodeURIComponent(gameId)}&limit=100`);
  if (!(saves.items ?? []).some((item) =>
    item.saveStateId === receipt.saveStateId && item.screenshotUrl === receipt.screenshotUrl,
  )) { throw new Error("RPG_012_PROVISION_PRODUCT_SAVE_NOT_LISTED"); }
  await closeCleanPlayer(page, "RPG_012_PROVISION_PRODUCT_PLAYER_ERROR");
  return { gameId, saveStateId: receipt.saveStateId };
}

async function openPlayer(context, playerUrl) {
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.stack || error.message));
  page.__retromPageErrors = errors;
  await page.goto(`${baseUrl}${playerUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
  return page;
}

async function runtimeAction(page, label, keys) {
  const button = page.getByRole("button", { name: label, exact: true });
  await button.waitFor({ state: "visible", timeout: 120_000 });
  const canvas = await focusRuntimeCanvas(page);
  for (const key of keys) {
    await canvas.press(key, { delay: 250 });
    await page.waitForTimeout(800);
  }
  await button.click();
  await page.waitForTimeout(500);
  const alert = (await page.getByRole("alert").allInnerTexts()).map((value) => value.trim()).find(Boolean);
  if (alert) { throw new Error(`RPG_012_PROVISION_RUNTIME_ACTION_${label}_${alert}`); }
}

async function focusRuntimeCanvas(page) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator("canvas").first();
      if (await canvas.isVisible().catch(() => false)) {
        await canvas.evaluate((element) => { element.tabIndex = 0; element.focus(); });
        return canvas;
      }
    }
    await page.waitForTimeout(100);
  }
  throw new Error("RPG_012_PROVISION_RUNTIME_CANVAS_MISSING");
}

async function closeCleanPlayer(page, code) {
  const errors = page.__retromPageErrors ?? [];
  await page.close();
  if (errors.length) { throw new Error(`${code}:${String(errors[0]).slice(0, 600)}`); }
}

async function waitForValidation(client, itemId, validationId, expectedState) {
  for (let attempt = 0; attempt < 600; attempt += 1) {
    const validation = await client.json("GET", `/api/v1/admin/reviews/${itemId}/runtime-validations/${validationId}`);
    if (validation.state === expectedState) { return validation; }
    if (["FAILED", "EXPIRED", "PASSED"].includes(validation.state)) {
      throw new Error(`RPG_012_PROVISION_VALIDATION_${validation.state}_${validation.failureCode ?? "UNKNOWN"}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error(`RPG_012_PROVISION_VALIDATION_${expectedState}_TIMEOUT`);
}

async function revealProductToolbar(page) {
  const toolbar = page.locator(".player-toolbar");
  if (!await toolbar.evaluate((element) => element.classList.contains("is-visible"))) {
    await page.locator(".player-hud-handle").click();
  }
  await page.locator(".player-toolbar.is-visible").waitFor({ state: "visible" });
}

function validationHeaders(client, version) {
  return { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${version}"` };
}

function capabilities() {
  return { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true };
}

function normalizedBase(value) {
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("RPG_012_PROVISION_BASE_URL_INVALID");
  }
  if (parsed.protocol !== "https:" && !isLocalAcceptanceHostname(parsed.hostname)) {
    throw new Error("RPG_012_PROVISION_BASE_URL_REQUIRES_HTTPS");
  }
  return parsed.origin;
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_012_PROVISION_ENV_MISSING_${name}`); }
  return value;
}

function exact(actual, expected, code) {
  if (actual !== expected) { throw new Error(`${code}:${actual}`); }
}
