#!/usr/bin/env node
import { createHash, randomUUID } from "node:crypto";
import {
  closeSync, existsSync, lstatSync, openSync, readFileSync, readSync, writeFileSync,
} from "node:fs";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { dirname, isAbsolute, resolve } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import {
  createProductClient, directoryFiles, reviewForImport,
} from "./rpgmaker_security_upload.mjs";
import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";

const cases = {
  "ACC-RPG-002": {
    coreId: "rpgmaker", generation: "RPG2000", targetId: "rpgmaker-2000",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpg2000"),
    prefix: "rpg2000/", saveKeys: ["ArrowRight", "ArrowRight"],
    divergeKeys: ["ArrowRight", "ArrowRight"],
    restoreKeys: ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"],
  },
  "ACC-RPG-003": {
    coreId: "rpgmaker", generation: "RPG2003", targetId: "rpgmaker-2003",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpg2003"),
    prefix: "rpg2003/", saveKeys: ["ArrowRight", "ArrowRight"],
    divergeKeys: ["ArrowRight", "ArrowRight"],
    restoreKeys: ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"],
  },
  "ACC-RPG-004": {
    coreId: "rpgmaker", generation: "RPGXP", targetId: "rpgmaker-xp",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpgxp"),
    prefix: "rpgxp/", saveKeys: ["ArrowRight", "KeyX"],
    divergeKeys: ["ArrowRight", "KeyX"], restoreKeys: ["ArrowRight", "KeyX"],
  },
  "ACC-RPG-005": {
    coreId: "rpgmaker", generation: "RPGVX", targetId: "rpgmaker-vx",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpgvx"),
    prefix: "rpgvx/", saveKeys: ["ArrowRight", "KeyX"],
    divergeKeys: ["ArrowRight", "KeyX"], restoreKeys: ["ArrowRight", "KeyX"],
  },
  "ACC-RPG-006": {
    coreId: "rpgmaker", generation: "RPGVXACE", targetId: "rpgmaker-vx-ace",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpgvxace"),
    prefix: "rpgvxace/", saveKeys: ["ArrowRight", "KeyX"],
    divergeKeys: ["ArrowRight", "KeyX"], restoreKeys: ["ArrowRight", "KeyX"],
  },
  "ACC-RPG-007": {
    coreId: "rpgmaker", generation: "RPGMV", targetId: "rpgmaker-mv",
    source: () => resolve("testdata/public-roms/rpgmaker-smoke/rpgmv"),
    prefix: "rpgmv/", saveKeys: ["ArrowRight", "Enter"],
    divergeKeys: ["ArrowRight", "Enter"], restoreKeys: ["ArrowRight", "Enter"],
  },
  "ACC-RPG-008": {
    coreId: "rpgmaker", generation: "RPGMZ", targetId: "rpgmaker-mz",
    source: () => resolve(required("RPG_MZ_SMOKE_ROOT")),
    prefix: "rpgmz/", saveKeys: ["ArrowRight", "Enter"],
    divergeKeys: ["ArrowRight", "Enter"], restoreKeys: ["ArrowRight", "Enter"],
  },
};
const falseCapabilities = {
  secureContext: false, crossOriginIsolated: false, sharedArrayBuffer: false,
};
const trueCapabilities = {
  secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true,
};
const caseId = process.argv[2];
const config = cases[caseId];
if (!config) { throw new Error("usage: rpgmaker_generation_provision.mjs ACC-RPG-002..ACC-RPG-008"); }
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const tracePath = caseId === "ACC-RPG-004"
  ? optionalTracePath(process.env.RETROM_ACC_RPG_004_TRACE)
  : null;
const sourceFiles = directoryFiles(config.source(), config.prefix);
if (caseId === "ACC-RPG-008") {
  validateMZProvenance(sourceFiles, required("RPG_MZ_SMOKE_PROVENANCE"));
}
const localProxy = await localRpgAcceptanceProxy(baseUrl);
const browser = await chromium.launch({
  executablePath: required("RETROM_CHROME_EXECUTABLE"), headless: true,
});

