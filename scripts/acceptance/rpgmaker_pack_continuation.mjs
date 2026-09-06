import {createHash} from "node:crypto";
import {readFileSync} from "node:fs";
import {basename} from "node:path";
import {directoryFiles} from "./rpgmaker_security_upload.mjs";
import {buildPlan, protectedRoles, reviewRoles} from "./rpgmaker_pack_provision_plan.mjs";
import {checkedPopulationPreservation, preservedPopulation, readPopulation} from "./rpgmaker_pack_population.mjs";
import {expectedResumeInputs} from "./rpgmaker_pack_resume.mjs";
import {assertReviewRole} from "./rpgmaker_pack_review_state.mjs";

const roles = Object.keys(protectedRoles);
const invalid = () => {throw new Error("RPG_009_CONTINUATION_STATE_INVALID");};

export function validatePartialRequest(value) {
  if (!keys(value, ["schemaVersion", "installations", "protectedReferences", "reviewIds", "previousEvidence"]) ||
      value.schemaVersion !== 2 || !keys(value.installations, roles) ||
      !keys(value.previousEvidence, ["resume", "populationBefore"])) {invalid();}
  buildPlan({inputs: {}}, value.reviewIds, value.protectedReferences);
  for (const role of roles) {
    if (value.installations[role] !== value.protectedReferences[role].installationId) {invalid();}
  }
  const prior = value.previousEvidence;
  checkedPopulationPreservation({before: prior.populationBefore, after: prior.populationBefore});
  if (!keys(prior.resume, ["schemaVersion", "mode", "capturedAtMs", "installations", "review"]) ||
      prior.resume.schemaVersion !== 1 || prior.resume.mode !== "EXPLICIT_PROTECTED_PREVIEW" ||
      !Number.isSafeInteger(prior.resume.capturedAtMs) || prior.resume.capturedAtMs <= 0 ||
      !keys(prior.resume.installations, roles) ||
      !keys(prior.resume.review, ["itemId", "version", "sourceSha256", "populationRow"])) {invalid();}
  const review = prior.resume.review;
  if (!Number.isSafeInteger(review.version) || review.version < 1 ||
      review.itemId !== review.populationRow?.id || !digest(review.sourceSha256) ||
      Object.values(value.reviewIds).includes(review.itemId)) {invalid();}
  checkedPopulationPreservation({before: {games: [], saves: [], reviews: [review.populationRow]},
    after: {games: [], saves: [], reviews: [review.populationRow]}});
  if (Object.values(prior.populationBefore).flat().some((row) => row.id === review.itemId)) {invalid();}
  return value;
}

export async function capturePartialResume(client, inputs, request) {
  validatePartialRequest(request);
  const expected = expectedPartialInputs(inputs);
  const [catalog, population, games, reviews, save, history] = await Promise.all([
    client.json("GET", "/api/v1/admin/runtime-asset-packs"), readPopulation(client),
    readRoles(request.protectedReferences, (ref) => client.json("GET", `/api/v1/admin/games/${ref.gameId}`)),
    readRoles(request.reviewIds, (id) => client.json("GET", `/api/v1/admin/reviews/${id}`)),
    readReferenceSave(client, request.protectedReferences.restorableCheckpoint),
    approvedHistory(client, request.previousEvidence.resume.review.itemId),
  ]);
  const result = validatePartialState({request, expected, catalog, population, games, reviews, save, history});
  return {...result, resume: {
    schemaVersion: 2, mode: "EXPLICIT_PARTIAL_PROVISION", capturedAtMs: Date.now(),
    previous: request.previousEvidence.resume,
    installations: Object.fromEntries(roles.map((role) => [role, {
      installationId: request.installations[role], filesDigest: expected.packs[role].filesDigest,
      sourceSha256: expected.packSources[role],
    }])),
    protectedReferences: request.protectedReferences, reviewIds: request.reviewIds,
    protectedPopulation: population,
    sourceIdentities: {protectedProjects: expected.projectSources,
      reviewProjects: Object.fromEntries(Object.entries(inputs.reviewProjects).map(([role, row]) => [role, row.sourceSha256]))},
    approvedReviewEventId: history.reviewEventId,
  }};
}

