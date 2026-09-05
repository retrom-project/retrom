import {test} from "node:test";
import assert from "node:assert/strict";
import {validateResumeRequest, validateResumeState} from "./rpgmaker_pack_resume.mjs";

const id = (index) => `01980000-0000-7000-8000-${String(index).padStart(12, "0")}`;
const roles = ["publishedVariant", "restorableCheckpoint"];

function fixture() {
  const request = {schemaVersion: 1, installations: {publishedVariant: id(1), restorableCheckpoint: id(2)}, reviewId: id(3)};
  const expected = {packs: {}, sourceFiles: [{name: "publishedVariant/Game.ini", sha256: "a".repeat(64), sizeBytes: 20}]};
  const installations = roles.map((role, index) => {
    expected.packs[role] = {definitionId: index ? "rgss2_rpgvx" : "rgss1_standard", filesDigest: String(index).repeat(64),
      fileCount: 1, totalBytes: 121, sourceNote: "Retrom-owned fixture"};
    return {...expected.packs[role], installationId: request.installations[role], status: "READY",
      references: {gameCount: 0, checkpointCount: 0}};
  });
  const review = {itemId: request.reviewId, version: 2, metadata: {title: "publishedVariant"},
    canApprove: true, validation: {current: true, status: "READY"}, sourceFiles: expected.sourceFiles,
    rpgMaker: {selectedCoreId: "rpgmaker", generation: "RPGXP", selfContainedOverride: false,
      runtimePackRequirements: [{slot: 1, declaredName: "Standard"}],
      runtimePackSelections: [{slot: 1, installationId: id(1)}]}};
  const population = {games: [{id: id(10), sha256: "b".repeat(64)}], saves: [],
    reviews: [{id: id(3), sha256: "c".repeat(64)}, {id: id(11), sha256: "d".repeat(64)}]};
  return {request, expected, catalog: {installations}, review, population};
}

test("resume accepts only two named, distinct installations and one explicitly named review", () => {
  const {request} = fixture();
  assert.deepEqual(validateResumeRequest(request), request);
  for (const mutation of [
    (value) => {value.schemaVersion = 2;}, (value) => {value.deleteExisting = true;},
    (value) => {value.installations.restorableCheckpoint = value.reviewId;},
    (value) => {delete value.reviewId;}, (value) => {value.reviewId = "not-an-id";},
  ]) {
    const value = structuredClone(request); mutation(value);
    assert.throws(() => validateResumeRequest(value), /RESUME_REQUEST_INVALID/);
  }
});

test("resume protects every other pre-existing row without mutating the original snapshot", () => {
  const data = fixture(); const original = structuredClone(data);
  const result = validateResumeState(data);
  assert.deepEqual(result.populationBefore, {...data.population, reviews: [data.population.reviews[1]]});
  assert.deepEqual(result.resumedReviewRow, data.population.reviews[0]);
  assert.equal(result.installations.publishedVariant.installationId, id(1));
  assert.deepEqual(data, original);
});

test("resume refuses changed bytes, foreign records, references, selections and missing pending review", () => {
  for (const mutate of [
    (data) => {data.catalog.installations.push({...data.catalog.installations[0], installationId: id(4)});},
    (data) => {data.catalog.installations[0].filesDigest = "f".repeat(64);},
    (data) => {data.catalog.installations[0].definitionId = "rgss3_rpgvxace";},
    (data) => {data.catalog.installations[1].references.gameCount = 1;},
    (data) => {delete data.catalog.installations[1].references.checkpointCount;},
    (data) => {data.catalog.installations[0].status = "DELETED";},
    (data) => {data.review.sourceFiles = [{...data.review.sourceFiles[0], sha256: "f".repeat(64)}];},
    (data) => {data.review.rpgMaker.runtimePackSelections[0].installationId = id(2);},
    (data) => {data.review.validation.current = false;},
    (data) => {data.population.reviews.shift();},
  ]) {
    const data = fixture(); mutate(data);
    assert.throws(() => validateResumeState(data), /RESUME_STATE_INVALID/);
  }
});