try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 }, ...localProxy.contextOptions,
  });
  const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: { Origin: baseUrl },
    data: {
      username: required("RETROM_ACCEPTANCE_USERNAME"),
      password: required("RETROM_ACCEPTANCE_PASSWORD"),
    },
    failOnStatusCode: false,
  });
  exact(loginResponse.status(), 200, "RPG_PROVISION_LOGIN_FAILED");
  const login = await loginResponse.json();
  if (!login.csrfToken) { throw new Error("RPG_PROVISION_CSRF_MISSING"); }
  const client = createProductClient(context, baseUrl, login.csrfToken);
  const platformInstanceId = await platformInstance(client);
  await assertSelectedRoute(client);
  const review = await provisionReview(client, sourceFiles, platformInstanceId);
  exact(review.rpgMaker?.selectedCoreId, config.coreId, "RPG_PROVISION_CORE_MISMATCH");
  exact(review.rpgMaker?.generation, config.generation, "RPG_PROVISION_GENERATION_MISMATCH");
  const published = await validateAndPublish(context, client, review);
  if (tracePath) {
    const trace = xpTrace(published);
    writeFileSync(tracePath, `${JSON.stringify(trace, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  }
  process.stdout.write(`${JSON.stringify({
    schemaVersion: 1, caseId, importItemId: review.itemId,
    validationId: published.validation.validationId, gameId: published.gameId,
    providerId: "retrom-runtime", targetId: config.targetId, xpTraceWritten: Boolean(tracePath),
  }, null, 2)}\n`);
} finally {
  await browser.close();
  await localProxy.close();
}

async function provisionReview(client, sourceFiles, platformInstanceId) {
  const resumeItemId = process.env.RETROM_RPG_PROVISION_RESUME_ITEM_ID;
  if (!resumeItemId) {
    const imported = await client.importProject(sourceFiles, "DIRECTORY", platformInstanceId);
    exact(imported.status, 202, "RPG_PROVISION_IMPORT_FAILED");
    return reviewForImport(client, imported.body.importJobId);
  }
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(resumeItemId)) {
    throw new Error("RPG_PROVISION_RESUME_REVIEW_INVALID");
  }
  const review = await client.json("GET", `/api/v1/admin/reviews/${resumeItemId}`);
  const expected = projectManifest(sourceFiles, config.prefix);
  const validation = review.rpgMaker?.runtimeValidation;
  const validationCanBeReplaced = validation === null
    || review.rpgMaker?.runtimeValidationCurrent !== true
    || ["FAILED", "EXPIRED"].includes(validation?.state);
  if (review.itemId !== resumeItemId || !validationCanBeReplaced
      || review.rpgMaker?.selectedCoreId !== config.coreId
      || review.rpgMaker?.generation !== config.generation
      || review.sourceManifest?.contentKind !== "RPG_MAKER_PROJECT"
      || review.sourceManifest?.fileCount !== expected.fileCount
      || review.sourceManifest?.totalBytes !== expected.totalBytes
      || review.sourceManifest?.filesDigest !== expected.filesDigest) {
    throw new Error("RPG_PROVISION_RESUME_REVIEW_INVALID");
  }
  return review;
}

function projectManifest(files, prefix) {
  const hasher = createHash("sha256");
  hasher.update(Buffer.from("RETROM_FILESET_V1\0", "ascii"));
  let totalBytes = 0;
  const entries = files.map((file) => {
    if (!file.relativePath.startsWith(prefix) || file.relativePath.length === prefix.length) {
      throw new Error("RPG_PROVISION_RESUME_REVIEW_INVALID");
    }
    return { file, logicalName: file.relativePath.slice(prefix.length).normalize("NFC") };
  });
  entries.sort((left, right) =>
    Buffer.compare(Buffer.from(left.logicalName), Buffer.from(right.logicalName)));
  let previousLogicalName = null;
  for (const { file, logicalName } of entries) {
    if (logicalName === previousLogicalName) {
      throw new Error("RPG_PROVISION_RESUME_REVIEW_INVALID");
    }
    previousLogicalName = logicalName;
    updateLengthPrefixed(hasher, "PROJECT_FILE");
    updateLengthPrefixed(hasher, logicalName);
    hasher.update(fileSHA256(file));
    const size = Buffer.alloc(8);
    size.writeBigUInt64BE(BigInt(file.sizeBytes));
    hasher.update(size);
    hasher.update(Buffer.from([0]));
    totalBytes += file.sizeBytes;
  }
  return { fileCount: files.length, totalBytes, filesDigest: hasher.digest("hex") };
}

function updateLengthPrefixed(hasher, value) {
  const bytes = Buffer.from(value, "utf8");
  const length = Buffer.alloc(4);
  length.writeUInt32BE(bytes.length);
  hasher.update(length);
  hasher.update(bytes);
}

function fileSHA256(file) {
  const hasher = createHash("sha256");
  const descriptor = openSync(file.path, "r");
  const buffer = Buffer.alloc(Math.min(1 << 20, Math.max(file.sizeBytes, 1)));
  try {
    for (let offset = 0; offset < file.sizeBytes;) {
      const length = readSync(descriptor, buffer, 0, Math.min(buffer.length, file.sizeBytes - offset), offset);
      if (length <= 0) { throw new Error("RPG_PROVISION_RESUME_REVIEW_INVALID"); }
      hasher.update(buffer.subarray(0, length));
      offset += length;
    }
  } finally {
    closeSync(descriptor);
  }
  return hasher.digest();
}

async function validateAndPublish(context, client, review) {
  const threadRejections = [];
  if (tracePath) {
    threadRejections.push(await expectThreadRejection(
      client, `/api/v1/admin/reviews/${review.itemId}/runtime-validations`, review.version, "VALIDATION",
    ));
  }
  const createdResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations`, {
      headers: validationHeaders(client, review.version), data: { clientCapabilities: trueCapabilities },
    },
  );
  exact(createdResponse.status(), 201, "RPG_PROVISION_VALIDATION_CREATE_FAILED");
  const created = await createdResponse.json();
  await assertLaunchCookie(context, created.launchId);
  const original = await openPlayer(context, created.playerUrl);
  const checkpointUpload = tracePath ? observeCheckpointUpload(original) : null;
  await runtimeAction(original, "输入已经生效", ["ArrowLeft"]);
  await runtimeAction(original, "已听到游戏音频", []);
  await runtimeAction(original, "记录 B 并创建检查点", config.saveKeys, 300_000);
  const observedUpload = checkpointUpload ? await checkpointUpload() : null;
  const oversizeRejection = tracePath
    ? await rejectDeclaredOversize(context, created.launchId)
    : null;
  await runtimeAction(original, "记录 C 并结束原运行", config.divergeKeys);
  const checkpointed = await waitForValidation(client, review.itemId, created.validationId, "CHECKPOINTED");
  await closeCleanPlayer(original, "RPG_PROVISION_ORIGINAL_PLAYER_ERROR");
  if (tracePath) {
    threadRejections.push(await expectThreadRejection(
      client,
      `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/restore-launch`,
      review.version, "RESTORE",
    ));
  }
  const restoreResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/restore-launch`, {
      headers: validationHeaders(client, review.version), data: { clientCapabilities: trueCapabilities },
    },
  );
  exact(restoreResponse.status(), 201, "RPG_PROVISION_RESTORE_CREATE_FAILED");
  const restored = await restoreResponse.json();
  if (restored.launchId === created.launchId) { throw new Error("RPG_PROVISION_RESTORE_LAUNCH_REUSED"); }
  await assertLaunchCookie(context, restored.launchId);
  const restorePage = await openPlayer(context, restored.playerUrl);
  await runtimeAction(restorePage, "恢复后输入已经生效", config.restoreKeys, 300_000);
  const awaiting = await waitForValidation(client, review.itemId, created.validationId, "AWAITING_DECISION");
  assertPositionSequence(awaiting.checkpointRoundTrip);
  await closeCleanPlayer(restorePage, "RPG_PROVISION_RESTORE_PLAYER_ERROR");
  const decision = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/decision`, {
      headers: validationHeaders(client, review.version),
      data: { decision: "PASS", note: `${caseId} strict generation provision` },
    },
  );
  exact(decision.status(), 200, "RPG_PROVISION_DECISION_FAILED");
  const validation = await decision.json();
  exact(validation.state, "PASSED", "RPG_PROVISION_VALIDATION_NOT_PASSED");
  exact(validation.routeEvidence?.providerId, "retrom-runtime", "RPG_PROVISION_PROVIDER_MISMATCH");
  exact(validation.routeEvidence?.targetId, config.targetId, "RPG_PROVISION_TARGET_MISMATCH");
  const currentReview = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const approvalResponse = await client.raw("POST", `/api/v1/admin/reviews/${review.itemId}/approve`, {
    headers: validationHeaders(client, currentReview.version), data: {},
  });
  exact(approvalResponse.status(), 201, "RPG_PROVISION_APPROVAL_FAILED");
  const approval = await approvalResponse.json();
  if (!approval.gameId) { throw new Error("RPG_PROVISION_GAME_ID_MISSING"); }
  return {
    gameId: approval.gameId, validation,
    checkpointUpload: tracePath ? bindCheckpointUpload(observedUpload, checkpointed.checkpointRoundTrip) : null,
    oversizeRejection, threadRejections,
  };
}

