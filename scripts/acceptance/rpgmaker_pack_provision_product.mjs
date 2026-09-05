import { directoryFiles, reviewForImport, singleFile } from "./rpgmaker_security_upload.mjs";
import { basename } from "node:path";
import {preservedPopulation, readPopulation} from "./rpgmaker_pack_population.mjs";
import {
  advanceFixture, captureOptionalReviewScreenshot, capturePreviewCheckpoint, finishPreview,
  observeFixturePosition, observeOwnedFixture, observePreviewFrames, revealPreviewToolbar, waitForPreviewReady,
} from "./rpgmaker_preview_actions.mjs";

const capabilities = { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true };

export async function capturePackBaseline(client) {
  const catalog = await client.json("GET", "/api/v1/admin/runtime-asset-packs");
  if (!Array.isArray(catalog.installations) || catalog.installations.length) {
    throw new Error("RPG_009_PROVISION_PACK_CATALOG_NOT_EMPTY");
  }
  return readPopulation(client);
}

export async function rpgPlatformInstances(client, expectedTargetIds) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const response = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  const platforms = (response.items ?? []).filter(
    (item) => item.enabled && item.defaultCoreId === "rpgmaker",
  );
  if (platforms.length !== 1) { throw new Error("RPG_009_PROVISION_PLATFORM_MISSING"); }
  const platform = platforms[0];
  const artifacts = (await client.json("GET", "/api/v1/admin/runtime-targets")).items ?? [];
  for (const targetId of expectedTargetIds) {
    const current = artifacts.filter((item) =>
      item.coreId === "rpgmaker" && item.targetId === targetId && item.providerId === "retrom-runtime" && /^[0-9a-f]{64}$/.test(item.bundleSha256));
    if (current.length !== 1) { throw new Error(`RPG_009_PROVISION_RUNTIME_UNAVAILABLE_${targetId}`); }
  }
  return new Map(expectedTargetIds.map((targetId) => [targetId, platform.id]));
}

