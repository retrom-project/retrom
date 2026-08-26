#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import {
  createProductClient, directoryFiles, mergeFiles, overlayFile, reviewForImport, singleFile,
} from "./rpgmaker_security_upload.mjs";

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
} finally {
  await browser.close();
}

async function contentSafetyCase(context, client, instances) {
  const wrongCore = [];
  let familyReview = null;
  for (const source of matrix.wrongCore) {
    const files = directoryFiles(join(fixtureRoot, source.fixture));
    for (const target of source.targets) {
      const outcome = await client.importProject(files, "DIRECTORY", instance(instances, target.coreId));
      if (target.accepted) {
        exact(outcome.status, 202, "RPG_ACCEPTANCE_FAMILY_ONLY_STATUS");
        familyReview = await reviewForImport(client, outcome.body.importJobId);
        exact(familyReview.rpgMaker?.selectedCoreId, target.coreId, "RPG_ACCEPTANCE_FAMILY_ONLY_CORE");
        exact(familyReview.rpgMaker?.evidenceConfidence, target.evidenceConfidence, "RPG_ACCEPTANCE_FAMILY_ONLY_CONFIDENCE");
      } else {
        assertRejected(outcome, target.expectedCode);
      }
      wrongCore.push({
        sourceGeneration: source.generation, selectedCoreId: target.coreId,
        accepted: target.accepted, status: outcome.status,
        code: target.accepted ? null : outcome.body.error?.code,
        evidenceConfidence: target.accepted ? familyReview.rpgMaker.evidenceConfidence : null,
      });
    }
  }
  if (!familyReview) { throw new Error("RPG_ACCEPTANCE_FAMILY_ONLY_MISSING"); }
  const familyLaunch = await createValidationLaunch(context, client, familyReview, "acc-rpg-010-family-only.png");
  exact(familyLaunch.config.routeKey, "RPG2003_EASYRPG_0811_V4", "RPG_ACCEPTANCE_FAMILY_ONLY_ROUTE");
  exact(familyLaunch.config.generation, "RPG2003", "RPG_ACCEPTANCE_FAMILY_ONLY_GENERATION");
  await familyLaunch.page.close();

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
  const opaqueLaunch = await createValidationLaunch(context, client, opaqueReview, "acc-rpg-010-opaque-native.png", true);
  const opaqueNames = ["Game.exe", "nw.dll", "plugin.node", "launcher.bat"];
  const opaqueSourceFiles = opaqueNames.map((name) => {
    const source = opaqueReview.sourceFiles.find((file) => file.name === name);
    if (!source?.sha256) { throw new Error(`RPG_ACCEPTANCE_OPAQUE_NATIVE_SOURCE_MISSING_${name}`); }
    return { name, sha256: source.sha256, sizeBytes: source.sizeBytes };
  });
  const opaqueRuntime = [];
  for (const name of opaqueNames) {
    const response = await context.request.get(
      `${opaqueLaunch.runtimeOrigin}/__retrom/project/${encodeURIComponent(name)}`, { failOnStatusCode: false },
    );
    exact(response.status(), 404, `RPG_ACCEPTANCE_OPAQUE_NATIVE_RUNTIME_${name}`);
    opaqueRuntime.push({ name, status: response.status() });
  }
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
    nestedArchives.push({
      generation: test.generation, format: test.format, detection: test.detection,
      sidecar: logicalName, sha256: sidecar.sha256, sizeBytes: sidecar.sizeBytes,
      filesDigest: review.sourceManifest.filesDigest, nestedEntryCount: sidecar.archiveEntries.length,
      importJobId: outcome.body.importJobId, importItemId: review.itemId,
      contentIdentityDigest: review.contentIdentityDigest,
    });
  }
  return {
    schemaVersion: 1, caseId, status: "PASS", wrongCore, unsafe, nestedArchives,
    familyOnly: {
      importItemId: familyReview.itemId, selectedCoreId: familyReview.rpgMaker.selectedCoreId,
      evidenceGeneration: familyReview.rpgMaker.evidenceGeneration,
      evidenceConfidence: familyReview.rpgMaker.evidenceConfidence,
      validationId: familyLaunch.validationId, launchId: familyLaunch.launchId,
      runtimeOrigin: familyLaunch.runtimeOrigin, config: safeConfig(familyLaunch.config),
    },
    opaqueNative: {
      importItemId: opaqueReview.itemId, generation: opaqueReview.rpgMaker.generation,
      filesDigest: opaqueReview.sourceManifest.filesDigest,
      sourceFiles: opaqueSourceFiles, runtimeProjection: opaqueRuntime,
      launchId: opaqueLaunch.launchId, runtimeOrigin: opaqueLaunch.runtimeOrigin,
    },
    screenshots: ["screenshots/acc-rpg-010-family-only.png", "screenshots/acc-rpg-010-opaque-native.png"],
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
    const launched = await createValidationLaunch(
      context, client, review, `acc-rpg-011-${input.generation.toLowerCase()}.png`, true,
    );
    await completeOriginalValidation(launched.page, launched.frame);
    const checkpointed = await waitForValidation(client, review.itemId, launched.validationId, "CHECKPOINTED");
    const inactiveBootstrap = await context.request.get(launched.config.adapter.bootstrapUrl, {
      failOnStatusCode: false, maxRedirects: 0,
    });
    launched.bootstrap.inactiveBootstrapStatus = inactiveBootstrap.status();
    const restored = await createRestoreLaunch(context, client, review, checkpointed);
    await completeRestoreValidation(restored.page, restored.frame);
    const validation = await waitForValidation(client, review.itemId, launched.validationId, "AWAITING_DECISION");
    harnesses.push({
      generation: input.generation, importItemId: review.itemId,
      validationId: launched.validationId, originalLaunchId: launched.launchId,
      restoreLaunchId: restored.launchId, runtimeOrigin: launched.runtimeOrigin,
      csp: launched.csp, probes: launched.probes, securityRequests: launched.securityRequests,
      bootstrap: launched.bootstrap, machineGates: validation.machineGates,
      checkpointRoundTrip: validation.checkpointRoundTrip,
    });
    await launched.page.close();
    await restored.page.close();
  }
  return {
    schemaVersion: 1, caseId, status: "PASS", harnesses,
    screenshots: ["screenshots/acc-rpg-011-rpgmv.png", "screenshots/acc-rpg-011-rpgmz.png"],
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
  page.on("response", async (response) => {
    if (response.url().includes(`/runtime/launches/${created.launchId}/config`)) {
      config = await response.json();
    }
    if (response.url().endsWith("/__retrom/entry")) { csp = response.headers()["content-security-policy"] ?? null; }
    if (response.url().includes("example.invalid") || response.url().includes("/api/v1/health")) {
      securityRequests.push({ urlKind: response.url().includes("example.invalid") ? "external" : "nonAllowlistApi", status: response.status() });
    }
  });
  page.on("requestfailed", (request) => {
    if (request.url().includes("example.invalid")) { securityRequests.push({ urlKind: "external", status: 0 }); }
  });
  await page.goto(`${baseUrl}${created.playerUrl}`, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => document.querySelector("iframe") !== null, null, { timeout: 120_000 });
  const frame = await waitForHarnessFrame(page, inspectIsolation);
  await page.screenshot({ path: join(caseDir, "screenshots", screenshotName), fullPage: true });
  if (!config) {
    await page.waitForTimeout(250);
    if (!config) { throw new Error("RPG_ACCEPTANCE_ISOLATION_CONFIG_MISSING"); }
  }
  const runtimeOrigin = new URL(frame.url()).origin;
  const nativeOrigin = config.adapter?.adapterKind === "NATIVE_WEB" ? config.adapter.uniqueOrigin : null;
  if (inspectIsolation && (runtimeOrigin === baseUrl || nativeOrigin !== runtimeOrigin)) {
    throw new Error("RPG_ACCEPTANCE_ISOLATION_ORIGIN_INVALID");
  }
  const probes = inspectIsolation ? await frame.evaluate(() => window.__RETROM_MALICIOUS_RESULTS__) : null;
  const bootstrap = inspectIsolation ? await bootstrapChecks(context, config, runtimeOrigin) : null;
  if (inspectIsolation) { validateIsolation(csp, probes, securityRequests); }
  return {
    page, frame, config, csp, probes, securityRequests, bootstrap,
    validationId: created.validationId, launchId: created.launchId, runtimeOrigin,
  };
}

