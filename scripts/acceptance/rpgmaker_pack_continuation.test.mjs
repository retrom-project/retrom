import {test} from "node:test";
import assert from "node:assert/strict";
import {validateResumeRequest, validateResumeState} from "./rpgmaker_pack_resume.mjs";
import {reviewRoles, protectedRoles} from "./rpgmaker_pack_provision_plan.mjs";
import {readReferenceSave} from "./rpgmaker_pack_continuation.mjs";

const id = (n) => `01980000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const sha = "a".repeat(64);
const row = (id) => ({id, sha256: sha});

test("continuation resolves the exact save through the public paginated collection", async () => {
  const ref = {gameId: id(4), saveStateId: id(5)};
  const save = {saveStateId: id(5), gameId: id(4)};
  const client = {json: async (method, route) => {
    assert.equal(method, "GET");
    assert.equal(route, `/api/v1/saves?gameId=${id(4)}&availability=ALL&limit=100`);
    return {items: [save], nextCursor: null};
  }};
  assert.deepEqual(await readReferenceSave(client, ref), save);
});

function fixture() {
  const references = {publishedVariant: {installationId: id(1), gameId: id(3)},
    restorableCheckpoint: {installationId: id(2), gameId: id(4), saveStateId: id(5)}};
  const reviewIds = Object.fromEntries(Object.keys(reviewRoles).map((role, n) => [role, id(n + 10)]));
  const previousEvidence = {resume: {schemaVersion: 1, mode: "EXPLICIT_PROTECTED_PREVIEW", capturedAtMs: 1,
    installations: Object.fromEntries(Object.entries(references).map(([role, ref]) => [role,
      {installationId: ref.installationId, filesDigest: sha, sourceSha256: sha}])),
    review: {itemId: id(6), version: 1, sourceSha256: sha, populationRow: row(id(6))}},
  populationBefore: {games: [row(id(30))], saves: [], reviews: []}};
  const request = {schemaVersion: 2, installations: {publishedVariant: id(1), restorableCheckpoint: id(2)},
    protectedReferences: references, reviewIds, previousEvidence};
  const expected = {packs: {}, games: {}, reviews: {}, projectSources: {publishedVariant: sha, restorableCheckpoint: sha},
    packSources: {publishedVariant: sha, restorableCheckpoint: sha}};
  const catalog = {installations: []}, games = {}, reviews = {};
  for (const [role, ref] of Object.entries(references)) {
    const identity = protectedRoles[role];
    expected.packs[role] = {definitionId: identity[5], sourceNote: "owned", filesDigest: sha, fileCount: 1, totalBytes: 10};
    catalog.installations.push({...expected.packs[role], installationId: ref.installationId, status: "READY",
      references: {gameCount: 1, checkpointCount: role === "restorableCheckpoint" ? 1 : 0}});
    expected.games[role] = [{logicalName: "Game.ini", sha256: sha, sizeBytes: 10, role: "PROJECT_FILE"}];
    games[role] = {gameId: ref.gameId, status: "PUBLISHED", contentKind: "RPG_MAKER_PROJECT", files: expected.games[role],
      variants: [{coreId: "rpgmaker", providerId: "retrom-runtime", targetId: identity[3], status: "READY",
        dependencySnapshot: {bindings: [{installationId: ref.installationId, filesDigest: sha}]}}]};
  }
  for (const [role, reviewId] of Object.entries(reviewIds)) {
    const identity = reviewRoles[role];
    const standard = ["rpgxpStandardAmbiguous", "rpgvxStandardAmbiguous"].includes(role);
    expected.reviews[role] = [{name: `${role}/Game.ini`, sha256: sha, sizeBytes: 10}];
    reviews[role] = {itemId: reviewId, version: 1, sourceFiles: expected.reviews[role],
      canApprove: identity[2] === "ready" || standard, rpgMaker: {
        generation: identity[1], selectedCoreId: "rpgmaker", selfContainedOverride: false,
        selfContained: role.endsWith("SelfContained"),
        runtimePackSelections: standard ? [{slot: 1, installationId: role.startsWith("rpgxp") ? id(1) : id(2)}] : [],
        runtimePackRequirements: identity[2] === "ready" ? [] : [{slot: 1, declaredName: {
          rpg2000Missing: "RPG2000_RTP", rpg2003Missing: "RPG2003_RTP", rpgxpStandardAmbiguous: "Standard",
          rpgvxStandardAmbiguous: "RPGVX", rpgvxaceStandardAmbiguous: "RPGVXAce", rpgxpCustom: "RetromCustomXP",
          rpgvxCustom: "RetromCustomVX", rpgvxaceCustom: "RetromCustomVXAce",
        }[role]}],
      }};
  }
  return {request, expected, catalog, games, reviews,
    save: {saveStateId: id(5), gameId: id(4), availability: {status: "AVAILABLE"}},
    history: {importItemId: id(6), eventType: "APPROVED", after: {gameId: id(3)}},
    population: {games: [row(id(3)), row(id(4)), row(id(30))], saves: [row(id(5))],
      reviews: Object.values(reviewIds).map(row)}};
}

test("explicit partial provision reuses all exact referenced objects and retains both protection boundaries", () => {
  const data = fixture(), original = structuredClone(data);
  assert.deepEqual(validateResumeRequest(data.request), data.request);
  const result = validateResumeState(data);
  assert.deepEqual(result.populationBefore, data.request.previousEvidence.populationBefore);
  assert.deepEqual(result.protectedPopulation, data.population);
  assert.deepEqual(result.protectedReferences, data.request.protectedReferences);
  assert.deepEqual(data, original);
});

test("partial continuation fails closed on missing roles, source drift, foreign references or changed protected rows", () => {
  for (const mutate of [
    (d) => {delete d.request.reviewIds.rpgxpCustom;},
    (d) => {d.request.protectedReferences.publishedVariant.gameId = id(31);},
    (d) => {d.request.installations.publishedVariant = id(32);},
    (d) => {d.catalog.installations[0].references.gameCount = 2;},
    (d) => {d.catalog.installations[1].references.checkpointCount = 0;},
    (d) => {d.games.publishedVariant.files[0].sha256 = "f".repeat(64); d.expected.games.publishedVariant = [{...d.games.publishedVariant.files[0], sha256: sha}];},
    (d) => {d.games.publishedVariant.variants[0].dependencySnapshot.bindings[0].installationId = id(32);},
    (d) => {d.reviews.rpgxpNoRtp.sourceFiles = [];},
    (d) => {d.reviews.rpgxpCustom.canApprove = true;},
    (d) => {d.population.games[2].sha256 = "f".repeat(64);},
    (d) => {d.save.gameId = id(3);},
    (d) => {d.history.eventType = "DISCARDED";},
    (d) => {d.request.previousEvidence.resume.installations.publishedVariant.sourceSha256 = "f".repeat(64);},
  ]) {
    const data = fixture(); mutate(data);
    assert.throws(() => validateResumeState(data), /RPG_009_|RPG_ACCEPTANCE_/);
  }
});
