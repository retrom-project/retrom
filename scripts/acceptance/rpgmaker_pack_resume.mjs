import {createHash} from "node:crypto";
import {execFileSync} from "node:child_process";
import {readFileSync, lstatSync, realpathSync} from "node:fs";
import {basename, isAbsolute} from "node:path";
import {directoryFiles} from "./rpgmaker_security_upload.mjs";
import {readPopulation} from "./rpgmaker_pack_population.mjs";

const roles = ["publishedVariant", "restorableCheckpoint"];
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export function loadResumeRequest(path) {
  if (!isAbsolute(path) || !lstatSync(path).isFile() || realpathSync(path) !== path) {
    throw new Error("RPG_009_RESUME_REQUEST_INVALID");
  }
  return validateResumeRequest(JSON.parse(readFileSync(path, "utf8")));
}

export function validateResumeRequest(value) {
  const identifiers = [...Object.values(value?.installations ?? {}), value?.reviewId];
  if (!keys(value, ["schemaVersion", "installations", "reviewId"]) || value.schemaVersion !== 1 ||
      !keys(value.installations, roles) || identifiers.some((id) => !uuid.test(id)) || new Set(identifiers).size !== 3) {
    throw new Error("RPG_009_RESUME_REQUEST_INVALID");
  }
  return value;
}

export async function captureApprovedResume(client, inputs, request) {
  validateResumeRequest(request);
  const expected = expectedResumeInputs(inputs);
  const [catalog, review, population] = await Promise.all([
    client.json("GET", "/api/v1/admin/runtime-asset-packs"),
    client.json("GET", `/api/v1/admin/reviews/${request.reviewId}`),
    readPopulation(client),
  ]);
  const result = validateResumeState({request, expected, catalog, review, population});
  const resume = {
    schemaVersion: 1, mode: "EXPLICIT_PROTECTED_PREVIEW", capturedAtMs: Date.now(),
    installations: Object.fromEntries(roles.map((role) => [role, {
      installationId: request.installations[role], filesDigest: result.installations[role].filesDigest,
      sourceSha256: inputs.protectedPackInputs[role].sourceSha256,
    }])),
    review: {itemId: review.itemId, version: review.version,
      sourceSha256: inputs.protectedProjects.publishedVariant.sourceSha256, populationRow: result.resumedReviewRow},
  };
  return {...result, reviews: {publishedVariant: review}, resume};
}

export function validateResumeState({request, expected, catalog, review, population}) {
  validateResumeRequest(request);
  const invalid = () => {throw new Error("RPG_009_RESUME_STATE_INVALID");};
  if (!Array.isArray(catalog?.installations) || catalog.installations.length !== 2) {invalid();}
  const installations = {};
  for (const role of roles) {
    const row = catalog.installations.find((item) => item.installationId === request.installations[role]);
    if (!row || row.status !== "READY" || row.references?.gameCount !== 0 || row.references?.checkpointCount !== 0 ||
        Object.entries(expected.packs[role]).some(([key, value]) => row[key] !== value)) {invalid();}
    installations[role] = row;
  }
  validateReview(review, request, expected.sourceFiles, invalid);
  const resumed = population.reviews.filter((row) => row.id === request.reviewId);
  if (resumed.length !== 1) {invalid();}
  return {installations, resumedReviewRow: resumed[0], populationBefore: {
    ...population, reviews: population.reviews.filter((row) => row.id !== request.reviewId),
  }};
}

function validateReview(review, request, sourceFiles, invalid) {
  const rpg = review?.rpgMaker;
  if (review?.itemId !== request.reviewId || !Number.isSafeInteger(review.version) || review.version < 1 ||
      review.metadata?.title !== "publishedVariant" || !review.canApprove || !review.validation?.current ||
      review.validation.status !== "READY" || rpg?.selectedCoreId !== "rpgmaker" || rpg.generation !== "RPGXP" ||
      rpg.selfContainedOverride !== false || rpg.runtimePackRequirements?.length !== 1 ||
      rpg.runtimePackRequirements[0].declaredName !== "Standard" || rpg.runtimePackSelections?.length !== 1 ||
      rpg.runtimePackSelections[0].slot !== rpg.runtimePackRequirements[0].slot ||
      rpg.runtimePackSelections[0].installationId !== request.installations.publishedVariant ||
      !Array.isArray(review.sourceFiles)) {invalid();}
  const observed = review.sourceFiles.map(({name, sha256, sizeBytes}) => ({name, sha256, sizeBytes}))
    .sort((a, b) => a.name.localeCompare(b.name));
  if (JSON.stringify(observed) !== JSON.stringify(sourceFiles)) {invalid();}
}

export function expectedResumeInputs(inputs) {
  // Only the generator's two known one-file fixtures are eligible. This is not
  // an arbitrary archive import or a way to adopt unrelated installed RTPs.
  const packs = Object.fromEntries(roles.map((role, index) => {
    const input = inputs.protectedPackInputs[role];
    const name = `Graphics/Characters/retrom-protected-${index ? "vx" : "xp"}.png`;
    const bytes = execFileSync("7z", ["x", "-so", input.sourcePath, `RGSS${index + 1}/${name}`],
      {maxBuffer: 4096, timeout: 10_000, stdio: ["ignore", "pipe", "pipe"]});
    return [role, {definitionId: input.definitionId, sourceNote: input.sourceNote,
      filesDigest: fixtureFilesDigest(name, bytes), fileCount: 1, totalBytes: bytes.length}];
  }));
  const root = inputs.protectedProjects.publishedVariant.sourcePath;
  const sourceFiles = directoryFiles(root, `${basename(root)}/`).map((file) => ({
    name: file.relativePath, sha256: createHash("sha256").update(readFileSync(file.path)).digest("hex"), sizeBytes: file.sizeBytes,
  }));
  return {packs, sourceFiles};
}

function fixtureFilesDigest(name, bytes) {
  const hash = createHash("sha256").update("RETROM_FILESET_V1\0");
  for (const value of ["PROJECT_FILE", name]) {
    const encoded = Buffer.from(value); const length = Buffer.alloc(4); length.writeUInt32BE(encoded.length);
    hash.update(length).update(encoded);
  }
  const size = Buffer.alloc(8); size.writeBigUInt64BE(BigInt(bytes.length));
  return hash.update(createHash("sha256").update(bytes).digest()).update(size).update(Buffer.of(0)).digest("hex");
}

function keys(value, expected) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join() === [...expected].sort().join();
}
