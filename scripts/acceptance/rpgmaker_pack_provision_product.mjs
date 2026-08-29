import { directoryFiles, reviewForImport, singleFile } from "./rpgmaker_security_upload.mjs";
import { basename } from "node:path";

const gates = [
  "RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO",
  "INITIAL_POSITION_RECORDED", "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED",
  "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED", "RESTORE_STARTED",
  "RESTORE_POSITION_VERIFIED", "RESTORE_SCREENSHOT", "RESTORE_INPUT",
];
const capabilities = { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true };

export async function assertFreshInstance(client) {
  const [catalog, games, saves, reviews, imports] = await Promise.all([
    client.json("GET", "/api/v1/admin/runtime-asset-packs"),
    client.json("GET", "/api/v1/admin/games?limit=1"),
    client.json("GET", "/api/v1/saves?limit=1"),
    client.json("GET", "/api/v1/admin/reviews?limit=1"),
    client.json("GET", "/api/v1/admin/imports/summary"),
  ]);
  const importCounts = [
    "completed", "failed", "issueItems", "processingItems", "publishedItems", "reviewPending", "running",
  ];
  if (catalog.installations?.length || games.items?.length || saves.items?.length || reviews.items?.length
      || importCounts.some((key) => imports[key] !== 0)) {
    throw new Error("RPG_009_PROVISION_DATABASE_NOT_FRESH");
  }
}

export async function rpgPlatformInstances(client, expectedCoreIds) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  const platforms = (response.items ?? []).filter(
    (item) => item.enabled && item.defaultCoreId === "rpgmaker",
  );
  if (platforms.length !== 1) { throw new Error("RPG_009_PROVISION_PLATFORM_MISSING"); }
  const platform = platforms[0];
  const artifacts = await allCoreArtifacts(client);
  for (const coreId of expectedCoreIds) {
    const current = artifacts.filter((item) =>
      item.coreId === coreId && item.selectedForNewBindings && item.availableForLaunch);
    if (current.length !== 1) { throw new Error(`RPG_009_PROVISION_RUNTIME_UNAVAILABLE_${coreId}`); }
  }
  return new Map(expectedCoreIds.map((coreId) => [coreId, platform.id]));
}

async function allCoreArtifacts(client) {
  const result = [];
  let cursor = "";
  do {
    const suffix = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    const page = await client.json("GET", `/api/v1/admin/core-artifacts${suffix}`);
    result.push(...(page.items ?? []));
    cursor = page.nextCursor ?? "";
  } while (cursor);
  return result;
}

export async function installRuntimePack(client, input, expectedDefinitionId) {
  const files = input.sourceType === "DIRECTORY" ? directoryFiles(input.sourcePath) : singleFile(input.sourcePath);
  const uploadId = await client.upload(files, input.sourceType, "RUNTIME_ASSET_PACK");
  const body = { uploadId, kind: input.kind };
  if (input.generation !== null) { body.generation = input.generation; }
  if (input.declaredName !== null) { body.declaredName = input.declaredName; }
  if (input.sourceNote !== null) { body.sourceNote = input.sourceNote; }
  const accepted = await client.json("POST", "/api/v1/admin/runtime-asset-packs/installations", {
    headers: client.writeHeaders(), data: body, expected: 202,
  });
  await waitForJob(client, accepted.jobId, "RUNTIME_ASSET_PACK_VALIDATE");
  const catalog = await client.json("GET", "/api/v1/admin/runtime-asset-packs");
  const installation = (catalog.installations ?? []).find((item) => item.installationId === accepted.installationId);
  if (!installation || installation.status !== "READY" || installation.definitionId !== expectedDefinitionId) {
    throw new Error("RPG_009_PROVISION_PACK_NOT_READY");
  }
  return installation;
}

export async function importReview(client, sourcePath, platformInstanceId) {
  const sourceName = basename(sourcePath);
  const imported = await client.importProject(
    directoryFiles(sourcePath, `${sourceName}/`), "DIRECTORY", platformInstanceId,
  );
  if (imported.status !== 202 || !imported.body.importJobId) {
    throw new Error(`RPG_009_PROVISION_IMPORT_${imported.status}_${imported.body.error?.code ?? "UNKNOWN"}`);
  }
  for (let attempt = 0; attempt < 600; attempt += 1) {
    try {
      const review = await reviewForImport(client, imported.body.importJobId);
      if (review.metadata?.title !== sourceName) {
        throw new Error("RPG_009_PROVISION_REVIEW_TITLE_INVALID");
      }
      return review;
    }
    catch (error) {
      if (!String(error.message).includes("REVIEW_CARDINALITY")) { throw error; }
    }
    await delay(250);
  }
  throw new Error("RPG_009_PROVISION_REVIEW_TIMEOUT");
}