export async function installRuntimePack(client, input, expectedDefinitionId) {
  const files = input.sourceType === "DIRECTORY" ? directoryFiles(input.sourcePath) : singleFile(input.sourcePath);
  const uploadId = await client.upload(files, input.sourceType, "RUNTIME_ASSET_PACK");
  const body = { uploadId };
  if (input.definitionId !== null) { body.definitionId = input.definitionId; }
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
  if (requirements.length !== 1 || review.rpgMaker.runtimePackSelections.length !== 1
      || review.rpgMaker.runtimePackSelections[0].installationId !== installationId) {
    throw new Error("RPG_009_PROVISION_PROTECTED_REQUIREMENT_INVALID");
  }
  const patched = await client.json("PATCH", `/api/v1/admin/reviews/${review.itemId}`, {
    headers: writeVersionHeaders(client, review.version), expected: 200,
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

export async function trialReview(context, client, baseUrl, review, generation) {
  const create = (restoreFromPreviewId) => client.json(
    "POST", "/api/v1/admin/reviews/" + review.itemId + "/previews", {
      headers: writeVersionHeaders(client, review.version), expected: 201,
      data: {clientCapabilities: capabilities, ...(restoreFromPreviewId ? {restoreFromPreviewId} : {})},
    },
  );
  const sequence = fixtureSequence(generation);
  const created = await create();
  const original = await openPlayer(context, baseUrl, created.playUrl);
  await waitForPreviewReady(original);
  await observePreviewFrames(original);
  const checkpointA = await capturePreviewCheckpoint(original, created.previewId);
  const initialPosition = await observeFixturePosition(original, generation, original.__retromOwnedFixture, checkpointA);
  await advanceFixture(original, sequence.save);
  const checkpointB = await capturePreviewCheckpoint(original, created.previewId);
  const savedPosition = await observeFixturePosition(original, generation, original.__retromOwnedFixture, checkpointB);
  const restored = await create(created.previewId);
  if (restored.previewId === created.previewId) {throw new Error("RPG_009_PROVISION_RESTORE_LAUNCH_REUSED");}
  await advanceFixture(original, sequence.diverge);
  const checkpointC = await capturePreviewCheckpoint(original, created.previewId);
  const divergedPosition = await observeFixturePosition(original, generation, original.__retromOwnedFixture, checkpointC);
  await captureOptionalReviewScreenshot(original, created.previewId);
  await finishPreview(original, created.previewId);
  assertCleanPlayer(original);
  const restorePage = await openPlayer(context, baseUrl, restored.playUrl);
  const envelope = restorePage.__retromEnvelope;
  if (envelope.restore?.sha256 !== checkpointB.sha256 || envelope.restore?.sizeBytes !== checkpointB.sizeBytes ||
      envelope.restore?.format !== checkpointB.format) {
    throw new Error("RPG_009_PROVISION_RESTORE_FROZEN_PAYLOAD_MISMATCH");
  }
  await waitForPreviewReady(restorePage);
  await observePreviewFrames(restorePage);
  const restoredCheckpoint = await capturePreviewCheckpoint(restorePage, restored.previewId);
  const restoredPosition = await observeFixturePosition(restorePage, generation, restorePage.__retromOwnedFixture, restoredCheckpoint);
  await advanceFixture(restorePage, sequence.restore);
  const afterRestore = await capturePreviewCheckpoint(restorePage, restored.previewId);
  const restoreInputPosition = await observeFixturePosition(restorePage, generation, restorePage.__retromOwnedFixture, afterRestore);
  assertRoundTrip([initialPosition, savedPosition, divergedPosition, restoredPosition, restoreInputPosition]);
  await captureOptionalReviewScreenshot(restorePage, restored.previewId);
  await finishPreview(restorePage, restored.previewId);
  assertCleanPlayer(restorePage);
}

export async function approveReview(client, itemId) {
  const review = await client.json("GET", `/api/v1/admin/reviews/${itemId}`);
  if (!review.canApprove || !review.validation?.current || review.validation.status !== "READY") {
    throw new Error("RPG_009_PROVISION_APPROVAL_VALIDATION_INVALID");
  }
  const response = await client.raw("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: writeVersionHeaders(client, review.version), data: {},
  });
  const body = await response.json();
  if (response.status() !== 201 || !body.gameId) {
    throw new Error(`RPG_009_PROVISION_APPROVAL_${response.status()}_${body.error?.code ?? "UNKNOWN"}`);
  }
  const game = await client.json("GET", `/api/v1/admin/games/${body.gameId}`);
  if (game.status !== "PUBLISHED") { throw new Error("RPG_009_PROVISION_GAME_NOT_PUBLISHED"); }
  return body.gameId;
}

export async function createProductSave(context, client, baseUrl, gameId, targetId) {
  const launch = await productLaunch(client, gameId, null);
  const page = await openPlayer(context, baseUrl, launch.playUrl);
  exact(page.__retromEnvelope.runtime.targetId, targetId, "RPG_009_PROVISION_PRODUCT_TARGET_MISMATCH");
  await waitForPreviewReady(page);
  await advanceFixture(page, ["ArrowRight", "KeyX"]);
  const savedPosition = await observeFixturePosition(page, "RPGVX", page.__retromOwnedFixture);
  if (savedPosition.fixtureState !== 1) {throw new Error("RPG_009_PROVISION_PRODUCT_SAVE_POSITION_INVALID");}
  await revealPreviewToolbar(page);
  const saveResponse = page.waitForResponse((response) =>
    response.request().method() === "POST"
      && new URL(response.url()).pathname === "/runtime/launches/" + launch.launchId + "/save-states",
  {timeout: 120_000});
  await page.getByRole("button", {name: "创建存档", exact: true}).click();
  const response = await saveResponse;
  exact(response.status(), 201, "RPG_009_PROVISION_PRODUCT_SAVE_FAILED");
  // Read the committed product projection, independently of Chromium's bounded response cache.
  const saves = await client.json("GET", "/api/v1/saves?gameId=" + encodeURIComponent(gameId) + "&limit=100");
  const receipts = (saves.items ?? []).filter((item) => item.availability?.status === "AVAILABLE" && item.screenshotUrl);
  if (receipts.length !== 1 || !receipts[0].saveStateId) {
    throw new Error("RPG_009_PROVISION_SAVE_NOT_AVAILABLE");
  }
  const receipt = receipts[0];
  await finishPreview(page, launch.launchId);
  assertCleanPlayer(page);
  const restore = await productLaunch(client, gameId, receipt.saveStateId);
  if (restore.launchId === launch.launchId) {throw new Error("RPG_009_PROVISION_PRODUCT_RESTORE_REUSED");}
  const restoredPage = await openPlayer(context, baseUrl, restore.playUrl);
  await waitForPreviewReady(restoredPage);
  await observePreviewFrames(restoredPage);
  const restoredPosition = await observeFixturePosition(restoredPage, "RPGVX", restoredPage.__retromOwnedFixture);
  if (!same(savedPosition, restoredPosition)) {throw new Error("RPG_009_PROVISION_PRODUCT_RESTORE_POSITION_INVALID");}
  await advanceFixture(restoredPage, ["ArrowRight", "KeyX"]);
  const continued = await observeFixturePosition(restoredPage, "RPGVX", restoredPage.__retromOwnedFixture);
  if (continued.fixtureState !== 2 || same(continued, restoredPosition)) {
    throw new Error("RPG_009_PROVISION_PRODUCT_RESTORE_INPUT_INVALID");
  }
  await finishPreview(restoredPage, restore.launchId);
  assertCleanPlayer(restoredPage);
  return receipt.saveStateId;
}

export async function assertProvisionedState(client, protectedReferences, reviewIds, baseline) {
  const [catalog, population] = await Promise.all([
    client.json("GET", "/api/v1/admin/runtime-asset-packs"),
    readPopulation(client),
  ]);
  if (catalog.installations?.length !== 2 || Object.keys(reviewIds).length !== 13) {
    throw new Error("RPG_009_PROVISION_FINAL_CARDINALITY_INVALID");
  }
  for (const reference of Object.values(protectedReferences)) { assertFinalReference(catalog, reference); }
  return preservedPopulation(baseline, population, {
    games: Object.values(protectedReferences).map((reference) => reference.gameId),
    saves: [protectedReferences.restorableCheckpoint.saveStateId],
    reviews: Object.values(reviewIds),
  });
}

function assertFinalReference(catalog, reference) {
  const row = catalog.installations.find((item) => item.installationId === reference.installationId);
  if (!row || row.status !== "READY" || row.references?.variantCount < 1) {
    throw new Error("RPG_009_PROVISION_PROTECTED_REFERENCE_INVALID");
  }
  if (reference.saveStateId && row.references.checkpointCount < 1) {
    throw new Error("RPG_009_PROVISION_CHECKPOINT_REFERENCE_INVALID");
  }
}

function fixtureSequence(generation) {
  return ["RPG2000", "RPG2003"].includes(generation)
    ? {save: ["ArrowRight", "ArrowRight"], diverge: ["ArrowRight", "ArrowRight"],
      restore: ["ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight"]}
    : {save: ["ArrowRight", "KeyX"], diverge: ["ArrowRight", "KeyX"], restore: ["ArrowRight", "KeyX"]};
}

function assertRoundTrip(positions) {
  if (JSON.stringify(positions.map((value) => value?.fixtureState)) !== "[0,1,2,1,2]" ||
      same(positions[0], positions[1]) || same(positions[1], positions[2]) ||
      !same(positions[1], positions[3]) || same(positions[3], positions[4])) {
    throw new Error("RPG_009_PROVISION_RESTORE_EVIDENCE_INVALID");
  }
}

async function productLaunch(client, gameId, saveStateId) {
  return client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: { gameId, coreId: "rpgmaker", saveStateId, dosEntry: null, returnTo: `/games/${gameId}`, clientCapabilities: capabilities },
  });
}