async function expectThreadRejection(client, path, reviewVersion, phase) {
  const attemptId = randomUUID();
  const response = await client.raw("POST", path, {
    headers: { ...validationHeaders(client, reviewVersion), "Idempotency-Key": attemptId },
    data: { clientCapabilities: falseCapabilities },
  });
  const body = await response.json();
  if (response.status() !== 409 || body.error?.code !== "RPG_RUNTIME_ROUTE_UNAVAILABLE"
      || body.launchId || body.playerUrl) {
    throw new Error(`RPG_PROVISION_THREAD_REJECTION_${phase}_INVALID`);
  }
  return {
    attemptId, phase, capabilities: falseCapabilities,
    responseStatus: response.status(), errorCode: body.error.code,
    launchCredentialIssued: false, projectPayloadRequestCount: 0,
  };
}

function observeCheckpointUpload(page) {
  const observation = { request: null, response: null };
  page.on("request", (request) => {
    if (request.method() !== "POST" || !request.url().includes("/save-states")) { return; }
    observation.request = { startedAtMs: Date.now(), headers: request.allHeaders() };
  });
  page.on("response", (response) => {
    if (response.request().method() !== "POST" || !response.url().includes("/save-states")) { return; }
    observation.response = { responseStatus: response.status(), finishedAtMs: Date.now() };
  });
  return async () => {
    for (let attempt = 0; attempt < 600 && (!observation.request || !observation.response); attempt += 1) {
      await page.waitForTimeout(100);
    }
    if (!observation.request || !observation.response) {
      throw new Error("RPG_PROVISION_CHECKPOINT_UPLOAD_TRACE_MISSING");
    }
    const headers = await observation.request.headers;
    const contentLength = Number(headers["content-length"]);
    if (!Number.isSafeInteger(contentLength)) {
      throw new Error("RPG_PROVISION_CHECKPOINT_CONTENT_LENGTH_MISSING");
    }
    return {
      requestContentLengthBytes: contentLength,
      startedAtMs: observation.request.startedAtMs,
      ...observation.response,
    };
  };
}