export async function selectRuntimePack(client, review, installationId) {
  const requirements = review.rpgMaker?.runtimePackRequirements ?? [];
  if (requirements.length !== 1 || review.rpgMaker.runtimePackSelections.length !== 0) {
    throw new Error("RPG_009_PROVISION_PROTECTED_REQUIREMENT_INVALID");
  }
  const patched = await client.json("PATCH", `/api/v1/admin/reviews/${review.itemId}`, {
    headers: validationHeaders(client, review.version), expected: 200,
    data: {
      tagIds: (review.tags ?? []).map((tag) => tag.tagId),
      runtimePackSelections: [{ slot: requirements[0].slot, installationId }],
      rpgSelfContainedOverride: false,
    },
  });
  if (patched.itemId !== review.itemId || patched.version !== review.version + 1) {
    throw new Error("RPG_009_PROVISION_PROTECTED_SELECTION_FAILED");
  }
  const current = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const selected = current.rpgMaker?.runtimePackSelections?.find((item) => item.slot === requirements[0].slot);
  if (selected?.installationId !== installationId || current.version !== patched.version) {
    throw new Error("RPG_009_PROVISION_PROTECTED_SELECTION_FAILED");
  }
  return current;
}

export async function validateReview(context, client, baseUrl, review, generation) {
  const createdResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations`,
    { headers: validationHeaders(client, review.version), data: { clientCapabilities: capabilities } },
  );
  exact(createdResponse.status(), 201, "RPG_009_PROVISION_VALIDATION_CREATE_FAILED");
  const created = await createdResponse.json();
  const sequence = validationSequence(generation);
  const original = await openPlayer(context, baseUrl, created.playerUrl);
  await runtimeAction(original, "输入已经生效", sequence.input);
  await runtimeAction(original, "已听到游戏音频", []);
  await runtimeAction(original, "记录 B 并创建检查点", sequence.save);
  await runtimeAction(original, "记录 C 并结束原运行", sequence.diverge);
  await waitForValidation(client, review.itemId, created.validationId, "CHECKPOINTED");
  await closeCleanPlayer(original, "RPG_009_PROVISION_ORIGINAL_PLAYER_ERROR");
  const restoreResponse = await client.raw(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/restore-launch`,
    { headers: validationHeaders(client, review.version), data: { clientCapabilities: capabilities } },
  );
  exact(restoreResponse.status(), 201, "RPG_009_PROVISION_RESTORE_CREATE_FAILED");
  const restored = await restoreResponse.json();
  if (restored.launchId === created.launchId) { throw new Error("RPG_009_PROVISION_RESTORE_LAUNCH_REUSED"); }
  const restorePage = await openPlayer(context, baseUrl, restored.playerUrl);
  await runtimeAction(restorePage, "恢复后输入已经生效", sequence.restore);
  await waitForValidation(client, review.itemId, created.validationId, "AWAITING_DECISION");
  await closeCleanPlayer(restorePage, "RPG_009_PROVISION_RESTORE_PLAYER_ERROR");
  const decision = await client.json(
    "POST", `/api/v1/admin/reviews/${review.itemId}/runtime-validations/${created.validationId}/decision`,
    {
      headers: validationHeaders(client, review.version), expected: 200,
      data: { decision: "PASS", note: "ACC-RPG-009 deterministic provisioning" },
    },
  );
  assertPassedValidation(decision, review.itemId, generation);
  return decision;
}

