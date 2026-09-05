const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digestPattern = /^[0-9a-f]{64}$/u;

export const butterscotchProductStages = [
  "imported", "preview-visible", "preview-captured", "published", "gamepad-input",
  "checkpoint-created", "different-launch-restored", "post-restore-input", "project-cache-reused",
];

export function assertButterscotchProductEvidence(value) {
  if (!exactRecord(value, [
    "browser", "cache", "caseId", "checkpoint", "ids", "schemaVersion", "screenshots", "stages", "status",
  ]) || value.schemaVersion !== 1 || value.caseId !== "ACC-BUTTERSCOTCH-001" || value.status !== "PASS" ||
      JSON.stringify(value.stages) !== JSON.stringify(butterscotchProductStages)) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
  assertIds(value.ids);
  assertCheckpoint(value.checkpoint);
  assertBrowser(value.browser);
  assertCache(value.cache);
  assertScreenshots(value.screenshots);
}

function assertIds(value) {
  const keys = ["gameId", "importItemId", "originalLaunchId", "restoreLaunchId", "saveStateId"];
  if (!exactRecord(value, keys) || !keys.every((key) => uuidPattern.test(value[key])) ||
      value.originalLaunchId === value.restoreLaunchId) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCheckpoint(value) {
  if (!exactRecord(value, ["format", "sizeBytes"]) || value.format !== "butterscotch-checkpoint-v2" ||
      !Number.isSafeInteger(value.sizeBytes) || value.sizeBytes < 13 || value.sizeBytes > 16 * 1024 * 1024) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertBrowser(value) {
  if (!exactRecord(value, ["consoleErrorCount", "dialogCount", "pageErrorCount"]) ||
      !Object.values(value).every((count) => Number.isSafeInteger(count) && count === 0)) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCache(value) {
  if (!exactRecord(value, [
    "contentDigest", "firstDataWinResponseCount", "restoreDataWinResponseCount", "restoreIndexResponseCount",
  ]) || !digestPattern.test(value.contentDigest) || !Number.isSafeInteger(value.firstDataWinResponseCount) ||
      value.firstDataWinResponseCount !== 1 || !Number.isSafeInteger(value.restoreDataWinResponseCount) ||
      value.restoreDataWinResponseCount !== 0 || !Number.isSafeInteger(value.restoreIndexResponseCount) ||
      value.restoreIndexResponseCount !== 1) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_CACHE_EVIDENCE_INVALID");
  }
}

function assertScreenshots(value) {
  const keys = ["postRestoreInput", "preview", "productAfterInput", "productBeforeInput", "restored"];
  if (!exactRecord(value, keys)) {throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");}
  for (const key of keys) {assertScreenshot(value[key]);}
  if (value.productBeforeInput.rgbaSha256 === value.productAfterInput.rgbaSha256 ||
      value.restored.rgbaSha256 === value.postRestoreInput.rgbaSha256) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertScreenshot(value) {
  if (!exactRecord(value, [
    "backingHeight", "backingWidth", "centerOffsetXPx", "centerOffsetYPx", "displayHeight", "displayWidth",
    "focused", "height", "nonBlackPixels", "rgbaSha256", "surfaceHeight", "surfaceWidth", "width",
  ]) || !Number.isSafeInteger(value.width) || value.width < 64 ||
      !Number.isSafeInteger(value.height) || value.height < 64 ||
      !Number.isSafeInteger(value.backingWidth) || value.backingWidth < 64 ||
      !Number.isSafeInteger(value.backingHeight) || value.backingHeight < 64 ||
      typeof value.displayWidth !== "number" || value.displayWidth < 64 ||
      typeof value.displayHeight !== "number" || value.displayHeight < 64 ||
      typeof value.surfaceWidth !== "number" || value.surfaceWidth < value.displayWidth ||
      typeof value.surfaceHeight !== "number" || value.surfaceHeight < value.displayHeight ||
      value.surfaceWidth - value.displayWidth > 1 && value.surfaceHeight - value.displayHeight > 1 ||
      Math.abs(value.backingWidth / value.backingHeight - value.displayWidth / value.displayHeight) > 0.01 ||
      typeof value.centerOffsetXPx !== "number" || value.centerOffsetXPx < 0 || value.centerOffsetXPx > 1 ||
      typeof value.centerOffsetYPx !== "number" || value.centerOffsetYPx < 0 || value.centerOffsetYPx > 1 ||
      value.focused !== true || !Number.isSafeInteger(value.nonBlackPixels) ||
      value.nonBlackPixels < value.width * value.height / 1000 || !digestPattern.test(value.rgbaSha256)) {
    throw new Error("BUTTERSCOTCH_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function exactRecord(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
