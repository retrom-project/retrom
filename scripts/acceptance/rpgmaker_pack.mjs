#!/usr/bin/env node
import { createHash, randomUUID } from "node:crypto";
import {
  closeSync, lstatSync, openSync, readFileSync, readSync, readdirSync, realpathSync, statSync, writeFileSync,
} from "node:fs";
import { basename, isAbsolute, join, relative, resolve, sep } from "node:path";
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import { normalizedBase } from "./rpgmaker_url.mjs";

const caseDir = required("RETROM_RPG_CASE_DIR");
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const plan = JSON.parse(readFileSync(required("RETROM_ACC_RPG_009_PLAN"), "utf8"));
const provisionEvidencePath = exactProvisionEvidence(required("RETROM_ACC_RPG_009_PROVISION_EVIDENCE"));
const provisionEvidence = JSON.parse(readFileSync(provisionEvidencePath, "utf8"));
if (provisionEvidence.schemaVersion !== 1 || provisionEvidence.caseId !== "ACC-RPG-009"
    || provisionEvidence.status !== "PROVISIONED") {
  throw new Error("RPG_ACCEPTANCE_PACK_PROVISION_EVIDENCE_INVALID");
}
const chromeExecutablePath = required("RETROM_CHROME_EXECUTABLE");
const screenshotDir = join(caseDir, "screenshots");
const reviewRoles = {
  rpg2000SelfContained: ["RPG2000", "selfContained", null],
  rpg2000Missing: ["RPG2000", "missing", "RPG2000_RTP"],
  rpg2003SelfContained: ["RPG2003", "selfContained", null],
  rpg2003Missing: ["RPG2003", "missing", "RPG2003_RTP"],
  rpgxpNoRtp: ["RPGXP", "noRtp", null],
  rpgxpStandardAmbiguous: ["RPGXP", "ambiguous", "Standard"],
  rpgxpCustom: ["RPGXP", "missing", "RetromCustomXP"],
  rpgvxNoRtp: ["RPGVX", "noRtp", null],
  rpgvxStandardAmbiguous: ["RPGVX", "ambiguous", "RPGVX"],
  rpgvxCustom: ["RPGVX", "missing", "RetromCustomVX"],
  rpgvxaceNoRtp: ["RPGVXACE", "noRtp", null],
  rpgvxaceStandardAmbiguous: ["RPGVXACE", "ambiguous", "RPGVXAce"],
  rpgvxaceCustom: ["RPGVXACE", "missing", "RetromCustomVXAce"],
};
const browser = await chromium.launch({ executablePath: chromeExecutablePath, headless: true });

