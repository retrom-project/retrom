const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digestPattern = /^[0-9a-f]{64}$/u;

export const tyranoScriptProductStages = [
  "imported", "preview-visible", "preview-captured", "published", "gamepad-input",
  "checkpoint-b-created", "marker-c-recorded", "different-launch-restored-b", "post-restore-gamepad-input",
];

export function assertTyranoScriptProductEvidence(value) {
  if (!exactRecord(value, [
    "browser", "caseId", "checkpoint", "ids", "resources", "schemaVersion", "screenshots", "stages",
    "state", "status",
  ]) || value.schemaVersion !== 1 || value.caseId !== "ACC-TYRANOSCRIPT-001" || value.status !== "PASS" ||
      JSON.stringify(value.stages) !== JSON.stringify(tyranoScriptProductStages)) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_EVIDENCE_INVALID");
  }
  assertIds(value.ids);
  assertCheckpoint(value.checkpoint);
  assertState(value.state);
  assertResources(value.resources);
  assertBrowser(value.browser);
  assertScreenshots(value.screenshots);
}

function assertIds(value) {
  const keys = ["gameId", "importItemId", "originalLaunchId", "restoreLaunchId", "saveStateId"];
  if (!exactRecord(value, keys) || !keys.every((key) => uuidPattern.test(value[key])) ||
      value.originalLaunchId === value.restoreLaunchId) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertCheckpoint(value) {
  if (!exactRecord(value, ["format", "sizeBytes"]) || value.format !== "tyranoscript-snapshot-v1" ||
      !Number.isSafeInteger(value.sizeBytes) || value.sizeBytes < 1 || value.sizeBytes > 32 * 1024 * 1024) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_EVIDENCE_INVALID");
  }
}

function assertState(value) {
  if (!exactRecord(value, ["b", "c", "restoredB"]) || !engineState(value.b, "B") ||
      !engineState(value.c, "C") || !engineState(value.restoredB, "B") ||
      value.b.scenario !== value.restoredB.scenario || value.b.order !== value.restoredB.order) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_STATE_INVALID");
  }
}

function engineState(value, marker) {
  return exactRecord(value, ["marker", "order", "scenario"]) && value.marker === marker &&
    typeof value.scenario === "string" && value.scenario.length > 0 && value.scenario.length <= 500 &&
    Number.isSafeInteger(value.order) && value.order >= 0;
}

function assertResources(value) {
  if (!exactRecord(value, ["contentDigest", "engineAsset200Count", "failedResponseCount"]) ||
      !digestPattern.test(value.contentDigest) || value.engineAsset200Count < 1 ||
      !Number.isSafeInteger(value.engineAsset200Count) || value.failedResponseCount !== 0) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_RESOURCE_INVALID");
  }
}

function assertBrowser(value) {
  if (!exactRecord(value, ["consoleErrorCount", "dialogCount", "ignoredSandboxAlertCount", "pageErrorCount"]) ||
      value.consoleErrorCount !== 0 || value.dialogCount !== 0 || value.pageErrorCount !== 0 ||
      !Number.isSafeInteger(value.ignoredSandboxAlertCount) || value.ignoredSandboxAlertCount < 0) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_BROWSER_INVALID");
  }
}

function assertScreenshots(value) {
  if (!exactRecord(value, ["preview", "product", "restored"])) {
    throw new Error("TYRANOSCRIPT_ACCEPTANCE_SCREENSHOT_INVALID");
  }
  for (const screenshot of Object.values(value)) {
    if (!exactRecord(screenshot, ["height", "nonBlackPixels", "pngSha256", "width"]) ||
        !Number.isSafeInteger(screenshot.width) || screenshot.width < 64 ||
        !Number.isSafeInteger(screenshot.height) || screenshot.height < 64 ||
        !Number.isSafeInteger(screenshot.nonBlackPixels) ||
        screenshot.nonBlackPixels < screenshot.width * screenshot.height / 1000 ||
        !digestPattern.test(screenshot.pngSha256)) {
      throw new Error("TYRANOSCRIPT_ACCEPTANCE_SCREENSHOT_INVALID");
    }
  }
}

function exactRecord(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
