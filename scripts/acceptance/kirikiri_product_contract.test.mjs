import assert from "node:assert/strict";
import test from "node:test";

import { assertKiriKiriProductEvidence, kirikiriProductStages } from "./kirikiri_product_contract.mjs";

test("accepts Range-backed KiriKiri loading with cached runtime assets on restore", () => {
  assert.doesNotThrow(() => assertKiriKiriProductEvidence(evidence()));
});

test("rejects eager XP3 responses and missing restore cache reuse", () => {
  const eager = evidence();
  eager.loading.firstVisible.fullProjectFileResponseCount = 1;
  assert.throws(() => assertKiriKiriProductEvidence(eager), /KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID/u);

  const uncached = evidence();
  uncached.loading.restoreVisible.runtimeAssetCacheHitCount = 0;
  assert.throws(() => assertKiriKiriProductEvidence(uncached), /KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID/u);
});

test("rejects missing stable project identity and non-Range project access", () => {
  const unstable = evidence();
  unstable.loading.sameProjectContentIdentity = false;
  assert.throws(() => assertKiriKiriProductEvidence(unstable), /KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID/u);

  const nonRange = evidence();
  nonRange.loading.firstVisible.rangeProjectFileResponseCount = 0;
  assert.throws(() => assertKiriKiriProductEvidence(nonRange), /KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID/u);
});

test("accepts a discriminative B restore without requiring an exact full-frame hash", () => {
  const restored = evidence();
  restored.screenshots.restored.rgbaSha256 = "6".repeat(64);
  assert.doesNotThrow(() => assertKiriKiriProductEvidence(restored));

  restored.restoreComparison.matched = false;
  assert.throws(() => assertKiriKiriProductEvidence(restored), /KIRIKIRI_ACCEPTANCE_EVIDENCE_INVALID/u);
});

function evidence() {
  const screenshot = (digest) => ({
    width: 1280, height: 720, nonBlackPixels: 20_000, rgbaSha256: digest.repeat(64),
    backingWidth: 1280, backingHeight: 720, displayWidth: 1280, displayHeight: 720,
    centerOffsetXPx: 0, centerOffsetYPx: 0, focused: true,
  });
  return {
    schemaVersion: 1,
    caseId: "ACC-KIRIKIRI-001",
    status: "PASS",
    stages: [...kirikiriProductStages],
    ids: {
      gameId: "01a0452f-a9cc-7883-bc20-d554b47224ad",
      immersiveLaunchId: "01a0452f-a9d9-7c54-a65e-c75fc19a473b",
      importItemId: "01a0452e-d583-7a46-ab55-67928e963086",
      originalLaunchId: "01a0452f-a9e9-7c54-a65e-c75fc19a473b",
      restoreLaunchId: "01a0452f-bc94-7cc2-8403-4819f3c381e2",
      saveStateId: "01a0452f-bc6d-7d0a-b975-891ece6a0cc9",
    },
    immersiveMenu: { actions: ["取消", "创建存档", "退出游戏"], screenshot: "screenshots/immersive-exit-menu.png" },
    checkpoint: { format: "kirikiri-save-bundle-v1", sizeBytes: 128_000 },
    restoreComparison: {
      discriminativePixelCount: 160, matched: true,
      restoredToBMeanDistance: 12, restoredToCMeanDistance: 120,
    },
    loading: {
      schemaVersion: 1,
      sameProjectContentIdentity: true,
      firstVisible: loadingSnapshot(0),
      restoreVisible: loadingSnapshot(2),
    },
    screenshots: {
      preview: screenshot("1"), productBeforeInput: screenshot("2"), productAfterInput: screenshot("3"),
      productAfterCheckpoint: screenshot("4"), restored: screenshot("3"), postRestoreInput: screenshot("5"),
    },
    browser: { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 },
  };
}

function loadingSnapshot(runtimeAssetCacheHitCount) {
  return {
    declaredLargeFileCount: 1,
    declaredProjectBytes: 32 * 1024 * 1024,
    declaredProjectFileCount: 1,
    fullProjectFileResponseCount: 0,
    nativeProjectResponseCount: 0,
    projectContentIdentityCount: 1,
    rangeProjectFileResponseCount: 4,
    requestedLargeFileCount: 1,
    requestedProjectBytes: 512 * 1024,
    requestedProjectFileCount: 1,
    runtimeAssetCacheHitCount,
    runtimeAssetRequestCount: 4,
    runtimeAssetTransferredBytes: 1_000_000,
  };
}
