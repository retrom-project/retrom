import assert from "node:assert/strict";
import test from "node:test";

import {
  assertButterscotchProductEvidence,
  butterscotchProductStages,
} from "./butterscotch_product_contract.mjs";

const screenshot = {
  backingHeight: 480, backingWidth: 640, centerOffsetXPx: 0, centerOffsetYPx: 0,
  displayHeight: 480, displayWidth: 640, focused: true, height: 480,
  nonBlackPixels: 10_000, rgbaSha256: "a".repeat(64), width: 640,
};

function evidence() {
  return {
    schemaVersion: 1,
    caseId: "ACC-BUTTERSCOTCH-001",
    status: "PASS",
    stages: [...butterscotchProductStages],
    ids: {
      importItemId: "01a05123-1234-7123-8123-123456789abc",
      gameId: "01a05123-1234-7123-8123-223456789abc",
      saveStateId: "01a05123-1234-7123-8123-323456789abc",
      originalLaunchId: "01a05123-1234-7123-8123-423456789abc",
      restoreLaunchId: "01a05123-1234-7123-8123-523456789abc",
    },
    checkpoint: { format: "butterscotch-checkpoint-v2", sizeBytes: 128 },
    cache: {
      contentDigest: "b".repeat(64), firstDataWinResponseCount: 1,
      restoreDataWinResponseCount: 0, restoreIndexResponseCount: 1,
    },
    screenshots: {
      preview: screenshot,
      productBeforeInput: screenshot,
      productAfterInput: { ...screenshot, rgbaSha256: "c".repeat(64) },
      restored: { ...screenshot, rgbaSha256: "d".repeat(64) },
      postRestoreInput: { ...screenshot, rgbaSha256: "e".repeat(64) },
    },
    browser: { pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0 },
  };
}

test("accepts the complete Butterscotch product chain", () => {
  assert.doesNotThrow(() => assertButterscotchProductEvidence(evidence()));
});

test("rejects cache, input, checkpoint and browser regressions", () => {
  const invalid = [
    { ...evidence(), cache: { ...evidence().cache, restoreDataWinResponseCount: 1 } },
    { ...evidence(), checkpoint: { format: "butterscotch-checkpoint-v2", sizeBytes: 17 * 1024 * 1024 } },
    { ...evidence(), browser: { pageErrorCount: 1, consoleErrorCount: 0, dialogCount: 0 } },
    {
      ...evidence(),
      screenshots: { ...evidence().screenshots, productAfterInput: evidence().screenshots.productBeforeInput },
    },
  ];
  for (const value of invalid) {
    assert.throws(() => assertButterscotchProductEvidence(value), /BUTTERSCOTCH_ACCEPTANCE_/u);
  }
});