export async function approveReview(client, itemId) {
  const review = await client.json("GET", `/api/v1/admin/reviews/${itemId}`);
  if (!review.rpgMaker?.runtimeValidationCurrent || review.rpgMaker.runtimeValidation?.state !== "PASSED") {
    throw new Error("RPG_009_PROVISION_APPROVAL_VALIDATION_INVALID");
  }
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: validationHeaders(client, review.version), data: {},
  });
  const body = await response.json();
  if (response.status() !== 201 || !body.gameId) {
    throw new Error(`RPG_009_PROVISION_APPROVAL_${response.status()}_${body.error?.code ?? "UNKNOWN"}`);
  }
  const game = await client.json("GET", `/api/v1/admin/games/${body.gameId}`);
  if (game.status !== "PUBLISHED") { throw new Error("RPG_009_PROVISION_GAME_NOT_PUBLISHED"); }
  return body.gameId;
}

export async function createProductSave(context, client, baseUrl, gameId, coreId) {
  const launch = await productLaunch(client, gameId, coreId, null);
  const page = await openPlayer(context, baseUrl, launch.playUrl);
  await page.getByRole("status").filter({ hasText: "可创建存档" }).waitFor({ state: "attached", timeout: 120_000 });
  const canvas = await focusRuntimeCanvas(page);
  await canvas.press("ArrowRight", { delay: 250 });
  await page.waitForTimeout(800);
  const saveButton = await revealProductSaveAction(page);
  const saveResponse = page.waitForResponse((response) =>
    response.request().method() === "POST"
      && response.url().includes(`/runtime/launches/${launch.launchId}/save-states`),
  { timeout: 120_000 });
  await saveButton.click();
  const response = await saveResponse;
  exact(response.status(), 201, "RPG_009_PROVISION_PRODUCT_SAVE_FAILED");
  // Chromium can evict a response body from the inspector cache after this
  // request uploads the exact 256 MiB mkxp checkpoint. The API projection is
  // the authoritative post-commit receipt and does not depend on CDP storage.
  const saves = await client.json("GET", `/api/v1/saves?gameId=${encodeURIComponent(gameId)}&limit=100`);
  const receipts = (saves.items ?? []).filter((item) =>
    item.availability?.status === "AVAILABLE" && item.screenshotUrl);
  if (receipts.length !== 1 || !receipts[0].saveStateId) {
    throw new Error("RPG_009_PROVISION_SAVE_NOT_AVAILABLE");
  }
  const receipt = receipts[0];
  await closeCleanPlayer(page, "RPG_009_PROVISION_PRODUCT_PLAYER_ERROR");
  const restore = await productLaunch(client, gameId, coreId, receipt.saveStateId);
  if (restore.launchId === launch.launchId) { throw new Error("RPG_009_PROVISION_PRODUCT_RESTORE_REUSED"); }
  const restoredPage = await openPlayer(context, baseUrl, restore.playUrl);
  await restoredPage.getByRole("status").filter({ hasText: "可创建存档" })
    .waitFor({ state: "attached", timeout: 120_000 });
  await focusRuntimeCanvas(restoredPage);
  await closeCleanPlayer(restoredPage, "RPG_009_PROVISION_PRODUCT_RESTORE_ERROR");
  return receipt.saveStateId;
}

export async function assertProvisionedState(client, protectedReferences, reviewIds) {
  const [catalog, games, saves, queue] = await Promise.all([
    client.json("GET", "/api/v1/admin/runtime-asset-packs"),
    client.json("GET", "/api/v1/admin/games?limit=100"),
    client.json("GET", "/api/v1/saves?limit=100"),
    client.json("GET", "/api/v1/admin/reviews?limit=20"),
  ]);
  assertFinalCardinality(catalog, games, saves, queue, reviewIds);
  for (const reference of Object.values(protectedReferences)) { assertFinalReference(catalog, reference); }
}

function assertFinalCardinality(catalog, games, saves, queue, reviewIds) {
  if (catalog.installations?.length !== 2 || games.items?.length !== 2 || saves.items?.length !== 1
      || queue.items?.length !== 13 || new Set(queue.items.map((item) => item.itemId)).size !== 13
      || Object.values(reviewIds).some((itemId) => !queue.items.some((item) => item.itemId === itemId))) {
    throw new Error("RPG_009_PROVISION_FINAL_CARDINALITY_INVALID");
  }
}

function assertFinalReference(catalog, reference) {
  const row = catalog.installations.find((item) => item.installationId === reference.installationId);
  if (!row || row.status !== "READY" || row.references?.variantRevisionCount < 1) {
    throw new Error("RPG_009_PROVISION_PROTECTED_REFERENCE_INVALID");
  }
  if (reference.saveStateId && row.references.checkpointCount < 1) {
    throw new Error("RPG_009_PROVISION_CHECKPOINT_REFERENCE_INVALID");
  }
}