try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const login = await jsonRequest(context.request, "POST", "/api/v1/auth/login", {
    headers: { Origin: baseUrl }, data: {
      username: required("RETROM_ACCEPTANCE_USERNAME"), password: required("RETROM_ACCEPTANCE_PASSWORD"),
    }, expected: 200,
  });
  const writeHeaders = () => ({
    Origin: baseUrl, "X-Retrom-Csrf": login.csrfToken, "Idempotency-Key": randomUUID(),
  });
  const before = await packCatalog(context.request);
  const expectedInitialInstallations = new Set(Object.values(plan.protectedReferences).map((item) => item.installationId));
  if (before.installations.length !== expectedInitialInstallations.size
      || before.installations.some((row) => !expectedInitialInstallations.has(row.installationId))) {
    throw new Error("RPG_ACCEPTANCE_PACK_DATABASE_NOT_DEDICATED");
  }
  const protectedRows = Object.fromEntries(Object.entries(plan.protectedReferences).map(([role, reference]) =>
    [role, installation(before, reference.installationId)]));
  if (protectedRows.publishedVariant.definitionId !== "rgss1_standard"
      || protectedRows.restorableCheckpoint.definitionId !== "rgss2_rpgvx") {
    throw new Error("RPG_ACCEPTANCE_PACK_PROTECTED_DEFINITION_INVALID");
  }
  if (protectedRows.publishedVariant.references.gameCount < 1
      || protectedRows.restorableCheckpoint.references.checkpointCount < 1) {
    throw new Error("RPG_ACCEPTANCE_PACK_REFERENCE_COVERAGE_INCOMPLETE");
  }
  const reviews = {};
  for (const [role, reviewId] of Object.entries(plan.reviewIds)) {
    reviews[role] = await reviewSnapshot(context.request, reviewId);
    validateReviewRole(role, reviews[role].body);
  }
  const matcherRejections = [];
  for (const role of ["rpg2000Missing", "rpg2003Missing", "rpgxpCustom", "rpgvxCustom", "rpgvxaceCustom"]) {
    matcherRejections.push(await rejectIncompleteBinding(context.request, writeHeaders, role, reviews[role]));
  }
  const installations = {};
  for (const [role, upload] of Object.entries(plan.uploads)) {
    installations[role] = await uploadAndInstall(context.request, writeHeaders, role, upload);
  }
  const installedCatalog = await packCatalog(context.request);
  const installedRows = Object.fromEntries(Object.entries(installations).map(([role, item]) =>
    [role, installation(installedCatalog, item.installationId)]));
  if (Object.values(installedRows).some((row) => row.status !== "READY" || !digest(row.filesDigest))) {
    throw new Error("RPG_ACCEPTANCE_PACK_INSTALLATION_NOT_READY");
  }
  for (const [protectedRole, uploadRoles] of Object.entries({
    publishedVariant: ["rgss1StandardV1", "rgss1StandardV2"],
    restorableCheckpoint: ["rgss2StandardV1", "rgss2StandardV2"],
  })) {
    const versions = uploadRoles.map((role) => installedRows[role]);
    if (versions.some((row) => row.definitionId !== protectedRows[protectedRole].definitionId)
        || new Set(versions.map((row) => row.filesDigest)).size !== versions.length
        || versions.some((row) => row.filesDigest === protectedRows[protectedRole].filesDigest)) {
      throw new Error("RPG_ACCEPTANCE_PACK_NEW_VERSION_IDENTITY_INVALID");
    }
  }
  const protectedDefinitions = new Map(Object.values(protectedRows).map((row) => [row.definitionId, row.filesDigest]));
  if (!Object.values(installedRows).some((row) => protectedDefinitions.has(row.definitionId)
      && protectedDefinitions.get(row.definitionId) !== row.filesDigest)) {
    throw new Error("RPG_ACCEPTANCE_PACK_NEW_VERSION_NOT_INSTALLED");
  }
  for (const [role, uploadRole] of Object.entries({
    rpg2000Missing: "rpg2000Rtp", rpg2003Missing: "rpg2003Rtp",
    rpgxpCustom: "rgss1Custom", rpgvxCustom: "rgss2Custom", rpgvxaceCustom: "rgss3Custom",
  })) {
    matcherRejections.push(await selectBindingAndRejectStaleApproval(
      context.request, writeHeaders, role, reviews[role], installedRows[uploadRole].installationId,
    ));
  }
  for (const [role, uploadRole] of Object.entries({
    rpgxpStandardAmbiguous: "rgss1StandardV1",
    rpgvxStandardAmbiguous: "rgss2StandardV1",
    rpgvxaceStandardAmbiguous: "rgss3StandardV1",
  })) {
    matcherRejections.push(await rejectAmbiguousThenSelect(
      context.request, writeHeaders, role, reviews[role], installedRows[uploadRole].installationId,
    ));
  }
  const page = await context.newPage();
  await captureAdminPage(page, `${baseUrl}/admin/bios?tab=rpgmaker`, "rpgmaker-pack-catalog.png");
  await captureAdminPage(
    page, `${baseUrl}/admin/reviews/${plan.reviewIds.rpg2000Missing}`, "rpgmaker-pack-review-binding.png",
  );
  await page.close();
  const publishedReviews = [];
  for (const role of [
    "rpg2000SelfContained", "rpg2003SelfContained", "rpgxpNoRtp", "rpgvxNoRtp", "rpgvxaceNoRtp",
  ]) {
    publishedReviews.push(await approveReadyReview(context.request, writeHeaders, role, reviews[role]));
  }
  const protectedDeletes = [];
  for (const [role, row] of Object.entries(protectedRows)) {
    const response = await rawRequest(context.request, "DELETE", `/api/v1/admin/runtime-asset-packs/installations/${row.installationId}`, {
      headers: { ...writeHeaders(), "If-Match": `"v${row.version}"` }, expected: 409,
    });
    const body = await response.json();
    if (body.error?.code !== "RPG_RUNTIME_PACK_IN_USE") {
      throw new Error("RPG_ACCEPTANCE_PACK_REFERENCE_DELETE_CODE_INVALID");
    }
    protectedDeletes.push({ role, installationId: row.installationId, status: 409, code: body.error.code });
  }
  const zero = installedRows.zeroReference;
  if (zero.references.gameCount !== 0 || zero.references.checkpointCount !== 0 || zero.status !== "READY") {
    throw new Error("RPG_ACCEPTANCE_PACK_ZERO_REFERENCE_PRECONDITION_INVALID");
  }
  await rawRequest(context.request, "DELETE", `/api/v1/admin/runtime-asset-packs/installations/${zero.installationId}`, {
    headers: { ...writeHeaders(), "If-Match": `"v${zero.version + 1}"` }, expected: 412,
  });
  await rawRequest(context.request, "DELETE", `/api/v1/admin/runtime-asset-packs/installations/${zero.installationId}`, {
    headers: { ...writeHeaders(), "If-Match": `"v${zero.version}"` }, expected: 204,
  });
  const after = await packCatalog(context.request);
  const deleted = installation(after, zero.installationId);
  if (deleted.status !== "DELETED" || deleted.deletedAtMs === null
      || deleted.references.gameCount !== 0 || deleted.references.checkpointCount !== 0) {
    throw new Error("RPG_ACCEPTANCE_PACK_DELETE_PROJECTION_INVALID");
  }
  for (const prior of Object.values(protectedRows)) {
    const current = installation(after, prior.installationId);
    if (current.status !== prior.status || current.filesDigest !== prior.filesDigest
        || JSON.stringify(current.references) !== JSON.stringify(prior.references)) {
      throw new Error("RPG_ACCEPTANCE_PACK_PROTECTED_ROW_MUTATED");
    }
  }
  const evidence = {
    schemaVersion: 1, caseId: "ACC-RPG-009", status: "OBSERVED",
    installations: Object.fromEntries(Object.entries(installedRows).map(([role, row]) => [role, safeInstallation(row)])),
    reviews: { published: publishedReviews, matcherRejections },
    protectedReferences: plan.protectedReferences,
    protectedDeletes, zeroReferenceDelete: {
      installationId: zero.installationId, staleStatus: 412, currentStatus: 204,
      finalStatus: deleted.status, deletedAtMs: deleted.deletedAtMs,
    },
    uploads: installations,
    screenshots: ["screenshots/rpgmaker-pack-catalog.png", "screenshots/rpgmaker-pack-review-binding.png"],
  };
  writeFileSync(join(caseDir, "rpgmaker-product.json"), `${JSON.stringify(evidence, null, 2)}\n`);
} finally {
  await browser.close();
}