async function createRestoreLaunch(context, client, review, validation) {
  const response = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${validation.validationId}/restore-launch`,
    { headers: { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${review.version}"` },
      data: { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } } },
  );
  if (response.status() !== 201) { throw new Error(`RPG_ACCEPTANCE_RESTORE_CREATE_${response.status()}`); }
  const created = await response.json();
  return openValidationPlayer(context, client, created, "acc-rpg-011-restore.png", false);
}

async function waitForHarnessFrame(page, requireProbes) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      if (frame === page.mainFrame() || (requireProbes && !frame.url().includes("/__retrom/entry"))) { continue; }
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

async function completeOriginalValidation(page, frame) {
  await clickRuntimeAction(page, frame, "输入已经生效", ["ArrowRight"]);
  await clickRuntimeAction(page, frame, "已听到游戏音频", []);
  await clickRuntimeAction(page, frame, "记录 B 并创建检查点", ["ArrowRight", "Enter"]);
  await clickRuntimeAction(page, frame, "记录 C 并结束原运行", ["ArrowRight", "Enter"]);
}

async function completeRestoreValidation(page, frame) {
  await clickRuntimeAction(page, frame, "恢复后输入已经生效", ["ArrowRight"]);
}

async function clickRuntimeAction(page, frame, label, keys) {
  const button = page.getByRole("button", { name: label, exact: true });
  await button.waitFor({ state: "visible", timeout: 120_000 });
  await frame.locator("canvas").evaluate((element) => {
    element.tabIndex = 0;
    element.focus();
  });
  for (const key of keys) { await page.keyboard.press(key); }
  await page.waitForTimeout(800);
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

async function bootstrapChecks(context, config, runtimeOrigin) {
  const isolated = config.adapter?.adapterKind === "NATIVE_WEB" ? config.adapter : null;
  if (!isolated?.bootstrapUrl || !isolated.bootstrapTicket) { throw new Error("RPG_ACCEPTANCE_BOOTSTRAP_CONFIG_MISSING"); }
  const reload = await context.request.get(isolated.bootstrapUrl, { failOnStatusCode: false, maxRedirects: 0 });
  const replay = await context.request.post(isolated.bootstrapUrl, {
    headers: { Origin: runtimeOrigin, "Content-Type": "application/json" },
    data: { ticket: isolated.bootstrapTicket }, failOnStatusCode: false,
  });
  const appHostEntry = await context.request.get(`${baseUrl}/__retrom/entry`, { failOnStatusCode: false });
  const runtimeAPI = await context.request.get(`${runtimeOrigin}/api/v1/admin/reviews`, { failOnStatusCode: false });
  const parsedRuntime = new URL(runtimeOrigin);
  const suffix = parsedRuntime.hostname.slice(parsedRuntime.hostname.indexOf("."));
  const confusedHost = await context.request.get(
    `${parsedRuntime.protocol}//not-a-launch${suffix}/__retrom/entry`, { failOnStatusCode: false },
  );
  return {
    authenticatedReloadStatus: reload.status(), replayStatus: replay.status(),
    appHostEntryStatus: appHostEntry.status(), runtimeApiStatus: runtimeAPI.status(),
    confusedHostStatus: confusedHost.status(), inactiveBootstrapStatus: null,
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

function exact(actual, expected, code) {
  if (JSON.stringify(actual) !== JSON.stringify(expected)) { throw new Error(code); }
}
