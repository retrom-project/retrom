const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export const kirikiriProductStages = [
  "imported", "preview-visible", "standard-gamepad-control", "preview-captured", "published", "product-a-to-b",
  "immersive-exit-menu", "checkpoint-at-b", "product-c", "different-launch-restored-to-b", "post-restore-input",
];

export function assertKiriKiriProductEvidence(value) {
  if (!exactRecord(value, ["browser", "caseId", "checkpoint", "ids", "immersiveMenu", "schemaVersion", "screenshots", "stages", "status"]) ||
      value.schemaVersion !== 1 || value.caseId !== "ACC-KIRIKIRI-001" || value.status !== "PASS" ||
      JSON.stringify(value.stages) !== JSON.stringify(kirikiriProductStages)) {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
  assertIds(value.ids);
  assertCheckpoint(value.checkpoint);
  assertBrowser(value.browser);
  assertImmersiveMenu(value.immersiveMenu);
  assertScreenshots(value.screenshots);
}

function assertIds(value) {
  const keys = ["gameId", "immersiveLaunchId", "importItemId", "originalLaunchId", "restoreLaunchId", "saveStateId"];
  if (!exactRecord(value, keys) || !keys.every((key) => uuidPattern.test(value[key])) ||
      new Set([value.immersiveLaunchId, value.originalLaunchId, value.restoreLaunchId]).size !== 3) {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertImmersiveMenu(value) {
  if (!exactRecord(value, ["actions", "screenshot"]) ||
      JSON.stringify(value.actions) !== JSON.stringify(["取消", "创建存档", "退出游戏"]) ||
      value.screenshot !== "screenshots/immersive-exit-menu.png") {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCheckpoint(value) {
  if (!exactRecord(value, ["payloadKind", "sizeBytes"]) || value.payloadKind !== "KIRIKIRI_SAVE_BUNDLE_V1" ||
      !Number.isSafeInteger(value.sizeBytes) || value.sizeBytes < 1 || value.sizeBytes > 64 * 1024 * 1024) {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertBrowser(value) {
  if (!exactRecord(value, ["consoleErrorCount", "dialogCount", "pageErrorCount"]) ||
      !Object.values(value).every((count) => Number.isSafeInteger(count) && count === 0)) {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertScreenshots(value) {
  const keys = [
    "postRestoreInput", "preview", "productAfterCheckpoint", "productAfterInput", "productBeforeInput", "restored",
  ];
  if (!exactRecord(value, keys)) {throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");}
  for (const key of keys) {assertScreenshot(value[key]);}
  if (value.productBeforeInput.rgbaSha256 === value.productAfterInput.rgbaSha256 ||
      value.productAfterInput.rgbaSha256 === value.productAfterCheckpoint.rgbaSha256 ||
      value.restored.rgbaSha256 !== value.productAfterInput.rgbaSha256 ||
      value.restored.rgbaSha256 === value.postRestoreInput.rgbaSha256) {
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
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
    throw new Error("KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function exactRecord(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
