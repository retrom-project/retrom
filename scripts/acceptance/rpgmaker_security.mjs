#!/usr/bin/env node
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import {
  createProductClient, directoryFiles, mergeFiles, overlayFile, reviewForImport,
  SecurityInputBlocked, singleFile,
} from "./rpgmaker_security_upload.mjs";
import {
  browserNavigationStatus, confusedRuntimeEntryURL, requireLocalRuntimeSite, runtimeBootstrapReplayStatus,
  runtimeFrameEligible, runtimeProjectStatus, runtimeRequestStatus,
} from "./rpgmaker_security_runtime.mjs";
import { normalizedBase } from "./rpgmaker_url.mjs";

const caseId = required("RETROM_RPG_CASE_ID");
const caseDir = required("RETROM_RPG_CASE_DIR");
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const fixtureRoot = resolve("testdata/public-roms/rpgmaker-smoke");
const matrix = JSON.parse(readFileSync(join(fixtureRoot, "negative-matrix/matrix.json"), "utf8"));
const chromeExecutablePath = required("RETROM_CHROME_EXECUTABLE");
const browser = await chromium.launch({ executablePath: chromeExecutablePath, headless: true });

try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: { Origin: baseUrl }, data: {
      username: required("RETROM_ACCEPTANCE_USERNAME"), password: required("RETROM_ACCEPTANCE_PASSWORD"),
    }, failOnStatusCode: false,
  });
  if (loginResponse.status() !== 200) { throw new Error(`RPG_ACCEPTANCE_LOGIN_${loginResponse.status()}`); }
  const login = await loginResponse.json();
  if (!login.csrfToken) { throw new Error("RPG_ACCEPTANCE_LOGIN_CSRF_MISSING"); }
  const client = createProductClient(context, baseUrl, login.csrfToken);
  const instances = await platformInstances(client);
  const payload = caseId === "ACC-RPG-010"
    ? await contentSafetyCase(context, client, instances)
    : await isolationCase(context, client, instances);
  writeFileSync(join(caseDir, "rpgmaker-product.json"), `${JSON.stringify(payload, null, 2)}\n`);
} catch (error) {
  if (!(error instanceof SecurityInputBlocked)) { throw error; }
  writeFileSync(join(caseDir, "rpgmaker-product.json"), `${JSON.stringify({
    schemaVersion: 1, caseId, status: "BLOCKED", reason: error.message, missingInputs: [],
  }, null, 2)}\n`);
  process.exitCode = 3;
} finally {
  await browser.close();
}