function bindCheckpointUpload(observed, checkpoint) {
  if (caseId !== "ACC-RPG-004") { return null; }
  if (!observed || observed.responseStatus !== 201 || !Number.isSafeInteger(checkpoint?.sizeBytes)
      || checkpoint.sizeBytes <= 24 || checkpoint.sizeBytes >= 268_435_456
      || !/^[0-9a-f]{64}$/.test(checkpoint.sha256 ?? "")) {
    throw new Error("RPG_PROVISION_CHECKPOINT_UPLOAD_INVALID");
  }
  return {
    requestPayloadBytes: checkpoint.sizeBytes,
    requestContentLengthBytes: observed.requestContentLengthBytes,
    responseStatus: observed.responseStatus, sha256: checkpoint.sha256,
    startedAtMs: observed.startedAtMs, finishedAtMs: observed.finishedAtMs,
  };
}

async function rejectDeclaredOversize(context, launchId) {
  const declaredContentLengthBytes = 283_115_521;
  const endpoint = new URL(`/runtime/launches/${launchId}/save-states`, baseUrl);
  const cookies = await context.cookies(endpoint.href);
  const cookie = cookies.map((item) => `${item.name}=${item.value}`).join("; ");
  if (!cookie) { throw new Error("RPG_PROVISION_OVERSIZE_COOKIE_MISSING"); }
  const startedAtMs = Date.now();
  const result = await declaredLengthRequest(endpoint, cookie, declaredContentLengthBytes);
  const finishedAtMs = Date.now();
  if (result.status !== 413 || result.body.error?.code !== "REQUEST_TOO_LARGE") {
    throw new Error(`RPG_PROVISION_OVERSIZE_REJECTION_INVALID_${result.status}`);
  }
  return {
    declaredContentLengthBytes: 283_115_521,
    responseStatus: result.status, errorCode: result.body.error.code,
    startedAtMs, finishedAtMs,
  };
}

