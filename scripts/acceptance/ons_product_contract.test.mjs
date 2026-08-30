import assert from "node:assert/strict";
import test from "node:test";

import { assertOnsProductEvidence, onsProductStages } from "./ons_product_contract.mjs";

test("accepts the minimal ONS import, play, save, restore, and input evidence", () => {
  assert.doesNotThrow(() => assertOnsProductEvidence(evidence()));
});

test("rejects black frames, ineffective input, reused launches, and wrong payloads", () => {
  const mutations = [
    (value) => {value.screenshots.preview.nonBlackPixels = 0;},
    (value) => {value.screenshots.productAfterInput.rgbaSha256 = value.screenshots.productBeforeInput.rgbaSha256;},
    (value) => {value.ids.restoreLaunchId = value.ids.originalLaunchId;},
    (value) => {value.checkpoint.payloadKind = "RUNTIME_STATE";},
    (value) => {value.screenshots.preview.backingWidth = 300; value.screenshots.preview.backingHeight = 150;},
    (value) => {value.screenshots.preview.centerOffsetXPx = 2;},
    (value) => {value.screenshots.preview.focused = false;},
  ];
  for (const mutate of mutations) {
    const value = evidence();
    mutate(value);
    assert.throws(() => assertOnsProductEvidence(value), /ONS_ACCEPTANCE_EVIDENCE_INVALID/);
  }
});

test("rejects missing or eager project loading evidence", () => {
  const missing = evidence();
  delete missing.loading;
  assert.throws(() => assertOnsProductEvidence(missing), /ONS_ACCEPTANCE_EVIDENCE_INVALID/);

  const eager = evidence();
  eager.loading.firstVisible.requestedProjectBytes = eager.loading.firstVisible.declaredProjectBytes;
  assert.throws(() => assertOnsProductEvidence(eager), /ONS_ACCEPTANCE_LOADING_EVIDENCE_INVALID/);
});

function evidence() {
  const screenshot = (digest) => ({
    width: 1280, height: 960, nonBlackPixels: 20_000, rgbaSha256: digest.repeat(64),
    backingWidth: 640, backingHeight: 480, displayWidth: 1280, displayHeight: 960,
    centerOffsetXPx: 0.5, centerOffsetYPx: 0, focused: true,
  });
  return {
    schemaVersion: 1,
    caseId: "ACC-ONS-001",
    status: "PASS",
    stages: [...onsProductStages],
    ids: {
      importItemId: "01a0452e-d583-7a46-ab55-67928e963086",
      gameId: "01a0452f-a9cc-7883-bc20-d554b47224ad",
      saveStateId: "01a0452f-bc6d-7d0a-b975-891ece6a0cc9",
      originalLaunchId: "01a0452f-a9e9-7c54-a65e-c75fc19a473b",
      restoreLaunchId: "01a0452f-bc94-7cc2-8403-4819f3c381e2",
    },
    checkpoint: { payloadKind: "ONS_SAVE_BUNDLE_V1", sizeBytes: 36_194 },
    loading: {
      schemaVersion: 1,
      sameProjectContentIdentity: true,
      firstVisible: loadingSnapshot({ cacheHits: 1, requestedBytes: 2048 }),
      restoreVisible: loadingSnapshot({ cacheHits: 2, requestedBytes: 2048 }),
    },
    screenshots: {
      preview: screenshot("1"), productBeforeInput: screenshot("2"), productAfterInput: screenshot("3"),
      restored: screenshot("4"), postRestoreInput: screenshot("5"),
    },
    browser: { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 },
  };
}

function loadingSnapshot({ cacheHits, requestedBytes }) {
  return {
    declaredLargeFileCount: 2,
    declaredProjectBytes: 20_000_000,
    declaredProjectFileCount: 12,
    fullProjectFileResponseCount: 3,
    nativeProjectResponseCount: 0,
    projectContentIdentityCount: 1,
    rangeProjectFileResponseCount: 0,
    requestedLargeFileCount: 0,
    requestedProjectBytes: requestedBytes,
    requestedProjectFileCount: 3,
    runtimeAssetCacheHitCount: cacheHits,
    runtimeAssetRequestCount: 2,
    runtimeAssetTransferredBytes: 0,
  };
}