export function validatePartialState({request, expected, catalog, population, games, reviews, save, history}) {
  validatePartialRequest(request);
  if (!Array.isArray(catalog?.installations) || catalog.installations.length !== 2) {invalid();}
  const installations = {};
  for (const role of roles) {
    const ref = request.protectedReferences[role];
    const pack = catalog.installations.find((row) => row.installationId === ref.installationId);
    if (!pack || pack.status !== "READY" || pack.references?.gameCount !== 1 ||
        pack.references?.checkpointCount !== (role === "restorableCheckpoint" ? 1 : 0) ||
        Object.entries(expected.packs[role]).some(([key, value]) => pack[key] !== value)) {invalid();}
    const previous = request.previousEvidence.resume.installations[role];
    if (!keys(previous, ["installationId", "filesDigest", "sourceSha256"]) ||
        previous.installationId !== ref.installationId || previous.filesDigest !== pack.filesDigest ||
        previous.sourceSha256 !== expected.packSources[role]) {invalid();}
    validateGame(games[role], ref, expected.games[role], protectedRoles[role][3], pack.filesDigest);
    installations[role] = pack;
  }
  if (save?.saveStateId !== request.protectedReferences.restorableCheckpoint.saveStateId ||
      save.gameId !== request.protectedReferences.restorableCheckpoint.gameId || save.availability?.status !== "AVAILABLE" ||
      history?.importItemId !== request.previousEvidence.resume.review.itemId || history.eventType !== "APPROVED" ||
      history.after?.gameId !== request.protectedReferences.publishedVariant.gameId ||
      request.previousEvidence.resume.review.sourceSha256 !== expected.projectSources.publishedVariant) {invalid();}
  for (const [role, itemId] of Object.entries(request.reviewIds)) {
    const review = reviews[role];
    if (review?.itemId !== itemId || !Number.isSafeInteger(review.version) || review.version < 1 ||
        !same(sourceRows(review.sourceFiles), expected.reviews[role])) {invalid();}
    assertReviewRole(role, review, reviewRoles[role]);
    if (["rpgxpStandardAmbiguous", "rpgvxStandardAmbiguous"].includes(role) &&
        review.rpgMaker.runtimePackSelections[0].installationId !==
        request.installations[role.startsWith("rpgxp") ? "publishedVariant" : "restorableCheckpoint"]) {invalid();}
  }
  const populationBefore = request.previousEvidence.populationBefore;
  checkedPopulationPreservation({before: population, after: population});
  preservedPopulation(populationBefore, population, {
    games: roles.map((role) => request.protectedReferences[role].gameId),
    saves: [save.saveStateId], reviews: Object.values(request.reviewIds),
  });
  return {installations, reviews, protectedReferences: request.protectedReferences,
    reviewIds: request.reviewIds, populationBefore, protectedPopulation: population};
}

function validateGame(game, reference, files, targetId, packDigest) {
  const variants = game?.variants?.filter((row) => row.coreId === "rpgmaker");
  const actual = game?.files?.map(({logicalName, role, sizeBytes, sha256}) => ({logicalName, role, sizeBytes, sha256}));
  if (game?.gameId !== reference.gameId || game.status !== "PUBLISHED" || game.contentKind !== "RPG_MAKER_PROJECT" ||
      !same(sortFiles(actual, "logicalName"), sortFiles(files, "logicalName")) || variants?.length !== 1) {invalid();}
  const variant = variants[0], bindings = variant.dependencySnapshot?.bindings;
  if (variant.providerId !== "retrom-runtime" || variant.targetId !== targetId || variant.status !== "READY" ||
      bindings?.length !== 1 || bindings[0].installationId !== reference.installationId ||
      bindings[0].filesDigest !== packDigest) {invalid();}
}

function expectedPartialInputs(inputs) {
  const expected = expectedResumeInputs(inputs);
  return {...expected,
    packSources: Object.fromEntries(roles.map((role) => [role, inputs.protectedPackInputs[role].sourceSha256])),
    projectSources: Object.fromEntries(roles.map((role) => [role, inputs.protectedProjects[role].sourceSha256])),
    games: Object.fromEntries(roles.map((role) => [role, fileRows(inputs.protectedProjects[role].sourcePath, false)])),
    reviews: Object.fromEntries(Object.keys(reviewRoles).map((role) => [role, fileRows(inputs.reviewProjects[role].sourcePath, true)])),
  };
}

function fileRows(root, review) {
  const files = directoryFiles(root, review ? `${basename(root)}/` : "");
  return files.map((file) => ({
    ...(review ? {name: file.relativePath} : {logicalName: file.relativePath, role: "PROJECT_FILE"}),
    sizeBytes: file.sizeBytes, sha256: createHash("sha256").update(readFileSync(file.path)).digest("hex"),
  })).sort((a, b) => (a.name ?? a.logicalName).localeCompare(b.name ?? b.logicalName));
}

async function approvedHistory(client, itemId) {
  const cursors = new Set();
  let cursor = null;
  const matches = [];
  for (let page = 0; page < 500; page++) {
    const result = await client.json("GET", "/api/v1/admin/review-history?limit=20" +
      (cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""));
    if (!Array.isArray(result.items)) {invalid();}
    matches.push(...result.items.filter((row) => row.importItemId === itemId && row.decision === "APPROVED"));
    if (result.nextCursor === null) {
      if (matches.length !== 1) {invalid();}
      return client.json("GET", `/api/v1/admin/review-history/${matches[0].reviewEventId}`);
    }
    cursor = result.nextCursor;
    if (typeof cursor !== "string" || !cursor || cursors.has(cursor)) {invalid();}
    cursors.add(cursor);
  }
  invalid();
}

async function readRoles(rows, read) {
  return Object.fromEntries(await Promise.all(Object.entries(rows).map(async ([role, value]) => [role, await read(value)])));
}
export async function readReferenceSave(client, ref) {
  const result = await client.json("GET", `/api/v1/saves?gameId=${encodeURIComponent(ref.gameId)}&availability=ALL&limit=100`);
  const matches = result.items?.filter((row) => row.saveStateId === ref.saveStateId);
  if (result.nextCursor !== null || matches?.length !== 1) {invalid();}
  return matches[0];
}
function sortFiles(rows, key) {return Array.isArray(rows) ? [...rows].sort((a, b) => a[key].localeCompare(b[key])) : null;}
function sourceRows(rows) {return sortFiles(rows?.map(({name, sha256, sizeBytes}) => ({name, sha256, sizeBytes})), "name");}
function digest(value) {return typeof value === "string" && /^[0-9a-f]{64}$/.test(value);}
function same(a, b) {return JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));}
function canonical(value) {
  if (Array.isArray(value)) {return value.map(canonical);}
  if (!value || typeof value !== "object") {return value;}
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
}
function keys(value, expected) {return value && typeof value === "object" && !Array.isArray(value) &&
  Object.keys(value).sort().join() === [...expected].sort().join();}