async function captureAdminPage(page, url, filename) {
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: 120_000 });
  await page.locator("main.content h1").first().waitFor({ state: "visible", timeout: 120_000 });
  await page.screenshot({ path: join(screenshotDir, filename), fullPage: true });
}

async function uploadAndInstall(request, writeHeaders, role, input) {
  const files = sourceFiles(input.sourcePath, input.sourceType);
  const upload = await jsonRequest(request, "POST", "/api/v1/admin/uploads", {
    headers: writeHeaders(), expected: 201,
    data: {
      purpose: "RUNTIME_ASSET_PACK", sourceType: input.sourceType,
      files: files.map((file, index) => ({ clientFileId: `pack-${index}`, relativePath: file.relativePath, sizeBytes: file.sizeBytes })),
    },
  });
  for (let index = 0; index < files.length; index += 1) {
    const remote = upload.files.find((file) => file.clientFileId === `pack-${index}`);
    if (!remote) { throw new Error("RPG_ACCEPTANCE_PACK_UPLOAD_FILE_MAPPING_MISSING"); }
    await uploadFile(request, writeHeaders, upload.uploadId, remote.fileId, files[index], upload.chunkSizeBytes);
  }
  const uploadResponse = await rawRequest(request, "GET", `/api/v1/admin/uploads/${upload.uploadId}`, { expected: 200 });
  const etag = uploadResponse.headers().etag;
  if (!etag) { throw new Error("RPG_ACCEPTANCE_PACK_UPLOAD_ETAG_MISSING"); }
  const completed = await jsonRequest(request, "POST", `/api/v1/admin/uploads/${upload.uploadId}/complete`, {
    headers: { ...writeHeaders(), "If-Match": etag }, expected: 202,
  });
  const finalizeJob = await waitForJob(request, completed.jobId);
  const installBody = { uploadId: upload.uploadId };
  if (input.definitionId !== null) { installBody.definitionId = input.definitionId; }
  if (input.generation !== null) { installBody.generation = input.generation; }
  if (input.declaredName !== null) { installBody.declaredName = input.declaredName; }
  if (input.sourceNote !== null) { installBody.sourceNote = input.sourceNote; }
  const accepted = await installWithResourceRetry(request, writeHeaders, installBody);
  const validationJob = await waitForJob(request, accepted.jobId);
  return {
    role, uploadId: upload.uploadId, installationId: accepted.installationId,
    jobId: accepted.jobId, definitionId: input.definitionId, finalizeJob, validationJob,
  };
}