async function contentSafetyCase(context, client, instances) {
  const wrongCore = [];
  let familyTarget = null;
  const rejectedTargets = [];
  for (const source of matrix.wrongCore) {
    for (const target of source.targets) {
      const input = { source, target };
      if (target.accepted) { familyTarget = input; } else { rejectedTargets.push(input); }
    }
  }
  const beforeRejections = await sideEffectSnapshot(client);
  for (const { source, target } of rejectedTargets) {
    const outcome = await client.importProject(
      directoryFiles(join(fixtureRoot, source.fixture)), "DIRECTORY", instance(instances, target.coreId),
    );
    assertRejected(outcome, target.expectedCode);
    wrongCore.push({
      sourceGeneration: source.generation, selectedCoreId: target.coreId,
      accepted: false, status: outcome.status, code: outcome.body.error?.code,
      evidenceConfidence: null, bindingCreated: false, sideEffectBatch: "wrong-core-rejections-v1",
    });
  }
  const afterRejections = await sideEffectSnapshot(client);
  exact(afterRejections, beforeRejections, "RPG_ACCEPTANCE_WRONG_CORE_SIDE_EFFECT");
  if (!familyTarget || rejectedTargets.length !== 41) { throw new Error("RPG_ACCEPTANCE_WRONG_CORE_MATRIX_INVALID"); }
  const familyOutcome = await client.importProject(
    directoryFiles(join(fixtureRoot, familyTarget.source.fixture)), "DIRECTORY",
    instance(instances, familyTarget.target.coreId),
  );
  exact(familyOutcome.status, 202, "RPG_ACCEPTANCE_FAMILY_ONLY_STATUS");
  const familyReview = await reviewForImport(client, familyOutcome.body.importJobId);
  exact(familyReview.rpgMaker?.selectedCoreId, familyTarget.target.coreId, "RPG_ACCEPTANCE_FAMILY_ONLY_CORE");
  exact(
    familyReview.rpgMaker?.evidenceConfidence,
    familyTarget.target.evidenceConfidence,
    "RPG_ACCEPTANCE_FAMILY_ONLY_CONFIDENCE",
  );
  wrongCore.push({
    sourceGeneration: familyTarget.source.generation, selectedCoreId: familyTarget.target.coreId,
    accepted: true, status: familyOutcome.status, code: null,
    evidenceConfidence: familyReview.rpgMaker.evidenceConfidence,
    bindingCreated: true, sideEffectBatch: null,
  });
  const familyLaunch = await createValidationLaunch(
    context, client, familyReview, "acc-rpg-010-family-only.png",
  );
  exact(familyLaunch.config.routeKey, "RPG2003_EASYRPG_0811_V4", "RPG_ACCEPTANCE_FAMILY_ONLY_ROUTE");
  exact(familyLaunch.config.generation, "RPG2003", "RPG_ACCEPTANCE_FAMILY_ONLY_GENERATION");
  exact(familyLaunch.config.adapter?.engineMode, "rpg2k3", "RPG_ACCEPTANCE_FAMILY_ONLY_ENGINE");
  await completeOriginalValidation(familyLaunch.page, familyLaunch.frame, "RPG2000");
  const familyCheckpointed = await waitForValidation(
    client, familyReview.itemId, familyLaunch.validationId, "CHECKPOINTED",
  );
  const familyRestore = await createRestoreLaunch(
    context, client, familyReview, familyCheckpointed, "acc-rpg-010-family-only-restore.png",
  );
  await completeRestoreValidation(familyRestore.page, familyRestore.frame, "RPG2000");
  const familyValidation = await waitForValidation(
    client, familyReview.itemId, familyLaunch.validationId, "AWAITING_DECISION",
  );
  await familyLaunch.page.close();
  await familyRestore.page.close();

  const unsafe = [];
  let opaqueReview = null;
  for (const test of matrix.unsafe) {
    const files = unsafeFiles(test);
    const sourceType = test.sourceType === "FILES" ? "FILES" : "DIRECTORY";
    const outcome = await client.importProject(files, sourceType, instance(instances, test.coreId));
    if (test.accepted) {
      exact(outcome.status, 202, `RPG_ACCEPTANCE_UNSAFE_${test.name}_STATUS`);
      opaqueReview = await reviewForImport(client, outcome.body.importJobId);
    } else {
      assertRejected(outcome, test.expectedCode);
    }
    unsafe.push({ name: test.name, accepted: test.accepted, status: outcome.status, code: outcome.body.error?.code ?? null });
  }
  if (!opaqueReview) { throw new Error("RPG_ACCEPTANCE_OPAQUE_NATIVE_REVIEW_MISSING"); }
  const opaqueLaunch = await createValidationLaunch(context, client, opaqueReview, "acc-rpg-010-opaque-native.png", false);
  const opaqueNames = ["Game.exe", "nw.dll", "plugin.node", "launcher.bat"];
  const opaqueSourceFiles = opaqueNames.map((name) => {
    const source = opaqueReview.sourceFiles.find((file) => file.name === name);
    if (!source?.sha256) { throw new Error(`RPG_ACCEPTANCE_OPAQUE_NATIVE_SOURCE_MISSING_${name}`); }
    return { name, sha256: source.sha256, sizeBytes: source.sizeBytes };
  });
  const opaqueRuntime = [];
  for (const name of opaqueNames) {
    const status = await runtimeProjectStatus(opaqueLaunch.frame, name);
    exact(status, 404, `RPG_ACCEPTANCE_OPAQUE_NATIVE_RUNTIME_${name}`);
    opaqueRuntime.push({ name, status });
  }
  await cleanupNativeProjection(opaqueLaunch.frame);
  await finishOpenedValidationLaunch(opaqueLaunch.page, opaqueLaunch.launchId);
  await opaqueLaunch.page.close();

  const nestedArchives = [];
  for (const test of matrix.nestedArchives) {
    const base = directoryFiles(join(fixtureRoot, test.fixture));
    const sidecarPath = join(fixtureRoot, test.sidecar);
    const logicalName = `RetromNested/${basename(sidecarPath)}`;
    const files = overlayFile(base, sidecarPath, logicalName);
    const outcome = await client.importProject(files, "DIRECTORY", instance(instances, test.coreId));
    exact(outcome.status, 202, `RPG_ACCEPTANCE_NESTED_${test.generation}_STATUS`);
    const review = await reviewForImport(client, outcome.body.importJobId);
    const sidecar = review.sourceFiles.find((file) => file.name === logicalName);
    if (!sidecar?.sha256 || sidecar.archive !== false || sidecar.archiveEntries?.length !== 0) {
      throw new Error(`RPG_ACCEPTANCE_NESTED_${test.generation}_${test.format}_${test.detection}_RECURSED`);
    }
    const inspected = await inspectNestedProject(context, client, review, sidecar);
    const postInspection = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
    exact(
      postInspection.sourceManifest.filesDigest,
      review.sourceManifest.filesDigest,
      `RPG_ACCEPTANCE_NESTED_${test.generation}_SOURCE_MUTATED`,
    );
    nestedArchives.push({
      generation: test.generation, format: test.format, detection: test.detection,
      sidecar: logicalName, sha256: sidecar.sha256, sizeBytes: sidecar.sizeBytes,
      filesDigest: review.sourceManifest.filesDigest,
      postInspectionFilesDigest: postInspection.sourceManifest.filesDigest,
      nestedEntryCount: sidecar.archiveEntries.length,
      importJobId: outcome.body.importJobId, importItemId: review.itemId,
      contentIdentityDigest: review.contentIdentityDigest,
      validationId: inspected.validationId, launchId: inspected.launchId,
      routeKey: inspected.config.routeKey, artifactId: inspected.config.artifactId,
      adapterKind: inspected.config.adapter.adapterKind,
      projection: inspected.projection, launchFinished: inspected.launchFinished,
    });
  }
  return {
    schemaVersion: 1, caseId, status: "PASS", wrongCore,
    rejectedSideEffects: {
      schemaVersion: 1, batchId: "wrong-core-rejections-v1", attemptedCount: rejectedTargets.length,
      before: beforeRejections, after: afterRejections, unchanged: true,
      importJobCreated: false, reviewCreated: false,
      validationOrLaunchCreated: false, publishedGameCreated: false,
    },
    unsafe, nestedArchives,
    familyOnly: {
      importItemId: familyReview.itemId, selectedCoreId: familyReview.rpgMaker.selectedCoreId,
      evidenceGeneration: familyReview.rpgMaker.evidenceGeneration,
      evidenceConfidence: familyReview.rpgMaker.evidenceConfidence,
      validationId: familyLaunch.validationId, originalLaunchId: familyLaunch.launchId,
      restoreLaunchId: familyRestore.launchId, config: safeEasyConfig(familyLaunch.config),
      machineGates: familyValidation.machineGates,
      checkpointRoundTrip: familyValidation.checkpointRoundTrip,
    },
    opaqueNative: {
      importItemId: opaqueReview.itemId, generation: opaqueReview.rpgMaker.generation,
      filesDigest: opaqueReview.sourceManifest.filesDigest,
      sourceFiles: opaqueSourceFiles, runtimeProjection: opaqueRuntime,
      launchId: opaqueLaunch.launchId, runtimeOrigin: opaqueLaunch.runtimeOrigin, launchFinished: true,
    },
    screenshots: [
      "screenshots/acc-rpg-010-family-only.png",
      "screenshots/acc-rpg-010-family-only-restore.png",
      "screenshots/acc-rpg-010-opaque-native.png",
    ],
  };
}

