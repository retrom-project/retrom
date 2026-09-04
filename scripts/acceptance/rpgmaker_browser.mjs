#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";
import { normalizedBase } from "./rpgmaker_url.mjs";
import { trackRuntimeLoading } from "./runtime_loading_evidence.mjs";

const caseId = required("RETROM_RPG_CASE_ID");
const caseDir = required("RETROM_RPG_CASE_DIR");
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const username = required("RETROM_ACCEPTANCE_USERNAME");
const password = required("RETROM_ACCEPTANCE_PASSWORD");
const chromeExecutablePath = required("RETROM_CHROME_EXECUTABLE");
const screenshotDir = join(caseDir, "screenshots");
const productReadyTimeoutMs = 180_000;
mkdirSync(screenshotDir, { recursive: true });

const localProxy = await localRpgAcceptanceProxy(baseUrl);
const browser = await chromium.launch({ executablePath: chromeExecutablePath, headless: true });
const chromeVersion = browser.version();
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 }, ...localProxy.contextOptions,
  });
  const login = await jsonRequest(context.request, "POST", "/api/v1/auth/login", {
    headers: { Origin: baseUrl }, data: { username, password }, expected: 200,
  });
  if (!login.csrfToken) { throw new Error("RPG_ACCEPTANCE_LOGIN_CSRF_MISSING"); }
  const writeHeaders = () => ({
    Origin: baseUrl, "X-Retrom-Csrf": login.csrfToken, "Idempotency-Key": randomUUID(),
  });
  const payload = caseId === "ACC-RPG-001"
    ? await catalogCase(context, writeHeaders)
    : await generationCase(context, writeHeaders);
  writeFileSync(join(caseDir, "rpgmaker-product.json"), `${JSON.stringify(payload, null, 2)}\n`);
  if (payload.status === "BLOCKED") { process.exitCode = 3; }
} finally {
  await browser.close();
  await localProxy.close();
}