async function installWithResourceRetry(request, writeHeaders, body) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const response = await request.fetch(`${baseUrl}/api/v1/admin/runtime-asset-packs/installations`, {
      method: "POST", headers: writeHeaders(), data: body, failOnStatusCode: false,
    });
    if (response.status() === 202) { return response.json(); }
    const code = await responseErrorCode(response);
    if (response.status() !== 503 || code !== "RPG_RUNTIME_PACK_UNAVAILABLE" || attempt === 2) {
      throw new Error(`RPG_ACCEPTANCE_HTTP_POST_${response.status()}_${code}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 250 * (attempt + 1)));
  }
  throw new Error("RPG_ACCEPTANCE_PACK_RESOURCE_RETRY_EXHAUSTED");
}

async function uploadFile(request, writeHeaders, uploadId, fileId, file, chunkSize) {
  const descriptor = openSync(file.path, "r");
  try {
    for (let start = 0, part = 0; start < file.sizeBytes; start += chunkSize, part += 1) {
      const length = Math.min(chunkSize, file.sizeBytes - start);
      const chunk = Buffer.alloc(length);
      if (readSync(descriptor, chunk, 0, length, start) !== length) {
        throw new Error("RPG_ACCEPTANCE_PACK_SOURCE_READ_SHORT");
      }
      const digestValue = createHash("sha256").update(chunk).digest("base64");
      await rawRequest(request, "PUT", `/api/v1/admin/uploads/${uploadId}/files/${fileId}/parts/${part}`, {
        headers: {
          ...writeHeaders(), "Content-Type": "application/octet-stream",
          "Content-Range": `bytes ${start}-${start + length - 1}/${file.sizeBytes}`,
          "Content-Digest": `sha-256=:${digestValue}:`,
        }, data: chunk, expected: 204,
      });
    }
  } finally { closeSync(descriptor); }
}

function sourceFiles(sourcePath, sourceType) {
  const root = resolve(sourcePath);
  if (lstatSync(root).isSymbolicLink()) { throw new Error("RPG_ACCEPTANCE_PACK_SOURCE_SYMLINK"); }
  if (sourceType === "FILES") {
    return [{ path: root, relativePath: basename(root), sizeBytes: statSync(root).size }];
  }
  const result = [];
  walk(root, root, result);
  if (!result.length || result.length > 10_000) { throw new Error("RPG_ACCEPTANCE_PACK_SOURCE_FILE_COUNT"); }
  return result.sort((left, right) => left.relativePath.localeCompare(right.relativePath));
}

function walk(root, directory, result) {
  for (const name of readdirSync(directory)) {
    const path = join(directory, name);
    const info = lstatSync(path);
    if (info.isSymbolicLink()) { throw new Error("RPG_ACCEPTANCE_PACK_SOURCE_SYMLINK"); }
    if (info.isDirectory()) { walk(root, path, result); }
    else if (info.isFile()) {
      result.push({ path, relativePath: relative(root, path).split(sep).join("/"), sizeBytes: info.size });
    }
  }
}

async function waitForJob(request, jobId) {
  for (let attempt = 0; attempt < 600; attempt += 1) {
    const job = await jsonRequest(request, "GET", `/api/v1/admin/jobs/${jobId}`);
    if (job.state === "SUCCEEDED") {
      const response = await rawRequest(request, "GET", `/api/v1/admin/jobs/${jobId}/events`, {
        headers: { "Last-Event-ID": "0" }, expected: 200,
      });
      const events = [...(await response.text()).matchAll(/^event: ([a-z_]+)$/gm)].map((match) => match[1].toUpperCase());
      const requiredEvents = job.kind === "UPLOAD_FINALIZE" ? ["SUCCEEDED"] : ["QUEUED", "STARTED", "SUCCEEDED"];
      if (!requiredEvents.every((event) => events.includes(event))) {
        throw new Error("RPG_ACCEPTANCE_PACK_JOB_EVENTS_INCOMPLETE");
      }
      return {
        jobId: job.jobId, kind: job.kind, scopeType: job.scopeType, scopeId: job.scopeId,
        state: job.state, attemptCount: job.attemptCount, events,
      };
    }
    if (["FAILED", "CANCELLED"].includes(job.state)) {
      throw new Error(`RPG_ACCEPTANCE_PACK_JOB_${job.errorCode ?? job.state}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 250));
  }
  throw new Error("RPG_ACCEPTANCE_PACK_JOB_TIMEOUT");
}