async function isolationCase(context, client, instances) {
  const harnesses = [];
  for (const input of [
    { fixture: "malicious-rpgmv", coreId: "rpgmaker_mv", generation: "RPGMV" },
    { fixture: "malicious-rpgmz", coreId: "rpgmaker_mz", generation: "RPGMZ" },
  ]) {
    const outcome = await client.importProject(
      directoryFiles(join(fixtureRoot, input.fixture)), "DIRECTORY", instance(instances, input.coreId),
    );
    exact(outcome.status, 202, `RPG_ACCEPTANCE_ISOLATION_${input.generation}_IMPORT`);
    const review = await reviewForImport(client, outcome.body.importJobId);
    const originalScreenshot = `screenshots/acc-rpg-011-${input.generation.toLowerCase()}.png`;
    const restoreScreenshot = `screenshots/acc-rpg-011-${input.generation.toLowerCase()}-restore.png`;
    const launched = await createValidationLaunch(
      context, client, review, basename(originalScreenshot), true,
    );
    await waitForAutomaticValidationGates(launched.page);
    launched.bootstrap = await bootstrapChecks(
      context, launched.frame, launched.config, launched.runtimeOrigin,
    );
    await launched.page.bringToFront();
    await completeOriginalValidation(launched.page, launched.frame, input.generation);
    const checkpointed = await waitForValidation(client, review.itemId, launched.validationId, "CHECKPOINTED");
    launched.bootstrap.inactiveBootstrapStatus = await browserNavigationStatus(
      context, launched.config.adapter.bootstrapUrl,
    );
    const restored = await createRestoreLaunch(
      context, client, review, checkpointed, basename(restoreScreenshot),
    );
    await completeRestoreValidation(restored.page, restored.frame, input.generation);
    const validation = await waitForValidation(client, review.itemId, launched.validationId, "AWAITING_DECISION");
    harnesses.push({
      generation: input.generation, importItemId: review.itemId,
      validationId: launched.validationId, originalLaunchId: launched.launchId,
      restoreLaunchId: restored.launchId, runtimeOrigin: launched.runtimeOrigin,
      config: safeConfig(launched.config), originalScreenshot, restoreScreenshot,
      csp: launched.csp, probes: launched.probes, securityRequests: launched.securityRequests,
      bootstrap: launched.bootstrap, machineGates: validation.machineGates,
      checkpointRoundTrip: validation.checkpointRoundTrip,
    });
    await launched.page.close();
    await restored.page.close();
  }
  return {
    schemaVersion: 1, caseId, status: "PASS", harnesses,
    screenshots: harnesses.flatMap(({ originalScreenshot, restoreScreenshot }) =>
      [originalScreenshot, restoreScreenshot]),
  };
}