async function openPlayer(context, baseUrl, playerUrl) {
  const page = await context.newPage();
  page.__retromOwnedFixture = await observeOwnedFixture(page);
  page.__retromPageErrors = [];
  page.__retromFatalError = new Promise((resolveError) => {
    page.on("pageerror", (error) => {
      page.__retromPageErrors.push(error.stack || error.message);
      resolveError();
    });
  });
  const configTask = page.waitForResponse((response) =>
    new URL(response.url()).pathname.match(/^\/runtime\/launches\/[^/]+\/config$/));
  await page.goto(new URL(playerUrl, baseUrl).href, {waitUntil: "domcontentloaded", timeout: 120_000});
  const response = await configTask;
  exact(response.status(), 200, "RPG_009_PROVISION_RUNTIME_CONFIG_FAILED");
  page.__retromEnvelope = await response.json();
  return page;
}

function assertCleanPlayer(page) {
  const errors = page.__retromPageErrors ?? [];
  if (errors.length) {throw new Error("RPG_009_PROVISION_PLAYER_ERROR:" + String(errors[0]).slice(0, 600));}
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

function writeVersionHeaders(client, version) {
  return { ...client.writeHeaders(), "Content-Type": "application/json", "If-Match": `"v${version}"` };
}

function same(left, right) { return JSON.stringify(left) === JSON.stringify(right); }
function exact(actual, expected, code) { if (actual !== expected) { throw new Error(`${code}:${actual}`); } }
function delay(milliseconds) { return new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)); }