async function reviewSnapshot(request, reviewId) {
  const response = await rawRequest(request, "GET", `/api/v1/admin/reviews/${reviewId}`);
  const etag = response.headers().etag;
  if (!etag || !/^"v[1-9][0-9]*"$/.test(etag)) {throw new Error("RPG_ACCEPTANCE_PACK_REVIEW_ETAG_MISSING");}
  return { body: await response.json(), etag };
}

function validateReviewRole(role, review) {
  const rpg = review.rpgMaker;
  const expected = reviewRoles[role];
  if (!rpg || !expected || rpg.generation !== expected[0]) {
    throw new Error("RPG_ACCEPTANCE_PACK_REVIEW_ROLE_INVALID");
  }
  const [generation, mode, declaredName] = expected;
  const requirements = rpg.runtimePackRequirements;
  if (mode === "selfContained") {
    if (!(rpg.selfContained || rpg.selfContainedOverride) || requirements.length || rpg.runtimePackSelections.length) {
      throw new Error("RPG_ACCEPTANCE_PACK_SELF_CONTAINED_PRECONDITION_INVALID");
    }
  } else if (mode === "noRtp") {
    if (requirements.length || rpg.runtimePackSelections.length || rpg.selfContainedOverride) {
      throw new Error("RPG_ACCEPTANCE_PACK_NO_RTP_PRECONDITION_INVALID");
    }
  } else if (requirements.length !== 1 || requirements[0].declaredName !== declaredName
      || requirements[0].slot !== (["RPG2000", "RPG2003"].includes(generation) ? 0 : 1)
      || rpg.runtimePackSelections.length !== 0 || rpg.selfContainedOverride) {
    throw new Error("RPG_ACCEPTANCE_PACK_MATCHER_PRECONDITION_INVALID");
  }
  if (["selfContained", "noRtp"].includes(mode)
      && (!rpg.runtimeValidationCurrent || rpg.runtimeValidation?.state !== "PASSED")) {
    throw new Error("RPG_ACCEPTANCE_PACK_REVIEW_VALIDATION_NOT_PASSED");
  }
}

