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
  ];
  for (const mutate of mutations) {
    const value = evidence();
    mutate(value);
    assert.throws(() => assertOnsProductEvidence(value), /ONS_ACCEPTANCE_EVIDENCE_INVALID/);
  }
});

function evidence() {
  const screenshot = (digest) => ({ width: 640, height: 480, nonBlackPixels: 20_000, rgbaSha256: digest.repeat(64) });
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
    screenshots: {
      preview: screenshot("1"), productBeforeInput: screenshot("2"), productAfterInput: screenshot("3"),
      restored: screenshot("4"), postRestoreInput: screenshot("5"),
    },
    browser: { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 },
  };
}