async function catalogCase(context, writeHeaders) {
  const coreIds = ["rpgmaker"];
  const targetIds = [
    "rpgmaker-2000", "rpgmaker-2003", "rpgmaker-xp", "rpgmaker-vx",
    "rpgmaker-vx-ace", "rpgmaker-mv", "rpgmaker-mz",
  ];
  const templateKeys = ["rpgmaker/rpgmaker"];
  const platforms = await jsonRequest(context.request, "GET", "/api/v1/admin/platforms");
  const rpgPlatforms = array(platforms.items).filter((platform) => platform.id === "rpgmaker");
  exact(rpgPlatforms.length, 1, "RPG_ACCEPTANCE_PLATFORM_COUNT");
  const cores = array(rpgPlatforms[0].cores).filter((core) => core.enabled);
  exact(cores.map((core) => core.id).sort(), [...coreIds].sort(), "RPG_ACCEPTANCE_CORE_CATALOG");
  const forbidden = /easyrpg|mkxp|native[ _-]?web|adapter|bridge/i;
  if (cores.some((core) => forbidden.test(String(core.name)))) {
    throw new Error("RPG_ACCEPTANCE_USER_CORE_LEAKS_IMPLEMENTATION");
  }
  const before = await jsonRequest(context.request, "GET", "/api/v1/admin/platform-instances/recommendations");
  const recommendations = array(before.items).filter((item) => item.platform?.id === "rpgmaker");
  exact(recommendations.map((item) => item.templateKey).sort(), [...templateKeys].sort(), "RPG_ACCEPTANCE_RECOMMENDATIONS");
  const apply = process.env.RETROM_ACC_RPG_001_MODE === "APPLY";
  const applied = apply
    ? await jsonRequest(context.request, "POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: writeHeaders(), data: {}, expected: 200,
    })
    : before;
  const covered = array(applied.items).filter((item) => item.platform?.id === "rpgmaker");
  exact(covered.map((item) => item.templateKey).sort(), [...templateKeys].sort(), "RPG_ACCEPTANCE_APPLIED_RECOMMENDATIONS");
  if (apply && covered.some((item) => !["ACTIVE", "CUSTOMIZED", "COVERED_BY_EQUIVALENT"].includes(item.state))) {
    throw new Error("RPG_ACCEPTANCE_RECOMMENDATION_NOT_COVERED");
  }
  const instances = await jsonRequest(context.request, "GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  const enabledInstances = array(instances.items).filter((item) => item.enabled);
  if (apply) {
    exact(enabledInstances.map((item) => item.defaultCoreId).sort(), [...coreIds], "RPG_ACCEPTANCE_DIRECTORY_CORE_BINDINGS");
  }
  const targets = await allRuntimeTargets(context.request);
  const selected = targets.filter((item) => item.providerId === "retrom-runtime" && targetIds.includes(item.targetId));
  exact(selected.map((item) => item.targetId).sort(), [...targetIds].sort(), "RPG_ACCEPTANCE_SELECTED_TARGETS");
  if (selected.some((item) => !/^[0-9a-f]{64}$/.test(item.bundleSha256 ?? ""))) {
    throw new Error("RPG_ACCEPTANCE_TARGET_DIAGNOSTIC_INCOMPLETE");
  }
  const page = await context.newPage();
  await page.goto(`${baseUrl}/admin/platform-instances`, { waitUntil: "domcontentloaded", timeout: 120_000 });
  await page.getByRole("table", { name: "游戏目录" }).waitFor({ state: "visible", timeout: 120_000 });
  await page.screenshot({ path: join(screenshotDir, "rpgmaker-directories.png"), fullPage: true });
  if (forbidden.test(await page.locator("body").innerText())) {
    throw new Error("RPG_ACCEPTANCE_DIRECTORY_UI_LEAKS_IMPLEMENTATION");
  }
  await page.goto(`${baseUrl}/admin/bios?tab=rpgmaker`, { waitUntil: "domcontentloaded", timeout: 120_000 });
  await page.waitForFunction(
    (identifiers) => identifiers.every((identifier) => document.body.innerText.includes(identifier)),
    selected.map((item) => item.targetId),
    { timeout: 120_000 },
  );
  await page.screenshot({ path: join(screenshotDir, "rpgmaker-runtime-diagnostics.png"), fullPage: true });
  const diagnosticText = await page.locator("body").innerText();
  for (const item of selected) {
    if (!diagnosticText.includes(item.targetId) || !diagnosticText.includes(item.providerVersion)) {
      throw new Error("RPG_ACCEPTANCE_TARGET_DIAGNOSTIC_NOT_VISIBLE");
    }
  }
  await page.close();
  return {
    schemaVersion: 1, caseId, status: apply ? "PASS" : "BLOCKED",
    reason: apply ? null : "只读 preflight 完成；fresh DB 必须显式设置 RETROM_ACC_RPG_001_MODE=APPLY 才执行推荐目录事务",
    catalog: { platformId: "rpgmaker", coreIds, targetIds, templateKeys },
    recommendationStates: covered.map((item) => ({ templateKey: item.templateKey, state: item.state })),
    targets: selected.map(safeTarget),
    screenshots: ["screenshots/rpgmaker-directories.png", "screenshots/rpgmaker-runtime-diagnostics.png"],
  };
}