function validationSequence(generation) {
  return ["RPG2000", "RPG2003"].includes(generation)
    ? { input: ["ArrowLeft"], save: ["ArrowRight", "ArrowRight"], diverge: ["ArrowRight", "ArrowRight"],
      restore: ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"] }
    : { input: ["ArrowLeft"], save: ["ArrowRight", "Enter"], diverge: ["ArrowRight", "Enter"],
      restore: ["ArrowRight"] };
}

function assertPassedValidation(validation, itemId, generation) {
  if (validation.importItemId !== itemId || validation.state !== "PASSED"
      || validation.decision?.decision !== "PASS" || validation.routeEvidence?.generation !== generation
      || validation.machineGates?.map((gate) => gate.gate).join("|") !== gates.join("|")
      || validation.machineGates.some((gate) => gate.status !== "PASSED")) {
    throw new Error("RPG_009_PROVISION_VALIDATION_INCOMPLETE");
  }
  assertRoundTrip(validation.checkpointRoundTrip);
}

function assertRoundTrip(roundTrip) {
  if (!roundTrip?.created || !roundTrip.originalLaunchEnded || !roundTrip.restoreStarted
      || !roundTrip.positionVerified || !roundTrip.restoreInputVerified
      || same(roundTrip.initialPosition, roundTrip.savedPosition)
      || same(roundTrip.savedPosition, roundTrip.divergedPosition)
      || !same(roundTrip.savedPosition, roundTrip.restoredPosition)
      || same(roundTrip.restoredPosition, roundTrip.restoreInputPosition)) {
    throw new Error("RPG_009_PROVISION_RESTORE_EVIDENCE_INVALID");
  }
}

async function productLaunch(client, gameId, coreId, saveStateId) {
  return client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: { gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`, clientCapabilities: capabilities },
  });
}

async function openPlayer(context, baseUrl, playerUrl) {
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
  for (const key of keys) { await canvas.press(key, { delay: 250 }); await page.waitForTimeout(800); }
  await button.click();
  await page.waitForTimeout(500);
  const alert = (await page.getByRole("alert").allInnerTexts()).map((value) => value.trim()).find(Boolean);
  if (alert) { throw new Error(`RPG_009_PROVISION_RUNTIME_ACTION_${label}_${alert}`); }
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
    await delay(100);
  }
  throw new Error("RPG_009_PROVISION_RUNTIME_CANVAS_MISSING");
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
      throw new Error(`RPG_009_PROVISION_VALIDATION_${validation.state}_${validation.failureCode ?? "UNKNOWN"}`);
    }
    await delay(250);
  }
  throw new Error(`RPG_009_PROVISION_VALIDATION_${expectedState}_TIMEOUT`);
}

async function waitForJob(client, jobId, expectedKind) {
  for (let attempt = 0; attempt < 600; attempt += 1) {
    const job = await client.json("GET", `/api/v1/admin/jobs/${jobId}`);
    if (job.state === "SUCCEEDED") {
      if (job.kind !== expectedKind) { throw new Error("RPG_009_PROVISION_JOB_KIND_INVALID"); }
      return;
    }
    if (["FAILED", "CANCELLED"].includes(job.state)) {
      throw new Error(`RPG_009_PROVISION_JOB_${job.errorCode ?? job.state}`);
    }
    await delay(250);
  }
  throw new Error("RPG_009_PROVISION_JOB_TIMEOUT");
}

async function revealProductSaveAction(page) {
  await page.mouse.move(720, 1);
  const saveButton = page.getByRole("button", { name: "创建存档", exact: true });
  await saveButton.waitFor({ state: "visible", timeout: 30_000 });
  if (!await saveButton.isEnabled()) { throw new Error("RPG_009_PROVISION_SAVE_UNAVAILABLE"); }
  return saveButton;
}

function validationHeaders(client, version) {
  return { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${version}"` };
}

function same(left, right) { return JSON.stringify(left) === JSON.stringify(right); }
function exact(actual, expected, code) { if (actual !== expected) { throw new Error(`${code}:${actual}`); } }
function delay(milliseconds) { return new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)); }