async function createValidationLaunch(context, client, review, screenshotName, inspectIsolation = false) {
  const response = await client.raw("POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations`, {
    headers: { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${review.version}"` },
    data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
  });
  if (response.status() !== 201) { throw new Error(`RPG_ACCEPTANCE_VALIDATION_CREATE_${response.status()}`); }
  const created = await response.json();
  return openValidationPlayer(context, client, created, screenshotName, inspectIsolation);
}

async function openValidationPlayer(context, client, created, screenshotName, inspectIsolation) {
  const page = await context.newPage();
  const securityRequests = [];
  let config = null;
  let csp = null;
  page.on("response", (response) => {
    if (response.url().endsWith("/__retrom/entry")) { csp = response.headers()["content-security-policy"] ?? null; }
    if (response.url().includes("example.invalid") || response.url().includes("/api/v1/health")) {
      securityRequests.push({ urlKind: response.url().includes("example.invalid") ? "external" : "nonAllowlistApi", status: response.status() });
    }
  });
  page.on("requestfailed", (request) => {
    if (request.url().includes("example.invalid")) { securityRequests.push({ urlKind: "external", status: 0 }); }
  });
  const configResponse = page.waitForResponse(
    (response) => response.url().includes(`/runtime/launches/${created.launchId}/config`) && response.status() === 200,
    { timeout: 120_000 },
  );
  await page.goto(`${baseUrl}${created.playerUrl}`, { waitUntil: "domcontentloaded" });
  config = await (await configResponse).json();
  requireLocalRuntimeSite(baseUrl, config.adapter?.uniqueOrigin);
  await page.waitForFunction(() => document.querySelector("iframe") !== null, null, { timeout: 120_000 });
  const frame = await waitForHarnessFrame(page, inspectIsolation, config.adapter?.uniqueOrigin);
  if (screenshotName) {
    await page.screenshot({ path: join(caseDir, "screenshots", screenshotName), fullPage: true });
  }
  const runtimeOrigin = new URL(frame.url()).origin;
  const nativeOrigin = config.adapter?.adapterKind === "NATIVE_WEB" ? config.adapter.uniqueOrigin : null;
  if (inspectIsolation && (runtimeOrigin === baseUrl || nativeOrigin !== runtimeOrigin)) {
    throw new Error("RPG_ACCEPTANCE_ISOLATION_ORIGIN_INVALID");
  }
  const probes = inspectIsolation ? await frame.evaluate(() => window.__RETROM_MALICIOUS_RESULTS__) : null;
  if (inspectIsolation) { validateIsolation(csp, probes, securityRequests); }
  return {
    page, frame, config, csp, probes, securityRequests, bootstrap: null,
    validationId: created.validationId, launchId: created.launchId, runtimeOrigin,
  };
}

async function createRestoreLaunch(context, client, review, validation, screenshotName) {
  const response = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${validation.validationId}/restore-launch`,
    { headers: { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${review.version}"` },
      data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } } },
  );
  if (response.status() !== 201) { throw new Error(`RPG_ACCEPTANCE_RESTORE_CREATE_${response.status()}`); }
  const created = await response.json();
  return openValidationPlayer(context, client, created, screenshotName, false);
}

async function waitForHarnessFrame(page, requireProbes, runtimeOrigin) {
  if (requireProbes && (typeof runtimeOrigin !== "string" || !runtimeOrigin)) {
    throw new Error("RPG_ACCEPTANCE_RUNTIME_ORIGIN_MISSING");
  }
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      if (frame === page.mainFrame() || !runtimeFrameEligible(frame.url(), runtimeOrigin) ||
        (requireProbes && !frame.url().includes("/__retrom/entry"))) { continue; }
      const ready = await frame.evaluate((probes) => {
        const canvas = document.querySelector("canvas");
        return Boolean(canvas && (!probes || window.__RETROM_MALICIOUS_RESULTS__?.complete === "true"));
      }, requireProbes).catch(() => false);
      if (ready) { return frame; }
    }
    await page.waitForTimeout(100);
  }
  throw new Error("RPG_ACCEPTANCE_ISOLATION_FRAME_TIMEOUT");
}

async function waitForAutomaticValidationGates(page) {
  await page.getByRole("button", { name: "输入已经生效", exact: true }).waitFor({
    state: "visible", timeout: 120_000,
  });
}

async function completeOriginalValidation(page, frame, generation) {
  const sequence = validationSequence(generation);
  await clickRuntimeAction(page, frame, "输入已经生效", sequence.input);
  await clickRuntimeAction(page, frame, "已听到游戏音频", []);
  await clickRuntimeAction(page, frame, "记录 B 并创建检查点", sequence.save);
  await clickRuntimeAction(page, frame, "记录 C 并结束原运行", sequence.diverge);
}

