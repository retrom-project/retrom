import { createHash } from "node:crypto";
import {
  existsSync, lstatSync, readFileSync, readdirSync, realpathSync, statSync, writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";

export const uploadRoles = {
  rpg2000Rtp: ["RPG2000_RTP", null, null],
  rpg2003Rtp: ["RPG2003_RTP", null, null],
  rgss1StandardV1: ["RGSS1_RTP_STANDARD", null, null],
  rgss1StandardV2: ["RGSS1_RTP_STANDARD", null, null],
  rgss1Custom: ["RGSS_CUSTOM_RTP", "RPGXP", "RetromCustomXP"],
  rgss2StandardV1: ["RGSS2_RTP_RPGVX", null, null],
  rgss2StandardV2: ["RGSS2_RTP_RPGVX", null, null],
  rgss2Custom: ["RGSS_CUSTOM_RTP", "RPGVX", "RetromCustomVX"],
  rgss3StandardV1: ["RGSS3_RTP_RPGVXAce", null, null],
  rgss3StandardV2: ["RGSS3_RTP_RPGVXAce", null, null],
  rgss3Custom: ["RGSS_CUSTOM_RTP", "RPGVXACE", "RetromCustomVXAce"],
  zeroReference: ["RGSS_CUSTOM_RTP", "RPGXP", "RetromZeroReference"],
};

export const reviewRoles = {
  rpg2000SelfContained: ["rpgmaker_2000", "RPG2000", "ready"],
  rpg2000Missing: ["rpgmaker_2000", "RPG2000", "missing"],
  rpg2003SelfContained: ["rpgmaker_2003", "RPG2003", "ready"],
  rpg2003Missing: ["rpgmaker_2003", "RPG2003", "missing"],
  rpgxpNoRtp: ["rpgmaker_xp", "RPGXP", "ready"],
  rpgxpStandardAmbiguous: ["rpgmaker_xp", "RPGXP", "unselected"],
  rpgxpCustom: ["rpgmaker_xp", "RPGXP", "missing"],
  rpgvxNoRtp: ["rpgmaker_vx", "RPGVX", "ready"],
  rpgvxStandardAmbiguous: ["rpgmaker_vx", "RPGVX", "unselected"],
  rpgvxCustom: ["rpgmaker_vx", "RPGVX", "missing"],
  rpgvxaceNoRtp: ["rpgmaker_vx_ace", "RPGVXACE", "ready"],
  rpgvxaceStandardAmbiguous: ["rpgmaker_vx_ace", "RPGVXACE", "unselected"],
  rpgvxaceCustom: ["rpgmaker_vx_ace", "RPGVXACE", "missing"],
};

export const protectedRoles = {
  publishedVariant: ["RGSS1_RTP_STANDARD", null, null, "rpgmaker_xp", "RPGXP", "rgss1_standard"],
  restorableCheckpoint: ["RGSS2_RTP_RPGVX", null, null, "rpgmaker_vx", "RPGVX", "rgss2_rpgvx"],
};

const sourceNote = "Retrom-owned ACC-RPG-009 deterministic fixture; no vendor RTP bytes";
const inputKeys = new Set([
  "sourcePath", "sourceType", "kind", "generation", "declaredName", "sourceNote",
  "sourceFileCount", "sourceSizeBytes", "sourceSha256",
]);
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export function loadGeneratorInputs(path) {
  const manifestPath = exactFile(path, "RPG_009_PROVISION_INPUT_MANIFEST_INVALID");
  const root = dirname(manifestPath);
  const payload = JSON.parse(readFileSync(manifestPath, "utf8"));
  const expectedKeys = [
    "schemaVersion", "license", "licenseSource", "copyright", "inputs",
    "reviewProjects", "protectedPackInputs", "protectedProjects",
  ];
  exactKeys(payload, expectedKeys, "RPG_009_PROVISION_INPUT_SCHEMA_INVALID");
  if (payload.schemaVersion !== 1 || payload.license !== "MIT"
      || payload.licenseSource !== "testdata/public-roms/rpgmaker-smoke/LICENSE"
      || payload.copyright !== "Copyright (c) 2026 Retrom contributors") {
    throw new Error("RPG_009_PROVISION_INPUT_LICENSE_INVALID");
  }
  validateRows(payload.inputs, uploadRoles, root);
  validateRows(payload.protectedPackInputs, protectedRows(), root);
  validateProjects(payload.reviewProjects, Object.keys(reviewRoles), root);
  validateProjects(payload.protectedProjects, Object.keys(protectedRoles), root);
  return payload;
}

export function buildPlan(inputs, reviewIds, protectedReferences) {
  validateIdentifierMap(reviewIds, Object.keys(reviewRoles), "RPG_009_PROVISION_REVIEW_IDS_INVALID");
  exactKeys(protectedReferences, Object.keys(protectedRoles), "RPG_009_PROVISION_REFERENCES_INVALID");
  validateReference(protectedReferences.publishedVariant, ["installationId", "gameId"]);
  validateReference(protectedReferences.restorableCheckpoint, ["installationId", "gameId", "saveStateId"]);
  const identifiers = [
    ...Object.values(reviewIds), ...Object.values(protectedReferences).flatMap((item) => Object.values(item)),
  ];
  if (new Set(identifiers).size !== identifiers.length) {
    throw new Error("RPG_009_PROVISION_REFERENCES_INVALID");
  }
  return {
    schemaVersion: 2,
    uploads: Object.fromEntries(Object.keys(uploadRoles).map((role) => [role, { ...inputs.inputs[role] }])),
    reviewIds: { ...reviewIds }, protectedReferences,
  };
}

export function writePlan(path, plan) {
  assertPlanTarget(path);
  writeFileSync(path, `${JSON.stringify(plan, null, 2)}\n`, { encoding: "utf8", flag: "wx", mode: 0o600 });
}

export function buildProvisionEvidence(inputs, plan, provenance) {
  return {
    schemaVersion: 1, caseId: "ACC-RPG-009", status: "PROVISIONED",
    generatorInputIdentity: {
      schemaVersion: inputs.schemaVersion,
      license: inputs.license,
      licenseSource: inputs.licenseSource,
      copyright: inputs.copyright,
      inputs: projectRows(inputs.inputs),
      reviewProjects: projectRows(inputs.reviewProjects),
      protectedPackInputs: projectRows(inputs.protectedPackInputs),
      protectedProjects: projectRows(inputs.protectedProjects),
    },
    planIdentity: {
      schemaVersion: plan.schemaVersion,
      uploads: projectRows(plan.uploads),
      reviewIds: plan.reviewIds,
      protectedReferences: plan.protectedReferences,
    },
    counts: {
      protectedInstallationCount: 2,
      protectedGameCount: 2,
      reviewItemCount: Object.keys(plan.reviewIds).length,
      readyUnapprovedReviewCount: Object.values(reviewRoles).filter((item) => item[2] === "ready").length,
    },
    repository: provenance,
  };
}

export function writeProvisionEvidence(path, payload) {
  assertPlanTarget(path);
  writeFileSync(path, `${JSON.stringify(payload, null, 2)}\n`, { encoding: "utf8", flag: "wx", mode: 0o600 });
}

function projectRows(rows) {
  return Object.fromEntries(Object.entries(rows).map(([role, row]) => [
    role, Object.fromEntries(Object.entries(row).filter(([key]) => key !== "sourcePath")),
  ]));
}

export function assertPlanTarget(path) {
  if (!isAbsolute(path) || existsSync(path)) { throw new Error("RPG_009_PROVISION_PLAN_TARGET_INVALID"); }
  const parent = resolve(dirname(path));
  if (!lstatSync(parent).isDirectory() || realpathSync(parent) !== parent) {
    throw new Error("RPG_009_PROVISION_PLAN_TARGET_INVALID");
  }
}

function protectedRows() {
  return Object.fromEntries(Object.entries(protectedRoles).map(([role, values]) => [role, values.slice(0, 3)]));
}

function validateRows(rows, roles, root) {
  exactKeys(rows, Object.keys(roles), "RPG_009_PROVISION_INPUT_ROLES_INVALID");
  for (const [role, identity] of Object.entries(roles)) {
    const row = rows[role];
    exactKeys(row, [...inputKeys], "RPG_009_PROVISION_INPUT_ROW_INVALID");
    if (row.sourceNote !== sourceNote || row.sourceType !== (role === "rpg2000Rtp" ? "DIRECTORY" : "FILES")
        || JSON.stringify([row.kind, row.generation, row.declaredName]) !== JSON.stringify(identity)) {
      throw new Error("RPG_009_PROVISION_INPUT_ROLE_INVALID");
    }
    const source = exactSource(row.sourcePath, row.sourceType, root);
    const observed = sourceIdentity(source, row.sourceType);
    if (JSON.stringify(observed) !== JSON.stringify([
      row.sourceFileCount, row.sourceSizeBytes, row.sourceSha256,
    ])) { throw new Error("RPG_009_PROVISION_INPUT_IDENTITY_INVALID"); }
  }
}

function validateProjects(rows, roles, root) {
  exactKeys(rows, roles, "RPG_009_PROVISION_PROJECT_ROLES_INVALID");
  for (const role of roles) {
    exactKeys(rows[role], ["sourcePath", "sourceSha256"], "RPG_009_PROVISION_PROJECT_ROW_INVALID");
    const source = exactSource(rows[role].sourcePath, "DIRECTORY", root);
    const [, , digest] = sourceIdentity(source, "DIRECTORY");
    if (digest !== rows[role].sourceSha256) { throw new Error("RPG_009_PROVISION_PROJECT_IDENTITY_INVALID"); }
  }
}

function exactSource(path, type, root) {
  if (!isAbsolute(path)) { throw new Error("RPG_009_PROVISION_SOURCE_INVALID"); }
  const source = resolve(path);
  const relation = relative(root, source);
  if (!relation || relation === ".." || relation.startsWith(`..${sep}`)
      || realpathSync(source) !== source || lstatSync(source).isSymbolicLink()
      || (type === "DIRECTORY" ? !statSync(source).isDirectory() : !statSync(source).isFile())) {
    throw new Error("RPG_009_PROVISION_SOURCE_INVALID");
  }
  return source;
}

function sourceIdentity(source, type) {
  if (type === "FILES") {
    const contents = readFileSync(source);
    return [1, contents.length, createHash("sha256").update(contents).digest("hex")];
  }
  const files = [];
  walk(source, source, files);
  const digest = createHash("sha256").update("RETROM_ACC_RPG_009_INPUT_V1\0");
  let total = 0;
  for (const file of files.sort((left, right) => left.logical.localeCompare(right.logical))) {
    const contents = readFileSync(file.path);
    const logical = Buffer.from(file.logical, "utf8");
    const length = Buffer.alloc(4); length.writeUInt32BE(logical.length);
    const size = Buffer.alloc(8); size.writeBigUInt64BE(BigInt(contents.length));
    digest.update(length).update(logical).update(createHash("sha256").update(contents).digest()).update(size);
    total += contents.length;
  }
  return [files.length, total, digest.digest("hex")];
}

function walk(root, directory, files) {
  for (const name of readdirSync(directory)) {
    const path = resolve(directory, name);
    const info = lstatSync(path);
    if (info.isSymbolicLink()) { throw new Error("RPG_009_PROVISION_SOURCE_SYMLINK"); }
    if (info.isDirectory()) { walk(root, path, files); }
    else if (info.isFile()) { files.push({ path, logical: relative(root, path).split(sep).join("/") }); }
  }
}

function validateIdentifierMap(value, roles, code) {
  exactKeys(value, roles, code);
  const identifiers = Object.values(value);
  if (new Set(identifiers).size !== identifiers.length || identifiers.some((item) => !uuidPattern.test(item))) {
    throw new Error(code);
  }
}

function validateReference(value, keys) {
  exactKeys(value, keys, "RPG_009_PROVISION_REFERENCES_INVALID");
  if (Object.values(value).some((item) => !uuidPattern.test(item))) {
    throw new Error("RPG_009_PROVISION_REFERENCES_INVALID");
  }
}

function exactFile(path, code) {
  if (!isAbsolute(path)) { throw new Error(code); }
  const absolute = resolve(path);
  if (!statSync(absolute).isFile() || lstatSync(absolute).isSymbolicLink() || realpathSync(absolute) !== absolute) {
    throw new Error(code);
  }
  return absolute;
}

function exactKeys(value, keys, code) {
  if (!value || Array.isArray(value) || typeof value !== "object"
      || JSON.stringify(Object.keys(value).sort()) !== JSON.stringify([...keys].sort())) {
    throw new Error(code);
  }
}