function declaredLengthRequest(endpoint, cookie, contentLength) {
  const transport = endpoint.protocol === "https:" ? httpsRequest : httpRequest;
  let responseStarted = false;
  let request;
  const responseResult = new Promise((resolvePromise, rejectPromise) => {
    request = transport(endpoint, {
      method: "POST", headers: {
        Cookie: cookie, Origin: baseUrl, "Idempotency-Key": randomUUID(),
        "Content-Type": "application/octet-stream",
        "Content-Length": String(contentLength), Connection: "close",
      },
    }, (response) => {
      responseStarted = true;
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => {
        try {
          resolvePromise({ status: response.statusCode, body: JSON.parse(Buffer.concat(chunks).toString("utf8")) });
        } catch (error) { rejectPromise(error); }
      });
    });
    request.setTimeout(300_000, () => request.destroy(new Error("RPG_PROVISION_OVERSIZE_TIMEOUT")));
    request.on("error", (error) => { if (!responseStarted) { rejectPromise(error); } });
  });
  void streamDeclaredBody(request, contentLength, () => responseStarted)
    .catch((error) => { if (!responseStarted) { request.destroy(error); } });
  return responseResult;
}

async function streamDeclaredBody(request, contentLength, responseHasStarted) {
  const buffer = Buffer.alloc(1 << 20);
  let remaining = contentLength;
  while (remaining > 0 && !responseHasStarted()) {
    const chunk = remaining >= buffer.length ? buffer : buffer.subarray(0, remaining);
    await writeChunk(request, chunk);
    remaining -= chunk.length;
  }
  if (!responseHasStarted()) { request.end(); }
}

function writeChunk(request, chunk) {
  if (request.write(chunk)) { return Promise.resolve(); }
  return new Promise((resolvePromise, rejectPromise) => {
    const drained = () => { cleanup(); resolvePromise(); };
    const failed = (error) => { cleanup(); rejectPromise(error); };
    const cleanup = () => {
      request.off("drain", drained);
      request.off("error", failed);
    };
    request.once("drain", drained);
    request.once("error", failed);
  });
}

function xpTrace(value) {
  return {
    schemaVersion: 1, checkpointUpload: value.checkpointUpload,
    oversizeRejection: value.oversizeRejection,
    threadCapabilityRejections: value.threadRejections,
  };
}

async function platformInstance(client) {
  let response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  let found = (response.items ?? []).find((item) => item.enabled && item.defaultCoreId === "rpgmaker");
  if (found) { return found.id; }
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  found = (response.items ?? []).find((item) => item.enabled && item.defaultCoreId === "rpgmaker");
  if (!found) { throw new Error("RPG_PROVISION_PLATFORM_INSTANCE_MISSING"); }
  return found.id;
}

async function assertSelectedRoute(client) {
  const response = await client.json("GET", "/api/v1/admin/runtime-targets");
  const selected = (response.items ?? []).filter((item) =>
    item.coreId === config.coreId && item.providerId === "retrom-runtime" && item.targetId === config.targetId,
  );
  if (selected.length !== 1 || !/^[0-9a-f]{64}$/.test(selected[0].bundleSha256)) {
    throw new Error("RPG_PROVISION_SELECTED_TARGET_MISMATCH");
  }
}

