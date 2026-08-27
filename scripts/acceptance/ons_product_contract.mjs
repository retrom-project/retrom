const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export const onsProductStages = [
  "imported", "preview-visible", "preview-captured", "published", "product-input",
  "checkpoint-created", "different-launch-restored", "post-restore-input",
];

export function assertOnsProductEvidence(value) {
  if (!exactRecord(value, ["browser", "caseId", "checkpoint", "ids", "schemaVersion", "screenshots", "stages", "status"]) ||
      value.schemaVersion !== 1 || value.caseId !== "ACC-ONS-001" || value.status !== "PASS" ||
      JSON.stringify(value.stages) !== JSON.stringify(onsProductStages)) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
  assertIds(value.ids);
  assertCheckpoint(value.checkpoint);
  assertBrowser(value.browser);
  assertScreenshots(value.screenshots);
}

function assertIds(value) {
  const keys = ["gameId", "importItemId", "originalLaunchId", "restoreLaunchId", "saveStateId"];
  if (!exactRecord(value, keys) || !keys.every((key) => uuidPattern.test(value[key])) ||
      value.originalLaunchId === value.restoreLaunchId) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCheckpoint(value) {
  if (!exactRecord(value, ["payloadKind", "sizeBytes"]) || value.payloadKind !== "ONS_SAVE_BUNDLE_V1" ||
      !Number.isSafeInteger(value.sizeBytes) || value.sizeBytes < 1 || value.sizeBytes > 64 * 1024 * 1024) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertBrowser(value) {
  if (!exactRecord(value, ["consoleErrorCount", "dialogCount", "pageErrorCount"]) ||
      !Object.values(value).every((count) => Number.isSafeInteger(count) && count === 0)) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertScreenshots(value) {
  const keys = ["postRestoreInput", "preview", "productAfterInput", "productBeforeInput", "restored"];
  if (!exactRecord(value, keys)) {throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");}
  for (const key of keys) {assertScreenshot(value[key]);}
  if (value.productBeforeInput.rgbaSha256 === value.productAfterInput.rgbaSha256 ||
      value.restored.rgbaSha256 === value.postRestoreInput.rgbaSha256) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertScreenshot(value) {
  if (!exactRecord(value, ["height", "nonBlackPixels", "rgbaSha256", "width"]) ||
      !Number.isSafeInteger(value.width) || value.width < 64 || !Number.isSafeInteger(value.height) || value.height < 64 ||
      !Number.isSafeInteger(value.nonBlackPixels) || value.nonBlackPixels < value.width * value.height / 1000 ||
      !/^[0-9a-f]{64}$/.test(value.rgbaSha256)) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function exactRecord(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