async function generationCase(context, writeHeaders) {
  const prefix = `RETROM_${caseId.replaceAll("-", "_")}`;
  const itemId = required(`${prefix}_IMPORT_ITEM_ID`);
  const validationId = required(`${prefix}_VALIDATION_ID`);
  const gameId = required(`${prefix}_GAME_ID`);
  const expectedDigest = required("RETROM_RPG_EXPECTED_PROJECT_DIGEST");
  const validation = await jsonRequest(
    context.request, "GET", `/api/v1/admin/reviews/${itemId}/runtime-validations/${validationId}`,
  );
  if (validation.importItemId !== itemId || validation.validationId !== validationId) {
    throw new Error("RPG_ACCEPTANCE_VALIDATION_RELATION_INVALID");
  }
  if (validation.routeEvidence?.projectFingerprint !== expectedDigest) {
    throw new Error("RPG_ACCEPTANCE_FIXTURE_DIGEST_MISMATCH");
  }
  const approved = await approvedReview(context.request, itemId, gameId);
  const review = approved.event;
  const inputTranscript = await readInputTranscript(context.request, approved.importJobId);
  const restoreScreenshotUrl = validation.checkpointRoundTrip?.screenshotUrl;
  if (!restoreScreenshotUrl?.startsWith("/api/v1/admin/review-assets/")) {
    throw new Error("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_MISSING");
  }
  const restoreScreenshot = await context.request.get(`${baseUrl}${restoreScreenshotUrl}`, { failOnStatusCode: false });
  if (restoreScreenshot.status() !== 200 || !restoreScreenshot.headers()["content-type"]?.startsWith("image/")) {
    throw new Error("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_UNAVAILABLE");
  }
  writeFileSync(
    join(screenshotDir, `${caseId.toLowerCase()}-restored-marker.png`),
    await restoreScreenshot.body(),
  );
  const game = await jsonRequest(context.request, "GET", `/api/v1/admin/games/${gameId}`);
  if (game.status !== "PUBLISHED") { throw new Error("RPG_ACCEPTANCE_GAME_NOT_PUBLISHED"); }
  const launch = await jsonRequest(context.request, "POST", "/api/v1/launches", {
    headers: writeHeaders(), expected: 201,
    data: {
      gameId, coreId: "rpgmaker", saveStateId: null, dosEntry: null,
      returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
  const config = await jsonRequest(context.request, "GET", `/runtime/launches/${launch.launchId}/config`);
  if (!/^\/play\/[0-9a-f-]{36}$/.test(launch.playUrl ?? "")) {
    throw new Error("RPG_ACCEPTANCE_PRODUCT_PLAY_URL_INVALID");
  }
  const projectDeclarations = projectLoadingDeclarations(config);
  const loadingProbeOptions = {
    collectRuntimeTimings: !["rpgmaker-mv", "rpgmaker-mz"].includes(config.runtime.targetId),
  };
  const page = await context.newPage();
  const loadingProbe = trackRuntimeLoading(page, projectDeclarations, loadingProbeOptions);
  const pageErrors = [];
  const runtimeExceptions = [];
  const runtimeExceptionTasks = [];
  const dialogs = [];
  const networkResponses = [];
  const consoleDiagnostics = [];
  const failedResponses = [];
  let responseInventoryOverflow = false;
  const cdp = await context.newCDPSession(page);
  await cdp.send("Runtime.enable");
  cdp.on("Runtime.exceptionThrown", ({ exceptionDetails }) => {
    const task = collectRuntimeException(cdp, exceptionDetails)
      .then((diagnostic) => runtimeExceptions.push(diagnostic));
    runtimeExceptionTasks.push(task);
  });
  page.on("pageerror", (error) => pageErrors.push(error.stack || error.message));
  page.on("console", (message) => {
    if (message.text().trim()) {
      consoleDiagnostics.push(`${message.type()}:${message.text()}`.slice(0, 800));
    }
  });
  page.on("dialog", async (dialog) => {
    dialogs.push(dialog.message().slice(0, 400));
    await dialog.dismiss();
  });
  page.on("response", (response) => {
    if (networkResponses.length >= 20_000) {
      responseInventoryOverflow = true;
      return;
    }
    const request = response.request();
    networkResponses.push({
      method: request.method(), url: safeStackUrl(response.url()),
      resourceType: request.resourceType(), status: response.status(),
    });
    if (response.status() >= 400) {
      failedResponses.push(`${response.status()}:${response.url()}`.slice(0, 800));
    }
  });
  progress("first-launch-navigation");
  await page.goto(`${baseUrl}${launch.playUrl}`, { waitUntil: "domcontentloaded" });
  const moreActions = page.getByRole("button", { name: "更多操作" });
  await moreActions.waitFor({ state: "visible", timeout: 120_000 });
  await waitForProductSaveAvailability(page, pageErrors, runtimeExceptions, dialogs, caseId);
  progress("first-launch-ready");
  const firstVisibleLoading = applyEasyProjectDeclaration(
    await loadingProbe.snapshot(), config, inputTranscript.upload,
  );
  progress("first-launch-loading-snapshot");
  loadingProbe.stop();
  await revealProductToolbar(page);
  await moreActions.click();
  const debugControl = page.locator(".player-debug-control");
  await debugControl.waitFor({ state: "visible" });
  await debugControl.click();
  const diagnostics = page.getByRole("complementary", { name: "运行调试信息" });
  await diagnostics.waitFor({ state: "visible" });
  const diagnosticText = await diagnostics.innerText();
  if (!diagnosticText.includes("RPG Maker")) {
    throw new Error("RPG_ACCEPTANCE_PLAYER_DIAGNOSTIC_BINDING_MISMATCH");
  }
  const internalValues = [config.runtime.providerId, config.runtime.targetId];
  if (internalValues.some((value) => diagnosticText.includes(value))) {
    throw new Error("RPG_ACCEPTANCE_PLAYER_DIAGNOSTIC_IMPLEMENTATION_LEAK");
  }
  await page.getByRole("button", { name: "关闭调试信息面板" }).click();
  await diagnostics.waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "继续游戏" }).click();
  await page.waitForTimeout(500);
  await page.screenshot({ path: join(screenshotDir, `${caseId.toLowerCase()}-product-player.png`), fullPage: true });
  progress("first-launch-screenshot");
  if (responseInventoryOverflow) { throw new Error("RPG_ACCEPTANCE_ORIGIN_INVENTORY_OVERFLOW"); }
  const isolatedTarget = ["rpgmaker-mv", "rpgmaker-mz"].includes(config.runtime.targetId);
  const runtimeFrameURL = isolatedTarget ? await page.locator("iframe.player-frame").getAttribute("src") : null;
  const originInventory = runtimeFrameURL
    ? await collectOriginInventory(page, new URL(runtimeFrameURL, baseUrl).origin, networkResponses)
    : null;
  progress("first-launch-origin-inventory");
  await assertNoPlayerErrors(
    pageErrors, runtimeExceptions, consoleDiagnostics, failedResponses,
    networkResponses, runtimeExceptionTasks,
  );
  const firstRuntimeProgress = await assertRuntimeProgress(page);
  await assertNoPlayerErrors(
    pageErrors, runtimeExceptions, consoleDiagnostics, failedResponses,
    networkResponses, runtimeExceptionTasks,
  );
  await page.close();
  pageErrors.length = 0;
  runtimeExceptions.length = 0;
  runtimeExceptionTasks.length = 0;
  networkResponses.length = 0;
  consoleDiagnostics.length = 0;
  failedResponses.length = 0;
  const cacheLaunch = await jsonRequest(context.request, "POST", "/api/v1/launches", {
    headers: writeHeaders(), expected: 201,
    data: {
      gameId, coreId: "rpgmaker", saveStateId: null, dosEntry: null,
      returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
  const cachePage = await context.newPage();
  const cacheCdp = await context.newCDPSession(cachePage);
  await cacheCdp.send("Runtime.enable");
  cacheCdp.on("Runtime.exceptionThrown", ({ exceptionDetails }) => {
    const task = collectRuntimeException(cacheCdp, exceptionDetails)
      .then((diagnostic) => runtimeExceptions.push(diagnostic));
    runtimeExceptionTasks.push(task);
  });
  cachePage.on("pageerror", (error) => pageErrors.push(error.stack || error.message));
  cachePage.on("console", (message) => {
    if (message.text().trim()) {
      consoleDiagnostics.push(`${message.type()}:${message.text()}`.slice(0, 800));
    }
  });
  cachePage.on("response", (response) => {
    if (networkResponses.length < 20_000) {
      const request = response.request();
      networkResponses.push({
        method: request.method(), url: safeStackUrl(response.url()),
        resourceType: request.resourceType(), status: response.status(),
      });
    }
    if (response.status() >= 400) {
      failedResponses.push(`${response.status()}:${response.url()}`.slice(0, 800));
    }
  });
  cachePage.on("dialog", async (dialog) => {
    dialogs.push(dialog.message().slice(0, 400));
    await dialog.dismiss();
  });
  const cacheLoadingProbe = trackRuntimeLoading(cachePage, projectDeclarations, loadingProbeOptions);
  progress("cache-launch-navigation");
  await cachePage.goto(`${baseUrl}${cacheLaunch.playUrl}`, { waitUntil: "domcontentloaded" });
  await waitForProductSaveAvailability(cachePage, pageErrors, runtimeExceptions, dialogs, caseId);
  progress("cache-launch-ready");
  const cacheVisibleLoading = applyEasyProjectDeclaration(
    await cacheLoadingProbe.snapshot(), config, inputTranscript.upload,
  );
  progress("cache-launch-loading-snapshot");
  cacheLoadingProbe.stop();
  await assertNoPlayerErrors(
    pageErrors, runtimeExceptions, consoleDiagnostics, failedResponses,
    networkResponses, runtimeExceptionTasks,
  );
  const cacheRuntimeProgress = await assertRuntimeProgress(cachePage);
  await assertNoPlayerErrors(
    pageErrors, runtimeExceptions, consoleDiagnostics, failedResponses,
    networkResponses, runtimeExceptionTasks,
  );
  await cachePage.close();
  const sameProjectContentIdentity = firstVisibleLoading.projectContentIdentity === null &&
      cacheVisibleLoading.projectContentIdentity === null
    ? null
    : firstVisibleLoading.projectContentIdentity !== null &&
      firstVisibleLoading.projectContentIdentity === cacheVisibleLoading.projectContentIdentity;
  return {
    schemaVersion: 1, caseId, status: "PASS",
    review: safeReview(review, validation), validation: safeValidation(validation),
    inputTranscript,
    runtimeEnvironment: { chromeVersion },
    productLaunch: {
      launchId: launch.launchId, playerRunning: true,
      runtimeProgress: { firstLaunch: firstRuntimeProgress, cacheLaunch: cacheRuntimeProgress },
      config: {
        purpose: config.session.purpose, providerId: config.runtime.providerId,
        providerVersion: config.runtime.providerVersion, targetId: config.runtime.targetId,
        bundleSha256: config.runtime.bundleSha256,
        checkpointFormat: config.runtime.checkpoint?.writeFormat ?? null,
        checkpointMaxBytes: config.runtime.checkpoint?.maxBytes ?? null,
      },
    },
    ...(originInventory ? { originInventory } : {}),
    loading: {
      schemaVersion: 1,
      cacheLaunchId: cacheLaunch.launchId,
      sameProjectContentIdentity,
      firstVisible: firstVisibleLoading.evidence,
      cacheLaunchVisible: cacheVisibleLoading.evidence,
    },
    screenshots: [
      `screenshots/${caseId.toLowerCase()}-restored-marker.png`,
      `screenshots/${caseId.toLowerCase()}-product-player.png`,
    ],
  };
}

async function assertNoPlayerErrors(
  pageErrors, runtimeExceptions, consoleDiagnostics, failedResponses,
  networkResponses, runtimeExceptionTasks,
) {
  await Promise.allSettled(runtimeExceptionTasks);
  if (!pageErrors.length && !runtimeExceptions.length) {return;}
  const details = [
    ...pageErrors.slice(0, 5).map((value) => value.slice(0, 1_200)),
    ...runtimeExceptions.slice(0, 5).map((value) => JSON.stringify(value).slice(0, 6_000)),
    ...consoleDiagnostics.slice(-10).map((value) => value.slice(0, 1_200)),
    ...failedResponses.slice(-10).map((value) => value.slice(0, 1_200)),
    ...networkResponses.slice(-100).map((value) => JSON.stringify(value).slice(0, 1_200)),
  ].join(" | ");
  throw new Error(`RPG_ACCEPTANCE_PLAYER_PAGE_ERROR:${details}`);
}

async function assertRuntimeProgress(page) {
  const frameCount = () => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount() ?? null;
  const beforeFrame = await page.evaluate(frameCount);
  if (!Number.isSafeInteger(beforeFrame) || beforeFrame < 0) {
    throw new Error("RPG_ACCEPTANCE_PRODUCT_RUNTIME_PROGRESS_UNAVAILABLE");
  }
  try {
    await page.waitForFunction(
      (before) => {
        const after = window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount();
        return Number.isSafeInteger(after) && after > before;
      },
      beforeFrame,
      { timeout: 10_000 },
    );
  } catch {
    const afterFrame = await page.evaluate(frameCount);
    throw new Error(`RPG_ACCEPTANCE_PRODUCT_RUNTIME_STALLED:${beforeFrame}:${afterFrame}`);
  }
  const afterFrame = await page.evaluate(frameCount);
  return { beforeFrame, afterFrame };
}

function projectLoadingDeclarations(config) {
  if (!["rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace"].includes(config?.runtime?.targetId)) {return [];}
  const sources = array(config.resources).filter((source) => source.kind === "SEEKABLE_BLOB");
  return sources.map((source) => {
    if (source?.kind !== "SEEKABLE_BLOB" || source.rangeRequired !== true ||
      !Number.isSafeInteger(source.sizeBytes) || source.sizeBytes < 1 || typeof source.url !== "string") {
      throw new Error("RPG_ACCEPTANCE_RUNTIME_LOADING_CONFIG_INVALID");
    }
    return { sizeBytes: source.sizeBytes, url: new URL(source.url, baseUrl).href };
  });
}

function applyEasyProjectDeclaration(snapshot, config, upload) {
  if (!["rpgmaker-2000", "rpgmaker-2003"].includes(config?.runtime?.targetId)) {return snapshot;}
  const fileCount = Number(upload?.fileCount);
  const totalBytes = Number(upload?.totalBytes);
  const evidence = snapshot?.evidence;
  if (!evidence || !Number.isSafeInteger(fileCount) || fileCount < 1 ||
    !Number.isSafeInteger(totalBytes) || totalBytes < 1 ||
    (evidence.declaredProjectFileCount > 0 && evidence.declaredProjectFileCount !== fileCount) ||
    (evidence.declaredProjectBytes > 0 && evidence.declaredProjectBytes !== totalBytes)) {
    throw new Error("RPG_ACCEPTANCE_RUNTIME_LOADING_CONFIG_INVALID");
  }
  return {
    ...snapshot,
    evidence: {
      ...evidence,
      declaredLargeFileCount: 0,
      declaredProjectBytes: totalBytes,
      declaredProjectFileCount: fileCount,
    },
  };
}

async function waitForProductSaveAvailability(page, pageErrors, runtimeExceptions, dialogs, caseId) {
  try {
    await page.getByRole("status").filter({ hasText: "可创建存档" })
      .waitFor({ state: "attached", timeout: productReadyTimeoutMs });
  } catch {
    await page.screenshot({
      path: join(screenshotDir, `${caseId.toLowerCase()}-product-unavailable.png`),
      fullPage: true,
    });
    const statuses = (await page.getByRole("status").allTextContents())
      .map((value) => value.trim().slice(0, 400)).filter(Boolean).slice(0, 10);
    const loading = (await page.locator(".player-loading").allTextContents())
      .map((value) => value.trim().slice(0, 400)).filter(Boolean).slice(0, 3);
    const diagnostics = {
      url: page.url(), statuses, loading, dialogs: dialogs.slice(0, 5),
      pageErrors: pageErrors.slice(0, 5).map((value) => value.slice(0, 1_200)),
      runtimeExceptions: runtimeExceptions.slice(0, 5),
    };
    throw new Error(`RPG_ACCEPTANCE_PRODUCT_SAVE_UNAVAILABLE:${JSON.stringify(diagnostics)}`);
  }
}

async function revealProductToolbar(page) {
  const toolbar = page.locator(".player-toolbar");
  const visible = await toolbar.evaluate((element) => element.classList.contains("is-visible"));
  if (!visible) {
    await page.locator(".player-hud-handle").click();
  }
  await page.locator(".player-toolbar.is-visible").waitFor({ state: "visible" });
}

async function collectRuntimeException(cdp, details) {
  const objectId = details.exception?.objectId;
  let ownProperties = [];
  let runtimeStack = "";
  if (objectId) {
    const [properties, stack] = await Promise.all([
      cdp.send("Runtime.getProperties", {
        objectId, ownProperties: true, accessorPropertiesOnly: false, generatePreview: true,
      }).catch(() => ({ result: [] })),
      cdp.send("Runtime.callFunctionOn", {
        objectId,
        functionDeclaration: "function () { return typeof this.stack === 'string' ? this.stack : ''; }",
        returnByValue: true,
        silent: true,
      }).catch(() => ({ result: { value: "" } })),
    ]);
    ownProperties = properties.result ?? [];
    runtimeStack = stack.result?.value ?? "";
  }
  return safeRuntimeException(details, ownProperties, runtimeStack);
}

function safeRuntimeException(details, ownProperties = [], runtimeStack = "") {
  const frames = details.stackTrace?.callFrames ?? [];
  const properties = [
    ...(details.exception?.preview?.properties ?? []),
    ...ownProperties.map((property) => ({
      name: property.name,
      type: property.value?.type,
      value: property.value?.value ?? property.value?.description,
    })),
  ];
  return {
    text: String(details.text ?? "").slice(0, 240),
    description: String(details.exception?.description ?? "").slice(0, 600),
    stack: String(runtimeStack ?? "").slice(0, 2_400),
    properties: properties.slice(0, 16).map((property) => ({
      name: String(property.name ?? "").slice(0, 80),
      type: String(property.type ?? "").slice(0, 40),
      value: String(property.value ?? "").slice(0, 240),
    })),
    frames: frames.slice(0, 8).map((frame) => ({
      functionName: String(frame.functionName ?? "").slice(0, 160),
      url: safeStackUrl(frame.url),
      lineNumber: frame.lineNumber,
      columnNumber: frame.columnNumber,
    })),
  };
}

function safeStackUrl(value) {
  try {
    const url = new URL(value);
    return `${url.origin}${url.pathname}`;
  } catch {
    return "";
  }
}

async function approvedReview(request, itemId, gameId) {
  const history = await jsonRequest(request, "GET", "/api/v1/admin/review-history");
  const matches = array(history.items).filter((item) =>
    item.importItemId === itemId && item.decision === "APPROVED");
  exact(matches.length, 1, "RPG_ACCEPTANCE_APPROVED_REVIEW_COUNT");
  const event = await jsonRequest(
    request, "GET", `/api/v1/admin/review-history/${matches[0].reviewEventId}`,
  );
  if (event.importItemId !== itemId || event.eventType !== "APPROVED" || event.after?.gameId !== gameId) {
    throw new Error("RPG_ACCEPTANCE_APPROVED_REVIEW_RELATION_INVALID");
  }
  if (!matches[0].importJobId) { throw new Error("RPG_ACCEPTANCE_APPROVED_IMPORT_RELATION_MISSING"); }
  return { event, importJobId: matches[0].importJobId };
}

async function readInputTranscript(request, importJobId) {
  const imported = await jsonRequest(request, "GET", `/api/v1/admin/imports/${importJobId}`);
  const upload = await jsonRequest(request, "GET", `/api/v1/admin/uploads/${imported.uploadId}`);
  const counts = imported.counts ?? {};
  return {
    transportScheme: new URL(baseUrl).protocol === "https:" ? "HTTPS" : "HTTP_LOCALHOST",
    upload: {
      uploadId: upload.uploadId, state: upload.state, purpose: upload.purpose,
      sourceType: upload.sourceType, fileCount: array(upload.files).length,
      totalBytes: upload.totalBytes,
      receivedBytes: array(upload.files).reduce((total, file) => total + Number(file.receivedSizeBytes ?? 0), 0),
      finalizationNo: upload.finalizationNo,
    },
    import: {
      importJobId: imported.importJobId, uploadId: imported.uploadId, state: imported.state,
      payloadState: imported.payloadState, platformId: imported.platformId,
      defaultCoreId: imported.defaultCoreId, providerId: imported.providerId,
      targetId: imported.targetId,
      counts: {
        total: counts.total, queued: counts.queued, running: counts.running,
        reviewPending: counts.reviewPending, published: counts.published,
        discarded: counts.discarded, failed: counts.failed, cancelled: counts.cancelled,
        unresolvedRejectedFiles: counts.unresolvedRejectedFiles,
      },
      createdAtMs: imported.createdAtMs, updatedAtMs: imported.updatedAtMs,
    },
  };
}

async function collectOriginInventory(page, runtimeOrigin, responses) {
  const appOrigin = new URL(baseUrl).origin;
  const expectedRuntimeOrigin = new URL(runtimeOrigin).origin;
  const app = originResponseCounts(responses, appOrigin);
  const runtime = originResponseCounts(responses, expectedRuntimeOrigin);
  const browserState = await page.evaluate(async () => {
    const projectPath = (value) => {
      try {
        const pathname = new URL(value, document.baseURI).pathname;
        return pathname.startsWith("/__retrom/project/")
          || /^\/(?:js|data|audio|img|effects|fonts)\//.test(pathname);
      }
      catch { return false; }
    };
    const domProjectResourceReferences = [...document.querySelectorAll("[src],[href]")]
      .filter((element) => projectPath(element.getAttribute("src") ?? element.getAttribute("href") ?? "")).length;
    let cacheProjectResourceEntries = 0;
    if ("caches" in window) {
      for (const name of await caches.keys()) {
        const cache = await caches.open(name);
        cacheProjectResourceEntries += (await cache.keys()).filter((request) => projectPath(request.url)).length;
      }
    }
    return { domProjectResourceReferences, cacheProjectResourceEntries };
  });
  const unexpectedOrigins = [...new Set(responses.map((item) => {
    try { return new URL(item.url).origin; } catch { return ""; }
  }).filter((origin) => origin && origin !== "null" && ![appOrigin, expectedRuntimeOrigin].includes(origin)))].sort();
  return {
    appOrigin: { origin: appOrigin, ...app, ...browserState },
    runtimeOrigin: { origin: expectedRuntimeOrigin, ...runtime },
    unexpectedOrigins,
  };
}

function originResponseCounts(responses, origin) {
  const selected = responses.filter((item) => {
    try { return new URL(item.url).origin === origin; } catch { return false; }
  });
  return {
    documentResponses: selected.filter((item) => item.resourceType === "document").length,
    scriptResponses: selected.filter((item) => item.resourceType === "script").length,
    projectResourceResponses: selected.filter((item) => {
      try { return projectResourcePath(new URL(item.url).pathname); } catch { return false; }
    }).length,
  };
}

function projectResourcePath(pathname) {
  return pathname.startsWith("/__retrom/project/")
    || /^\/(?:js|data|audio|img|effects|fonts)\//.test(pathname);
}

async function allRuntimeTargets(request) {
  const items = [];
  let cursor = "";
  do {
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    const response = await jsonRequest(request, "GET", `/api/v1/admin/runtime-targets${query}`);
    items.push(...array(response.items));
    cursor = response.nextCursor ?? "";
  } while (cursor);
  return items;
}

async function jsonRequest(request, method, path, options = {}) {
  const response = await request.fetch(`${baseUrl}${path}`, {
    method, headers: options.headers, data: options.data, failOnStatusCode: false,
  });
  const expected = options.expected ?? 200;
  if (response.status() !== expected) {
    throw new Error(`RPG_ACCEPTANCE_HTTP_${method}_${response.status()}`);
  }
  return response.json();
}

function safeReview(review, validation) {
  const route = validation.routeEvidence;
  return {
    itemId: review.importItemId, reviewEventId: review.reviewEventId, decision: review.eventType,
    version: null, contentIdentityDigest: route.projectFingerprint,
    rpgMaker: {
      selectedCoreId: "rpgmaker", generation: route.generation,
      evidenceGeneration: route.evidenceGeneration, evidenceConfidence: route.evidenceConfidence,
      reviewVersionAtCreate: validation.reviewVersionAtCreate, runtimeValidationCurrent: true,
    },
  };
}

function safeValidation(value) {
  return {
    validationId: value.validationId, importItemId: value.importItemId,
    reviewVersionAtCreate: value.reviewVersionAtCreate,
    launchId: value.launchId, restoreLaunchId: value.restoreLaunchId, state: value.state,
    lastGateSequence: value.lastGateSequence, routeEvidence: value.routeEvidence,
    machineGates: value.machineGates, checkpointRoundTrip: value.checkpointRoundTrip,
    failureCode: value.failureCode, decision: value.decision,
    createdAtMs: value.createdAtMs, updatedAtMs: value.updatedAtMs, expiresAtMs: value.expiresAtMs,
  };
}

function safeTarget(item) {
  return {
    providerId: item.providerId, providerVersion: item.providerVersion, targetId: item.targetId,
    bundleSha256: item.bundleSha256,
    coreId: item.coreId, coreName: item.coreName,
  };
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_ACCEPTANCE_ENV_MISSING_${name}`); }
  return value;
}

function array(value) { return Array.isArray(value) ? value : []; }
function progress(stage) { process.stdout.write(`rpg_product_stage=${stage}\n`); }
function exact(actual, expected, code) {
  if (JSON.stringify(actual) !== JSON.stringify(expected)) { throw new Error(code); }
}