async function openPlayer(context, playerUrl) {
  const page = await context.newPage();
  const consoleDiagnostics = [];
  const errors = [];
  const exceptionDiagnostics = [];
  const exceptionTasks = [];
  const networkRequests = [];
  const projectRequests = [];
  const runtimeDiagnostics = [];
  let resolveFatalError;
  const fatalError = new Promise((resolvePromise) => { resolveFatalError = resolvePromise; });
  const cdp = await context.newCDPSession(page);
  await cdp.send("Runtime.enable");
  cdp.on("Runtime.exceptionThrown", ({ exceptionDetails }) => {
    const task = collectRuntimeException(cdp, exceptionDetails).then((diagnostic) => {
      exceptionDiagnostics.push(diagnostic);
      if (exceptionDiagnostics.length > 100) {
        exceptionDiagnostics.splice(0, exceptionDiagnostics.length - 100);
      }
    });
    exceptionTasks.push(task);
  });
  page.on("console", (message) => {
    const text = message.text();
    if (!text.trim()) { return; }
    consoleDiagnostics.push({ type: message.type(), message: trimDiagnostic(text) });
    if (consoleDiagnostics.length > 100) { consoleDiagnostics.splice(0, consoleDiagnostics.length - 100); }
  });
  page.on("pageerror", (error) => {
    errors.push(error.stack || error.message);
    resolveFatalError(error);
  });
  page.on("request", (request) => {
    if (!request.url().includes("/runtime/content/project/")) { return; }
    projectRequests.push({
      method: request.method(), range: request.headers().range ?? null,
      responseStatus: null, contentRange: null, failure: null,
    });
  });
  page.on("response", (response) => {
    networkRequests.push({
      method: response.request().method(), url: safeStackUrl(response.url()), status: response.status(),
    });
    if (networkRequests.length > 500) { networkRequests.splice(0, networkRequests.length - 500); }
    if (!response.url().includes("/runtime/content/project/")) { return; }
    const match = [...projectRequests].reverse().find((item) =>
      item.method === response.request().method() && item.responseStatus === null && item.failure === null);
    if (!match) { return; }
    match.responseStatus = response.status();
    match.contentRange = response.headers()["content-range"] ?? null;
  });
  page.on("requestfailed", (request) => {
    networkRequests.push({
      method: request.method(), url: safeStackUrl(request.url()),
      failure: trimDiagnostic(request.failure()?.errorText ?? "unknown"),
    });
    if (networkRequests.length > 500) { networkRequests.splice(0, networkRequests.length - 500); }
    if (!request.url().includes("/runtime/content/project/")) { return; }
    const match = [...projectRequests].reverse().find((item) =>
      item.method === request.method() && item.responseStatus === null && item.failure === null);
    if (match) { match.failure = trimDiagnostic(request.failure()?.errorText ?? "unknown"); }
  });
  page.__retromPageErrors = errors;
  page.__retromFatalError = fatalError;
  page.__retromExceptionTasks = exceptionTasks;
  page.__retromExceptionDiagnostics = exceptionDiagnostics;
  page.__retromConsoleDiagnostics = consoleDiagnostics;
  page.__retromNetworkRequests = networkRequests;
  page.__retromProjectRequests = projectRequests;
  page.__retromRuntimeDiagnostics = runtimeDiagnostics;
  await page.exposeBinding("__retromCaptureRuntimeDiagnostic", (_source, detail) => {
    if (!detail || typeof detail.code !== "string" || typeof detail.message !== "string") { return; }
    runtimeDiagnostics.push({
      code: detail.code.slice(0, 128), message: detail.message.slice(0, 1_000),
    });
    if (runtimeDiagnostics.length > 100) { runtimeDiagnostics.splice(0, runtimeDiagnostics.length - 100); }
  });
  await page.addInitScript(() => {
    window.__retromRuntimeDiagnostics = [];
    window.addEventListener("retrom:runtime-diagnostic", (event) => {
      const detail = event instanceof CustomEvent ? event.detail : null;
      if (!detail || typeof detail.code !== "string" || typeof detail.message !== "string") { return; }
      window.__retromRuntimeDiagnostics.push({
        code: detail.code.slice(0, 128), message: detail.message.slice(0, 1_000),
      });
      void window.__retromCaptureRuntimeDiagnostic?.(detail);
      if (window.__retromRuntimeDiagnostics.length > 100) {
        window.__retromRuntimeDiagnostics.splice(0, window.__retromRuntimeDiagnostics.length - 100);
      }
    });
  });
  const configResponse = page.waitForResponse((response) =>
    response.request().method() === "GET" && /\/runtime\/launches\/[^/]+\/config$/.test(response.url()),
  );
  await page.goto(`${baseUrl}${playerUrl}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
  const config = await configResponse;
  if (config.status() !== 200) {
    throw new Error(`RPG_PROVISION_LAUNCH_CONFIG_${config.status()}`);
  }
  return page;
}

async function assertLaunchCookie(context, launchId) {
  const expectedPath = `/runtime/launches/${launchId}/`;
  const cookies = await context.cookies(`${baseUrl}${expectedPath}config`);
  const matches = cookies.filter((cookie) =>
    cookie.name === `retrom_launch_${launchId}` && cookie.path === expectedPath
      && cookie.httpOnly && cookie.sameSite === "Strict",
  );
  if (matches.length !== 1) { throw new Error("RPG_PROVISION_LAUNCH_COOKIE_MISSING"); }
}

async function runtimeAction(page, label, keys, timeout = 120_000) {
  const button = page.getByRole("button", { name: label, exact: true });
  const fatalError = page.__retromFatalError;
  const runtimeFailure = page.getByRole("alert")
    .filter({ hasText: /\b(?:RPG|RUNTIME)_[A-Z0-9_]+\b/u }).first();
  try {
    await Promise.race([
      button.waitFor({ state: "visible", timeout }),
      runtimeFailure.waitFor({ state: "visible", timeout }).then(() => {
        throw new Error("RPG_PROVISION_RUNTIME_FAILED");
      }),
      fatalError.then(() => { throw new Error("RPG_PROVISION_PAGE_ERROR"); }),
    ]);
  } catch {
    await Promise.allSettled(page.__retromExceptionTasks ?? []);
    const runtimeDiagnostics = (page.__retromRuntimeDiagnostics ?? []).slice(-20);
    const diagnostics = {
      alerts: (await page.getByRole("alert").allInnerTexts()).map(trimDiagnostic).slice(0, 5),
      consoleDiagnostics: (page.__retromConsoleDiagnostics ?? []).slice(-30),
      exceptionDiagnostics: (page.__retromExceptionDiagnostics ?? []).slice(-20),
      loading: (await page.locator(".player-loading").allInnerTexts()).map(trimDiagnostic).slice(0, 3),
      networkRequests: (page.__retromNetworkRequests ?? []).slice(-100),
      pageErrors: (page.__retromPageErrors ?? []).map(trimDiagnostic).slice(0, 5),
      projectRequests: (page.__retromProjectRequests ?? []).slice(-30),
      runtimeDiagnostics: runtimeDiagnostics.map((value) => ({
        code: trimDiagnostic(value.code).slice(0, 128), message: trimDiagnostic(value.message),
      })),
      statuses: (await page.getByRole("status").allInnerTexts()).map(trimDiagnostic).slice(0, 10),
    };
    throw new Error(`RPG_PROVISION_RUNTIME_ACTION_UNAVAILABLE_${label}:${JSON.stringify(diagnostics)}`);
  }
  const canvas = await focusRuntimeCanvas(page);
  for (const key of keys) {
    await canvas.press(key, { delay: 250 });
    await page.waitForTimeout(800);
  }
  await button.click();
  await page.waitForTimeout(500);
  const alerts = (await page.getByRole("alert").allInnerTexts()).map((value) => value.trim()).filter(Boolean);
  if (alerts.length) { throw new Error(`RPG_PROVISION_RUNTIME_ACTION_${label}:${alerts[0]}`); }
}

function trimDiagnostic(value) {
  return String(value).trim().slice(0, 600);
}

async function collectRuntimeException(cdp, details) {
  const objectId = details.exception?.objectId;
  let ownProperties = [];
  if (objectId) {
    const result = await cdp.send("Runtime.getProperties", {
      objectId, ownProperties: true, accessorPropertiesOnly: false, generatePreview: true,
    }).catch(() => ({ result: [] }));
    ownProperties = result.result ?? [];
  }
  return safeRuntimeException(details, ownProperties);
}

function safeRuntimeException(details, ownProperties = []) {
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
    text: trimDiagnostic(details.text).slice(0, 240),
    description: trimDiagnostic(details.exception?.description),
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
  throw new Error("RPG_PROVISION_RUNTIME_CANVAS_MISSING");
}

async function closeCleanPlayer(page, code) {
  const errors = page.__retromPageErrors ?? [];
  await page.close();
  if (errors.length) { throw new Error(`${code}:${String(errors[0]).slice(0, 600)}`); }
}

async function waitForValidation(client, itemId, validationId, expectedState) {
  for (let attempt = 0; attempt < 1_200; attempt += 1) {
    const value = await client.json("GET", `/api/v1/admin/reviews/${itemId}/runtime-validations/${validationId}`);
    if (value.state === expectedState) { return value; }
    if (["FAILED", "EXPIRED", "PASSED"].includes(value.state)) {
      throw new Error(`RPG_PROVISION_VALIDATION_${value.state}_${value.failureCode ?? "UNKNOWN"}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 250));
  }
  throw new Error(`RPG_PROVISION_VALIDATION_${expectedState}_TIMEOUT`);
}