function reviewTags(review) {
  return (review.tags ?? []).map((tag) => tag.tagId);
}

async function patchBinding(request, writeHeaders, snapshot, selections, expected) {
  const response = await rawRequest(request, "PATCH", `/api/v1/admin/reviews/${snapshot.body.itemId}`, {
    headers: { ...writeHeaders(), "If-Match": snapshot.etag, "Content-Type": "application/json" },
    data: {
      tagIds: reviewTags(snapshot.body), runtimePackSelections: selections,
      rpgSelfContainedOverride: false,
    }, expected,
  });
  return { response, body: await response.json(), etag: response.headers().etag ?? snapshot.etag };
}

async function rejectApproval(request, writeHeaders, snapshot) {
  const response = await rawRequest(request, "POST", `/api/v1/admin/reviews/${snapshot.body.itemId}/approve`, {
    headers: { ...writeHeaders(), "If-Match": snapshot.etag, "Content-Type": "application/json" },
    data: {}, expected: 409,
  });
  const body = await response.json();
  if (body.error?.code !== "REVIEW_VALIDATION_STALE") {
    throw new Error("RPG_ACCEPTANCE_PACK_PUBLISH_REJECTION_INVALID");
  }
  return { status: 409, code: body.error.code };
}

async function rejectIncompleteBinding(request, writeHeaders, role, snapshot) {
  const patched = await patchBinding(request, writeHeaders, snapshot, [], 422);
  if (patched.body.error?.code !== "REVIEW_DRAFT_INVALID") {
    throw new Error("RPG_ACCEPTANCE_PACK_MATCHER_REJECTION_INVALID");
  }
  return {
    role, matcher: "MISSING", patchStatus: 422, patchCode: patched.body.error.code,
    publish: await rejectApproval(request, writeHeaders, snapshot),
  };
}

async function selectBindingAndRejectStaleApproval(request, writeHeaders, role, snapshot, installationId) {
  const requirement = snapshot.body.rpgMaker.runtimePackRequirements[0];
  const patched = await patchBinding(request, writeHeaders, snapshot, [{ slot: requirement.slot, installationId }], 200);
  if (patched.body.itemId !== snapshot.body.itemId || patched.body.version !== snapshot.body.version + 1) {
    throw new Error("RPG_ACCEPTANCE_PACK_PATCH_RESULT_INVALID");
  }
  const refreshed = await reviewSnapshot(request, snapshot.body.itemId);
  const selected = refreshed.body.rpgMaker?.runtimePackSelections?.find((item) => item.slot === requirement.slot);
  if (selected?.installationId !== installationId || refreshed.body.rpgMaker?.runtimeValidationCurrent !== false) {
    throw new Error("RPG_ACCEPTANCE_PACK_EXPLICIT_SELECTION_INVALID");
  }
  return {
    role, itemId: snapshot.body.itemId, matcher: "SELECTED", patchStatus: 200, installationId,
    publish: await rejectApproval(request, writeHeaders, refreshed),
  };
}

