const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export const onsProductStages = [
  "imported", "preview-visible", "preview-captured", "published", "product-input",
  "checkpoint-created", "different-launch-restored", "post-restore-input",
];

export function assertOnsProductEvidence(value) {
  if (!exactRecord(value, ["browser", "caseId", "checkpoint", "ids", "loading", "schemaVersion", "screenshots", "stages", "status"]) ||
      value.schemaVersion !== 1 || value.caseId !== "ACC-ONS-001" || value.status !== "PASS" ||
      JSON.stringify(value.stages) !== JSON.stringify(onsProductStages)) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
  assertIds(value.ids);
  assertCheckpoint(value.checkpoint);
  assertBrowser(value.browser);
  assertLoading(value.loading);
  assertScreenshots(value.screenshots);
}

function assertLoading(value) {
  if (!exactRecord(value, ["firstVisible", "restoreVisible", "sameProjectContentIdentity", "schemaVersion"]) ||
      value.schemaVersion !== 1 || value.sameProjectContentIdentity !== true) {
    throw new Error("ONS_ACCEPTANCE_LOADING_EVIDENCE_INVALID");
  }
  assertLoadingSnapshot(value.firstVisible, false);
  assertLoadingSnapshot(value.restoreVisible, true);
}

function assertLoadingSnapshot(value, requireCacheHit) {
  const keys = [
    "declaredLargeFileCount", "declaredProjectBytes", "declaredProjectFileCount",
    "fullProjectFileResponseCount", "nativeProjectResponseCount", "projectContentIdentityCount",
    "rangeProjectFileResponseCount", "requestedLargeFileCount", "requestedProjectBytes",
    "requestedProjectFileCount", "runtimeAssetCacheHitCount", "runtimeAssetRequestCount",
    "runtimeAssetTransferredBytes",
  ];
  if (!exactRecord(value, keys) || keys.some((key) => !Number.isSafeInteger(value[key]) || value[key] < 0) ||
      value.projectContentIdentityCount !== 1 || value.nativeProjectResponseCount !== 0 ||
      value.declaredProjectFileCount < 2 || value.declaredProjectBytes < 1 ||
      value.declaredLargeFileCount < 1 || value.requestedProjectFileCount < 1 ||
      value.requestedProjectFileCount >= value.declaredProjectFileCount ||
      value.requestedProjectBytes < 1 || value.requestedProjectBytes >= value.declaredProjectBytes ||
      value.requestedLargeFileCount >= value.declaredLargeFileCount ||
      value.runtimeAssetRequestCount < 2 || requireCacheHit && value.runtimeAssetCacheHitCount < 1) {
    throw new Error("ONS_ACCEPTANCE_LOADING_EVIDENCE_INVALID");
  }
}

function assertIds(value) {
  const keys = ["gameId", "importItemId", "originalLaunchId", "restoreLaunchId", "saveStateId"];
  if (!exactRecord(value, keys) || !keys.every((key) => uuidPattern.test(value[key])) ||
      value.originalLaunchId === value.restoreLaunchId) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCheckpoint(value) {
  if (!exactRecord(value, ["format", "sizeBytes"]) || value.format !== "ons-save-bundle-v1" ||
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
  if (!exactRecord(value, [
    "backingHeight", "backingWidth", "centerOffsetXPx", "centerOffsetYPx", "displayHeight", "displayWidth",
    "focused", "height", "nonBlackPixels", "rgbaSha256", "width",
  ]) ||
      !Number.isSafeInteger(value.width) || value.width < 64 || !Number.isSafeInteger(value.height) || value.height < 64 ||
      !Number.isSafeInteger(value.backingWidth) || value.backingWidth < 1 ||
      !Number.isSafeInteger(value.backingHeight) || value.backingHeight < 1 ||
      value.backingWidth === 300 && value.backingHeight === 150 ||
      typeof value.displayWidth !== "number" || value.displayWidth < 64 ||
      typeof value.displayHeight !== "number" || value.displayHeight < 64 ||
      Math.abs(value.backingWidth / value.backingHeight - value.displayWidth / value.displayHeight) > 0.01 ||
      typeof value.centerOffsetXPx !== "number" || value.centerOffsetXPx < 0 || value.centerOffsetXPx > 1 ||
      typeof value.centerOffsetYPx !== "number" || value.centerOffsetYPx < 0 || value.centerOffsetYPx > 1 ||
      value.focused !== true ||
      !Number.isSafeInteger(value.nonBlackPixels) || value.nonBlackPixels < value.width * value.height / 1000 ||
      !/^[0-9a-f]{64}$/.test(value.rgbaSha256)) {
    throw new Error("ONS_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function exactRecord(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
