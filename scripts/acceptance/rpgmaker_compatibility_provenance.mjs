import { createHash } from "node:crypto";
import { lstatSync, readFileSync, realpathSync, statSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";


const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const phases = {
  prepare: "RETROM_ACC_RPG_012_PREPARE_EVIDENCE",
  oldProvision: "RETROM_ACC_RPG_012_OLD_PROVISION_EVIDENCE",
  promote: "RETROM_ACC_RPG_012_PROMOTE_EVIDENCE",
  newProvision: "RETROM_ACC_RPG_012_NEW_PROVISION_EVIDENCE",
  drift: "RETROM_ACC_RPG_012_DRIFT_EVIDENCE",
  inspect: "RETROM_ACC_RPG_012_INSPECT_EVIDENCE",
};


export function loadCompatibilityProvisioning(finalState) {
  const evidence = Object.fromEntries(Object.entries(phases).map(([phase, name]) => [phase, readPhase(name)]));
  validateStatePhase(evidence.prepare.payload, "OLD_SELECTED");
  validateProductPhase(evidence.oldProvision.payload, "OLD");
  validateStatePhase(evidence.promote.payload, "NEW_SELECTED");
  validateProductPhase(evidence.newProvision.payload, "NEW");
  validateStatePhase(evidence.drift.payload, "DRIFT_SEEDED");
  validateStatePhase(evidence.inspect.payload, "DRIFT_SEEDED");
  assertRelations(evidence, finalState);
  return { schemaVersion: 1, phases: evidence };
}


function readPhase(name) {
  const value = process.env[name];
  if (!value || !isAbsolute(value)) { throw new Error(`RPG_012_PROVENANCE_ENV_INVALID_${name}`); }
  const path = resolve(value);
  if (!statSync(path).isFile() || lstatSync(path).isSymbolicLink() || realpathSync(path) !== path) {
    throw new Error(`RPG_012_PROVENANCE_FILE_INVALID_${name}`);
  }
  const contents = readFileSync(path);
  const payload = JSON.parse(contents.toString("utf8"));
  rejectSensitiveKeys(payload);
  return { documentSha256: createHash("sha256").update(contents).digest("hex"), payload };
}


function validateStatePhase(value, phase) {
  const keys = [
    "schemaVersion", "caseId", "phase", "databasePathSha256", "oldArtifact", "newArtifact",
    "oldCheckpoint", "newVariant", "driftSaveStateIds", "updatedAtMs",
  ];
  if (!exactKeys(value, keys) || value.schemaVersion !== 1 || value.caseId !== "ACC-RPG-012"
      || value.phase !== phase || !/^[0-9a-f]{64}$/.test(value.databasePathSha256)) {
    throw new Error(`RPG_012_PROVENANCE_STATE_INVALID_${phase}`);
  }
}


function validateProductPhase(value, phase) {
  const common = ["schemaVersion", "caseId", "phase", "importItemId", "validationId", "routeKey", "gameId", "repository"];
  const keys = phase === "OLD" ? [...common, "saveStateId"] : common;
  if (!exactKeys(value, keys) || value.schemaVersion !== 1 || value.caseId !== "ACC-RPG-012"
      || value.phase !== phase || [value.importItemId, value.validationId, value.gameId]
        .some((item) => !uuidPattern.test(item)) || (phase === "OLD" && !uuidPattern.test(value.saveStateId))) {
    throw new Error(`RPG_012_PROVENANCE_PRODUCT_INVALID_${phase}`);
  }
  validateRepository(value.repository);
}


function assertRelations(evidence, finalState) {
  if (JSON.stringify(evidence.drift.payload) !== JSON.stringify(finalState)
      || JSON.stringify(evidence.inspect.payload) !== JSON.stringify(finalState)
      || evidence.prepare.payload.oldArtifact.id !== finalState.oldArtifact.id
      || evidence.prepare.payload.newArtifact.id !== finalState.newArtifact.id
      || evidence.oldProvision.payload.routeKey !== finalState.oldArtifact.routeKey
      || evidence.oldProvision.payload.gameId !== finalState.oldCheckpoint.gameId
      || evidence.oldProvision.payload.saveStateId !== finalState.oldCheckpoint.saveStateId
      || JSON.stringify(evidence.promote.payload.oldCheckpoint) !== JSON.stringify(finalState.oldCheckpoint)
      || evidence.newProvision.payload.routeKey !== finalState.newArtifact.routeKey
      || evidence.newProvision.payload.gameId !== finalState.newVariant.gameId) {
    throw new Error("RPG_012_PROVENANCE_PHASE_RELATION_INVALID");
  }
  if (evidence.prepare.payload.oldCheckpoint !== null || evidence.prepare.payload.newVariant !== null
      || evidence.prepare.payload.driftSaveStateIds !== null || evidence.promote.payload.newVariant !== null
      || evidence.promote.payload.driftSaveStateIds !== null) {
    throw new Error("RPG_012_PROVENANCE_PHASE_ORDER_INVALID");
  }
}


function validateRepository(value) {
  const summary = value?.gitDirtySummary;
  if (!exactKeys(value, ["gitCommit", "gitDirty", "gitDirtySummary"])
      || !/^(?:[0-9a-f]{40}|UNBORN)$/.test(value.gitCommit)
      || !exactKeys(summary, ["fileCount", "sha256", "entries"])
      || !/^[0-9a-f]{64}$/.test(summary.sha256) || summary.fileCount !== summary.entries.length
      || value.gitDirty !== (summary.entries.length > 0)
      || summary.entries.some((item) => !exactKeys(item, ["status", "path"]) || isAbsolute(item.path))) {
    throw new Error("RPG_012_PROVENANCE_REPOSITORY_INVALID");
  }
  const digest = createHash("sha256").update(JSON.stringify(summary.entries)).digest("hex");
  if (digest !== summary.sha256) { throw new Error("RPG_012_PROVENANCE_REPOSITORY_INVALID"); }
}


function rejectSensitiveKeys(value) {
  const forbidden = new Set(["sourcePath", "databasePath", "statePath", "password", "csrfToken", "capability", "cookie"]);
  const stack = [value];
  while (stack.length) {
    const item = stack.pop();
    if (Array.isArray(item)) { stack.push(...item); }
    else if (item && typeof item === "object") {
      if (Object.keys(item).some((key) => forbidden.has(key))) {
        throw new Error("RPG_012_PROVENANCE_SENSITIVE_FIELD");
      }
      stack.push(...Object.values(item));
    }
  }
}


function exactKeys(value, keys) {
  return value && !Array.isArray(value) && typeof value === "object"
    && JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...keys].sort());
}