async function rejectAmbiguousThenSelect(request, writeHeaders, role, snapshot, installationId) {
  const rejected = await patchBinding(request, writeHeaders, snapshot, [], 422);
  if (rejected.body.error?.code !== "REVIEW_DRAFT_INVALID") {
    throw new Error("RPG_ACCEPTANCE_PACK_MATCHER_REJECTION_INVALID");
  }
  const selected = await selectBindingAndRejectStaleApproval(request, writeHeaders, role, snapshot, installationId);
  return { ...selected, matcher: "AMBIGUOUS", rejectionStatus: 422, rejectionCode: rejected.body.error.code };
}

async function approveReadyReview(request, writeHeaders, role, snapshot) {
  const response = await rawRequest(request, "POST", `/api/v1/admin/reviews/${snapshot.body.itemId}/approve`, {
    headers: { ...writeHeaders(), "If-Match": snapshot.etag, "Content-Type": "application/json" },
    data: {}, expected: 201,
  });
  const body = await response.json();
  if (!/^[0-9a-f-]{36}$/.test(body.gameId ?? "")) {throw new Error("RPG_ACCEPTANCE_PACK_APPROVAL_INVALID");}
  return {
    role, itemId: snapshot.body.itemId, gameId: body.gameId,
    validationId: snapshot.body.rpgMaker.runtimeValidation.validationId,
    generation: snapshot.body.rpgMaker.generation, status: 201,
  };
}

async function packCatalog(request) {
  return jsonRequest(request, "GET", "/api/v1/admin/runtime-asset-packs");
}

function installation(catalog, id) {
  const found = catalog.installations.find((row) => row.installationId === id);
  if (!found) { throw new Error("RPG_ACCEPTANCE_PACK_INSTALLATION_NOT_FOUND"); }
  return found;
}

function safeInstallation(row) {
  return {
    installationId: row.installationId, definitionId: row.definitionId, filesDigest: row.filesDigest,
    fileCount: row.fileCount, totalBytes: row.totalBytes, bundleSha256: row.bundleSha256,
    status: row.status, diagnostics: row.diagnostics, sourceNote: row.sourceNote,
    references: row.references, version: row.version, createdAtMs: row.createdAtMs, validatedAtMs: row.validatedAtMs,
  };
}

async function rawRequest(request, method, path, options = {}) {
  const response = await request.fetch(`${baseUrl}${path}`, {
    method, headers: options.headers, data: options.data, failOnStatusCode: false,
  });
  if (response.status() !== (options.expected ?? 200)) {
    const code = await responseErrorCode(response);
    throw new Error(`RPG_ACCEPTANCE_HTTP_${method}_${response.status()}_${code}`);
  }
  return response;
}

async function responseErrorCode(response) {
  try {
    const body = JSON.parse(await response.text());
    return /^[A-Z][A-Z0-9_]{0,127}$/.test(body?.error?.code) ? body.error.code : "UNKNOWN";
  } catch {
    return "UNKNOWN";
  }
}

async function jsonRequest(request, method, path, options = {}) {
  return (await rawRequest(request, method, path, options)).json();
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_ACCEPTANCE_ENV_MISSING_${name}`); }
  return value;
}

function digest(value) { return typeof value === "string" && /^[0-9a-f]{64}$/.test(value); }
function exactProvisionEvidence(value) {
  if (!isAbsolute(value)) { throw new Error("RPG_ACCEPTANCE_PACK_PROVISION_EVIDENCE_INVALID"); }
  const path = resolve(value);
  if (!statSync(path).isFile() || lstatSync(path).isSymbolicLink() || realpathSync(path) !== path) {
    throw new Error("RPG_ACCEPTANCE_PACK_PROVISION_EVIDENCE_INVALID");
  }
  return path;
}