function assertPositionSequence(roundTrip) {
  const positions = [
    roundTrip?.initialPosition, roundTrip?.savedPosition, roundTrip?.divergedPosition,
    roundTrip?.restoredPosition, roundTrip?.restoreInputPosition,
  ];
  const states = positions.map((value) => value?.fixtureState);
  if (JSON.stringify(states) !== JSON.stringify([0, 1, 2, 1, 2])
      || samePosition(positions[0], positions[1]) || samePosition(positions[1], positions[2])
      || !samePosition(positions[1], positions[3]) || samePosition(positions[3], positions[4])) {
    throw new Error(`RPG_PROVISION_POSITION_SEQUENCE_INVALID:${states.join(",")}`);
  }
}

function samePosition(left, right) {
  return left?.mapId === right?.mapId && left?.playerX === right?.playerX
    && left?.playerY === right?.playerY && left?.fixtureState === right?.fixtureState;
}

function validationHeaders(client, version) {
  return { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${version}"` };
}

function checkedTracePath(value) {
  const path = resolve(value);
  const parent = dirname(path);
  if (!isAbsolute(value) || parent === path || existsSync(path)
      || !existsSync(parent) || !lstatSync(parent).isDirectory() || lstatSync(parent).isSymbolicLink()) {
    throw new Error("RPG_PROVISION_TRACE_PATH_INVALID");
  }
  return path;
}

function optionalTracePath(value) {
  return value ? checkedTracePath(value) : null;
}

function validateMZProvenance(files, pathValue) {
  if (!isAbsolute(pathValue) || !existsSync(pathValue)) {
    throw new Error("RPG_PROVISION_MZ_PROVENANCE_INVALID");
  }
  const metadata = lstatSync(pathValue);
  if (!metadata.isFile() || metadata.isSymbolicLink() || metadata.size > 64 * 1024) {
    throw new Error("RPG_PROVISION_MZ_PROVENANCE_INVALID");
  }
  const value = JSON.parse(readFileSync(pathValue, "utf8"));
  const transformation = value?.transformation;
  const expected = projectManifest(files, config.prefix);
  if (!hasExactKeys(value, [
    "schemaVersion", "kind", "licenseBasis", "licenseUrl", "sourceUrl", "sourceVersion",
    "sourceSha256", "marker", "markerRgb", "transformation",
  ]) || value.schemaVersion !== 1 || value.kind !== "LICENSED_EXTERNAL_WEB_DEPLOYMENT"
      || !hasExactKeys(transformation, [
        "schemaVersion", "recipe", "tool", "sourceSizeBytes", "removedEntries", "injectedFiles",
        "outputProjectFingerprint", "outputFileCount", "outputTotalBytes",
      ]) || transformation.outputProjectFingerprint !== expected.filesDigest
      || transformation.outputFileCount !== expected.fileCount
      || transformation.outputTotalBytes !== expected.totalBytes) {
    throw new Error("RPG_PROVISION_MZ_PROVENANCE_INVALID");
  }
}

function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value)
    && JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...keys].sort());
}

function normalizedBase(value) {
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("RPG_PROVISION_BASE_URL_INVALID");
  }
  if (parsed.protocol !== "https:" && !isLocalAcceptanceHostname(parsed.hostname)) {
    throw new Error("RPG_PROVISION_BASE_URL_REQUIRES_HTTPS");
  }
  return parsed.origin;
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_PROVISION_ENV_MISSING_${name}`); }
  return value;
}

function exact(actual, expected, code) {
  if (actual !== expected) { throw new Error(`${code}:${actual}`); }
}