async function completeRestoreValidation(page, frame, generation) {
  await clickRuntimeAction(page, frame, "恢复后输入已经生效", validationSequence(generation).restore);
}

function validationSequence(generation) {
  return ["RPG2000", "RPG2003"].includes(generation)
    ? { input: ["ArrowLeft"], save: ["ArrowRight", "ArrowRight"],
      diverge: ["ArrowRight", "ArrowRight"],
      restore: ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"] }
    : { input: ["ArrowLeft"], save: ["ArrowRight", "Enter"],
      diverge: ["ArrowRight", "Enter"], restore: ["ArrowRight"] };
}

async function clickRuntimeAction(page, frame, label, keys) {
  const button = page.getByRole("button", { name: label, exact: true });
  await button.waitFor({ state: "visible", timeout: 120_000 });
  const canvas = frame.locator("canvas").first();
  await canvas.evaluate((element) => {
    element.tabIndex = 0;
    element.focus();
  });
  for (const key of keys) {
    await canvas.press(key, { delay: 250 });
    await page.waitForTimeout(800);
  }
  await button.click();
  await page.waitForTimeout(500);
  const alerts = await page.getByRole("alert").allInnerTexts();
  const message = alerts.map((value) => value.trim()).find(Boolean);
  if (message) { throw new Error(`RPG_ACCEPTANCE_RUNTIME_ACTION_${label}_${message}`); }
}

async function waitForValidation(client, itemId, validationId, expectedState) {
  for (let attempt = 0; attempt < 600; attempt += 1) {
    const validation = await client.json("GET", `/api/v1/admin/reviews/${itemId}/runtime-validations/${validationId}`);
    if (validation.state === expectedState) { return validation; }
    if (["FAILED", "EXPIRED", "PASSED"].includes(validation.state)) {
      throw new Error(`RPG_ACCEPTANCE_VALIDATION_${validation.state}_${validation.failureCode ?? "UNKNOWN"}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error(`RPG_ACCEPTANCE_VALIDATION_${expectedState}_TIMEOUT`);
}

async function sideEffectSnapshot(client) {
  return {
    importJobs: await resourceIdentity(client, "/api/v1/admin/imports", "id"),
    reviewItems: await resourceIdentity(client, "/api/v1/admin/reviews", "itemId"),
    games: await resourceIdentity(client, "/api/v1/admin/games", "gameId"),
  };
}

async function resourceIdentity(client, path, idField) {
  const identifiers = [];
  const seenCursors = new Set();
  let cursor = "";
  do {
    const separator = path.includes("?") ? "&" : "?";
    const query = `${path}${separator}limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`;
    const response = await client.json("GET", query);
    for (const item of response.items ?? []) {
      if (typeof item?.[idField] !== "string") { throw new Error("RPG_ACCEPTANCE_SIDE_EFFECT_ID_INVALID"); }
      identifiers.push(item[idField]);
    }
    cursor = response.nextCursor ?? "";
    if (cursor && seenCursors.has(cursor)) { throw new Error("RPG_ACCEPTANCE_SIDE_EFFECT_CURSOR_LOOP"); }
    if (cursor) { seenCursors.add(cursor); }
  } while (cursor);
  const sorted = [...new Set(identifiers)].sort();
  if (sorted.length !== identifiers.length) { throw new Error("RPG_ACCEPTANCE_SIDE_EFFECT_ID_DUPLICATE"); }
  return { count: sorted.length, sha256: sha256(Buffer.from(JSON.stringify(sorted))) };
}

async function inspectNestedProject(context, client, review, sidecar) {
  const created = await createInspectionLaunch(client, review);
  if (["RPGMV", "RPGMZ"].includes(review.rpgMaker?.generation)) {
    return inspectNativeNestedProject(context, client, created, sidecar);
  }
  let config = null;
  let launchActive = false;
  let projection;
  try {
    const response = await client.raw("GET", `/runtime/launches/${created.launchId}/config`);
    exact(response.status(), 200, "RPG_ACCEPTANCE_NESTED_CONFIG_STATUS");
    launchActive = true;
    config = await response.json();
    if (config.adapter?.adapterKind === "EASYRPG_WEB") {
      projection = await inspectEasyRPGProjection(client, config.adapter, sidecar);
    } else if (config.adapter?.adapterKind === "MKXP_LIBRETRO_WEB") {
      projection = await inspectMKXPProjection(client, config.adapter, sidecar);
    } else {
      throw new Error("RPG_ACCEPTANCE_NESTED_ADAPTER_INVALID");
    }
  } finally {
    if (launchActive) { await finishInspectionLaunch(client, created.launchId); }
  }
  return {
    validationId: created.validationId, launchId: created.launchId,
    config, projection, launchFinished: true,
  };
}

async function inspectNativeNestedProject(context, client, created, sidecar) {
  let launched = null;
  let launchActive = true;
  let runtimeActive = false;
  try {
    launched = await openValidationPlayer(context, client, created, null, false);
    runtimeActive = true;
    const projection = await inspectNativeProjection(launched.frame, sidecar);
    await cleanupNativeProjection(launched.frame);
    runtimeActive = false;
    await finishOpenedValidationLaunch(launched.page, launched.launchId);
    launchActive = false;
    return {
      validationId: created.validationId, launchId: created.launchId,
      config: launched.config, projection, launchFinished: true,
    };
  } finally {
    try {
      if (runtimeActive) { await cleanupNativeProjection(launched.frame); }
    } finally {
      if (launchActive) { await finishInspectionLaunch(client, created.launchId); }
      await launched?.page.close();
    }
  }
}

async function createInspectionLaunch(client, review) {
  const response = await client.raw("POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations`, {
    headers: {
      ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${review.version}"`,
    },
    data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
  });
  exact(response.status(), 201, "RPG_ACCEPTANCE_NESTED_VALIDATION_CREATE");
  return response.json();
}

async function finishInspectionLaunch(client, launchId) {
  const response = await client.raw("POST", `/runtime/launches/${launchId}/finish`, {
    headers: { Origin: baseUrl, "Content-Type": "application/json" },
    data: { clientSequence: 0, clientObservedAtMs: Date.now(), previousInterval: null },
  });
  exact(response.status(), 200, "RPG_ACCEPTANCE_NESTED_LAUNCH_FINISH_STATUS");
  const result = await response.json();
  exact(result.state, "FINISHED", "RPG_ACCEPTANCE_NESTED_LAUNCH_FINISH_STATE");
}

async function finishOpenedValidationLaunch(page, launchId) {
  const result = await page.evaluate(async (id) => {
    const response = await fetch(`/runtime/launches/${id}/finish`, {
      method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ clientSequence: 0, clientObservedAtMs: Date.now(), previousInterval: null }),
    });
    return { status: response.status, body: await response.json().catch(() => null) };
  }, launchId);
  exact(result.status, 200, "RPG_ACCEPTANCE_OPAQUE_LAUNCH_FINISH_STATUS");
  exact(result.body?.state, "FINISHED", "RPG_ACCEPTANCE_OPAQUE_LAUNCH_FINISH_STATE");
}

async function inspectEasyRPGProjection(client, adapter, sidecar) {
  const indexResponse = await client.raw("GET", adapter.projectIndexUrl);
  exact(indexResponse.status(), 200, "RPG_ACCEPTANCE_NESTED_EASY_INDEX_STATUS");
  const indexBytes = Buffer.from(await indexResponse.body());
  const index = JSON.parse(indexBytes.toString("utf8"));
  if (!easyRPGIndexPaths(index.cache).includes(sidecar.name)) {
    throw new Error("RPG_ACCEPTANCE_NESTED_EASY_INDEX_MEMBER_MISSING");
  }
  const response = await client.raw("GET", `${adapter.projectRootUrl}${encodedLogicalPath(sidecar.name)}`);
  exact(response.status(), 200, "RPG_ACCEPTANCE_NESTED_EASY_CONTENT_STATUS");
  const contents = Buffer.from(await response.body());
  exactBytes(contents, sidecar, "RPG_ACCEPTANCE_NESTED_EASY_CONTENT");
  return {
    kind: "EASYRPG_PROJECT_FILE", status: response.status(), logicalName: sidecar.name,
    sha256: sha256(contents), sizeBytes: contents.length, containerSha256: sha256(indexBytes), exactMember: true,
  };
}

function easyRPGIndexPaths(cache, prefix = []) {
  if (!cache || typeof cache !== "object" || Array.isArray(cache)) {
    throw new Error("RPG_ACCEPTANCE_NESTED_EASY_INDEX_INVALID");
  }
  const paths = [];
  for (const [key, value] of Object.entries(cache)) {
    if (key === "_dirname") { continue; }
    if (typeof value === "string") { paths.push([...prefix, value].join("/")); }
    else {
      const directory = value?._dirname;
      if (typeof directory !== "string") { throw new Error("RPG_ACCEPTANCE_NESTED_EASY_INDEX_INVALID"); }
      paths.push(...easyRPGIndexPaths(value, [...prefix, directory]));
    }
  }
  return paths;
}

async function inspectMKXPProjection(client, adapter, sidecar) {
  const response = await client.raw("GET", adapter.projectArchive.url);
  exact(response.status(), 200, "RPG_ACCEPTANCE_NESTED_MKXP_ARCHIVE_STATUS");
  const archive = Buffer.from(await response.body());
  exact(archive.length, adapter.projectArchive.sizeBytes, "RPG_ACCEPTANCE_NESTED_MKXP_ARCHIVE_SIZE");
  exact(sha256(archive), adapter.projectArchive.sha256, "RPG_ACCEPTANCE_NESTED_MKXP_ARCHIVE_SHA");
  const contents = storedZIPMember(archive, sidecar.name);
  exactBytes(contents, sidecar, "RPG_ACCEPTANCE_NESTED_MKXP_MEMBER");
  return {
    kind: "MKXP_ARCHIVE_MEMBER", status: response.status(), logicalName: sidecar.name,
    sha256: sha256(contents), sizeBytes: contents.length,
    containerSha256: adapter.projectArchive.sha256, exactMember: true,
  };
}

function storedZIPMember(archive, expectedName) {
  const eocd = findEndOfCentralDirectory(archive);
  const diskEntries = archive.readUInt16LE(eocd + 8);
  const count = archive.readUInt16LE(eocd + 10);
  const centralSize = archive.readUInt32LE(eocd + 12);
  const centralOffset = archive.readUInt32LE(eocd + 16);
  if (archive.readUInt16LE(eocd + 4) !== 0 || archive.readUInt16LE(eocd + 6) !== 0 ||
      count === 0xffff || diskEntries !== count || centralOffset + centralSize !== eocd) {
    throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_ZIP64_UNSUPPORTED");
  }
  let offset = centralOffset;
  let found = null;
  for (let index = 0; index < count; index += 1) {
    requireZIPRange(archive, offset, 46);
    if (archive.readUInt32LE(offset) !== 0x02014b50) { throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_CENTRAL_INVALID"); }
    const method = archive.readUInt16LE(offset + 10);
    const compressedSize = archive.readUInt32LE(offset + 20);
    const uncompressedSize = archive.readUInt32LE(offset + 24);
    const nameLength = archive.readUInt16LE(offset + 28);
    const extraLength = archive.readUInt16LE(offset + 30);
    const commentLength = archive.readUInt16LE(offset + 32);
    const localOffset = archive.readUInt32LE(offset + 42);
    requireZIPRange(archive, offset + 46, nameLength + extraLength + commentLength);
    const name = archive.subarray(offset + 46, offset + 46 + nameLength).toString("utf8");
    if (name === expectedName) {
      if (found || method !== 0 || compressedSize !== uncompressedSize) {
        throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_MEMBER_INVALID");
      }
      found = storedZIPLocalContents(archive, localOffset, compressedSize, expectedName);
    }
    offset += 46 + nameLength + extraLength + commentLength;
  }
  if (offset !== centralOffset + centralSize || !found) {
    throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_MEMBER_MISSING");
  }
  return found;
}

function findEndOfCentralDirectory(archive) {
  if (archive.length < 22) { throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_EOCD_MISSING"); }
  const minimum = Math.max(0, archive.length - 65_557);
  for (let offset = archive.length - 22; offset >= minimum; offset -= 1) {
    if (archive.readUInt32LE(offset) === 0x06054b50 && offset + 22 + archive.readUInt16LE(offset + 20) === archive.length) {
      return offset;
    }
  }
  throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_EOCD_MISSING");
}

function storedZIPLocalContents(archive, offset, size, expectedName) {
  requireZIPRange(archive, offset, 30);
  if (archive.readUInt32LE(offset) !== 0x04034b50 || archive.readUInt16LE(offset + 8) !== 0) {
    throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_LOCAL_INVALID");
  }
  const nameLength = archive.readUInt16LE(offset + 26);
  const extraLength = archive.readUInt16LE(offset + 28);
  requireZIPRange(archive, offset + 30, nameLength + extraLength);
  if (archive.subarray(offset + 30, offset + 30 + nameLength).toString("utf8") !== expectedName) {
    throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_LOCAL_NAME_INVALID");
  }
  const start = offset + 30 + nameLength + extraLength;
  requireZIPRange(archive, start, size);
  return archive.subarray(start, start + size);
}

function requireZIPRange(archive, offset, length) {
  if (!Number.isSafeInteger(offset) || !Number.isSafeInteger(length) || offset < 0 || length < 0
      || offset + length > archive.length) {
    throw new Error("RPG_ACCEPTANCE_NESTED_MKXP_RANGE_INVALID");
  }
}

async function inspectNativeProjection(frame, sidecar) {
  const status = await runtimeProjectStatus(frame, sidecar.name);
  exact(status, 404, "RPG_ACCEPTANCE_NESTED_NATIVE_CONTENT_STATUS");
  return {
    kind: "NATIVE_WEB_DENIED", status, logicalName: sidecar.name,
    sha256: null, sizeBytes: null, containerSha256: null, exactMember: false,
  };
}

async function cleanupNativeProjection(frame) {
  const status = await runtimeRequestStatus(frame, "/__retrom/cleanup", "POST");
  exact(status, 204, "RPG_ACCEPTANCE_NESTED_NATIVE_CLEANUP_STATUS");
}

function encodedLogicalPath(value) {
  return value.split("/").map((segment) => encodeURIComponent(segment)).join("/");
}

function exactBytes(contents, sidecar, code) {
  if (contents.length !== sidecar.sizeBytes || sha256(contents) !== sidecar.sha256) { throw new Error(code); }
}

function sha256(contents) {
  return createHash("sha256").update(contents).digest("hex");
}

async function bootstrapChecks(context, frame, config, runtimeOrigin) {
  const isolated = config.adapter?.adapterKind === "NATIVE_WEB" ? config.adapter : null;
  if (!isolated?.bootstrapUrl || !isolated.bootstrapTicket) { throw new Error("RPG_ACCEPTANCE_BOOTSTRAP_CONFIG_MISSING"); }
  const authenticatedReloadStatus = await browserNavigationStatus(context, isolated.bootstrapUrl);
  const replayStatus = await runtimeBootstrapReplayStatus(frame, isolated.bootstrapTicket);
  const appHostEntry = await context.request.get(`${baseUrl}/__retrom/entry`, { failOnStatusCode: false });
  const runtimeApiStatus = await runtimeRequestStatus(frame, "/api/v1/admin/reviews", "GET");
  const confusedHostStatus = await browserNavigationStatus(
    context, confusedRuntimeEntryURL(runtimeOrigin),
  );
  return {
    authenticatedReloadStatus, replayStatus,
    appHostEntryStatus: appHostEntry.status(), runtimeApiStatus,
    confusedHostStatus, inactiveBootstrapStatus: null,
  };
}

function validateIsolation(csp, probes, securityRequests) {
  if (!csp?.includes("base-uri 'self'") || !csp.includes("worker-src 'self' blob:")) {
    throw new Error("RPG_ACCEPTANCE_ISOLATION_CSP_INVALID");
  }
  for (const name of ["parentDom", "topNavigation", "popup", "externalFetch", "serviceWorker"]) {
    exact(probes?.[name], "blocked", `RPG_ACCEPTANCE_ISOLATION_${name}`);
  }
  exact(probes?.appCookie, "none", "RPG_ACCEPTANCE_ISOLATION_COOKIE");
  exact(probes?.nonAllowlistApi, "404", "RPG_ACCEPTANCE_ISOLATION_NON_ALLOWLIST_API");
  if (!["attempted", "blocked"].includes(probes?.form) ||
      securityRequests.some((request) => request.urlKind === "external" && request.status !== 0) ||
      !securityRequests.some((request) => request.urlKind === "nonAllowlistApi" && request.status === 404)) {
    throw new Error("RPG_ACCEPTANCE_ISOLATION_EXTERNAL_REQUEST_NOT_BLOCKED");
  }
}

function unsafeFiles(test) {
  if (test.sourceType === "FILES") { return singleFile(join(fixtureRoot, test.fixture)); }
  if (test.sourceType === "COMPOSITE") {
    return mergeFiles(
      directoryFiles(join(fixtureRoot, "malicious-rpgmv")),
      directoryFiles(join(fixtureRoot, "malicious-rpgmz")),
    );
  }
  if (test.sourceType === "GENCACHE_COMPOSITE") {
    return mergeFiles(
      directoryFiles(join(fixtureRoot, "rpg2000")),
      directoryFiles(join(fixtureRoot, test.fixture)),
    );
  }
  return directoryFiles(join(fixtureRoot, test.fixture));
}

async function platformInstances(client) {
  const result = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  const values = new Map();
  for (const item of result.items ?? []) {
    if (item.enabled && item.defaultCoreId) { values.set(item.defaultCoreId, item.id); }
  }
  return values;
}

function instance(instances, coreId) {
  const value = instances.get(coreId);
  if (!value) { throw new Error(`RPG_ACCEPTANCE_PLATFORM_INSTANCE_MISSING_${coreId}`); }
  return value;
}

function assertRejected(outcome, expectedCode) {
  if (![400, 409, 413, 422].includes(outcome.status) || outcome.body.error?.code !== expectedCode) {
    throw new Error(`RPG_ACCEPTANCE_SECURITY_REJECTION_${expectedCode}_${outcome.status}_${outcome.body.error?.code ?? "NONE"}`);
  }
}

function safeConfig(config) {
  return {
    runtimeFamily: config.runtimeFamily, generation: config.generation, coreId: config.coreId,
    routeKey: config.routeKey, artifactId: config.artifactId, adapterId: config.adapter?.adapterId,
  };
}

function safeEasyConfig(config) {
  return {
    ...safeConfig(config), adapterKind: config.adapter?.adapterKind, engineMode: config.adapter?.engineMode,
  };
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_ACCEPTANCE_ENV_MISSING_${name}`); }
  return value;
}

function exact(actual, expected, code) {
  if (JSON.stringify(actual) !== JSON.stringify(expected)) { throw new Error(code); }
}
